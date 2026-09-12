package gateway

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProxy answers one CONNECT on a loopback listener and returns the
// address to ask. handle is given the target the request named and the
// connection, positioned just after the request head.
func fakeProxy(t *testing.T, handle func(target string, c net.Conn)) string {
	t.Helper()

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()

		req, err := http.ReadRequest(bufio.NewReader(c))
		if err != nil {
			return
		}

		handle(req.RequestURI, c)
	}()

	return ln.Addr().String()
}

// The tunnel is a pipe once the proxy has answered, so what the workload
// writes reaches the target and what the target says reaches the workload.
//
// The server's first bytes ride in the same write as the response head
// here, which is what a real one does and is exactly what a reader that
// buffers that head will swallow if it then reads from the socket instead
// of from its own buffer.
func TestTunnelCarriesBytesBothWays(t *testing.T) {
	t.Parallel()

	sent := make(chan string, 1)
	addr := fakeProxy(t, func(_ string, c net.Conn) {
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\nSSH-2.0-server\r\n")

		b, _ := io.ReadAll(c)
		sent <- string(b)
	})

	var out bytes.Buffer
	err := Tunnel(t.Context(), addr, "github.com", "22", strings.NewReader("SSH-2.0-client\r\n"), &out)
	require.NoError(t, err)

	assert.Equal(t, "SSH-2.0-server\r\n", out.String())
	assert.Equal(t, "SSH-2.0-client\r\n", <-sent)
}

// The proxy is told which host and port to reach, in the one form it
// accepts.
func TestTunnelAsksForTheTarget(t *testing.T) {
	t.Parallel()

	asked := make(chan string, 1)
	addr := fakeProxy(t, func(target string, c net.Conn) {
		asked <- target
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
	})

	require.NoError(t, Tunnel(t.Context(), addr, "github.com", "22", strings.NewReader(""), io.Discard))
	assert.Equal(t, "github.com:22", <-asked)
}

// A refusal is the gateway's policy answering, and it is the whole reason
// a workload cannot reach something. Saying which target was refused and
// what the proxy said is what turns it into something actionable.
func TestTunnelReportsARefusal(t *testing.T) {
	t.Parallel()

	addr := fakeProxy(t, func(_ string, c net.Conn) {
		_, _ = io.WriteString(c, "HTTP/1.1 403 Forbidden\r\nContent-Length: 10\r\n\r\nForbidden\n")
	})

	err := Tunnel(t.Context(), addr, "gitlab.com", "22", strings.NewReader(""), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com:22")
	assert.Contains(t, err.Error(), "403")
}

// A host is put into a request head, so one carrying a line ending could
// write a second request of its own choosing to the proxy. The target
// comes from an ssh command line, which is a place a workload chooses, so
// it is checked before anything is dialled rather than trusted.
func TestTunnelRejectsATargetThatCouldForgeARequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		port string
	}{
		{"a newline in the host", "github.com\r\nCONNECT evil:22 HTTP/1.1", "22"},
		{"a bare newline", "github.com\nx", "22"},
		{"a space in the host", "github.com evil", "22"},
		{"an empty host", "", "22"},
		{"a port that is not a number", "github.com", "22\r\nx"},
		{"a port out of range", "github.com", "70000"},
		{"an empty port", "github.com", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// An address nothing is listening on, so a dial that should
			// not happen fails as a dial and not as this.
			err := Tunnel(t.Context(), "127.0.0.1:1", tc.host, tc.port, strings.NewReader(""), io.Discard)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not a target")
		})
	}
}

// A proxy that is not there is the gateway not being there, which is a
// different thing from one that refused.
func TestTunnelReportsAProxyItCannotReach(t *testing.T) {
	t.Parallel()

	err := Tunnel(t.Context(), "127.0.0.1:1", "github.com", "22", strings.NewReader(""), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1:1")
}

// The endpoint comes from the sandbox's environment, where the launch put
// it. Nothing else in the sandbox knows the port, so its absence means
// this workload has no gateway rather than that a default would do.
func TestProxyFromEnv(t *testing.T) {
	t.Setenv(ProxyEnv, "10.111.0.1:3128")

	got, err := ProxyFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "10.111.0.1:3128", got)
}

func TestProxyFromEnvWithoutOne(t *testing.T) {
	t.Setenv(ProxyEnv, "")

	_, err := ProxyFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), ProxyEnv)
}
