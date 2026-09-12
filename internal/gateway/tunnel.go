package gateway

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// ProxyEnv names the variable a sandbox is told its proxy endpoint in.
//
// It is the whole endpoint and not only the address, because the port
// belongs to the gateway image rather than to qubesome. A workload could
// work the address out for itself, since it is already its default route
// and its resolver, and it could not work out the port at all.
const ProxyEnv = "QUBESOME_GATEWAY_PROXY"

// ProxyFromEnv returns the endpoint this sandbox asks for a tunnel on.
//
// An absent variable means this workload has no gateway, which is not a
// thing a default could stand in for: there would be nothing listening at
// whatever was guessed.
func ProxyFromEnv() (string, error) {
	addr := os.Getenv(ProxyEnv)
	if addr == "" {
		return "", fmt.Errorf("%s is not set, so this workload has no gateway to ask for a tunnel", ProxyEnv)
	}

	return addr, nil
}

// Tunnel joins in and out to host:port through the proxy at proxyAddr.
//
// The gateway drops every port but 80, 443 and 53, so a workload reaches
// anything else by asking it for a tunnel rather than by connecting out.
// This is that ask, in the shape ssh wants it: a command that speaks the
// target on its own standard input and output, so
//
//	ProxyCommand qubesome tunnel %h %p
//
// is the whole of what a workload needs to reach a host it is allowed to.
//
// It returns when the target is done, not when the workload stops writing.
// A client that has said all it has to say still has an answer coming, and
// ending at its end of the copy would cut that answer off.
func Tunnel(ctx context.Context, proxyAddr, host, port string, in io.Reader, out io.Writer) error {
	target, err := checkTarget(host, port)
	if err != nil {
		return err
	}

	var d net.Dialer

	conn, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return fmt.Errorf("failed to reach the gateway proxy at %s: %w", proxyAddr, err)
	}
	defer conn.Close()

	// Any bytes the target has already sent are in this reader and not in
	// the socket, because reading the response head is what put them
	// there. Reading from the socket after this point loses them, and for
	// ssh those bytes are the server's banner.
	br := bufio.NewReader(conn)

	if err := connect(conn, br, target); err != nil {
		return err
	}

	go func() {
		_, _ = io.Copy(conn, in)

		// The target learns that the workload has finished only if the
		// write half is closed. Without this a server waiting for the
		// end of a request waits for the tunnel to be torn down instead.
		if c, ok := conn.(*net.TCPConn); ok {
			_ = c.CloseWrite()
		}
	}()

	if _, err := io.Copy(out, br); err != nil {
		return fmt.Errorf("failed while carrying %s: %w", target, err)
	}

	return nil
}

// connect asks the proxy for a tunnel to target and reports whether it
// gave one.
func connect(conn net.Conn, br *bufio.Reader, target string) error {
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		return fmt.Errorf("failed to ask the gateway for a tunnel to %s: %w", target, err)
	}

	// A response to CONNECT carries no body of its own, so this reads the
	// head and stops, leaving the target's own bytes in br.
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return fmt.Errorf("the gateway gave no usable answer for %s: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// The gateway says why in the body, and why is the whole of what
		// is useful here: a refusal is its policy, not a fault.
		reason, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		said := strings.TrimSpace(string(reason))
		if said == "" {
			said = resp.Status
		}

		return fmt.Errorf("the gateway refused a tunnel to %s with %d: %s", target, resp.StatusCode, said)
	}

	return nil
}

// checkTarget returns host and port in the one form CONNECT accepts, and
// refuses anything that would not survive being written into a request.
//
// The target reaches here from an ssh command line, which is a workload's
// to choose, and it goes straight into a request head. A host carrying a
// line ending would end that head early and let whatever follows be read
// as a second request of the workload's own writing, so the check is on
// what the proxy would parse rather than on what a hostname may contain.
func checkTarget(host, port string) (string, error) {
	if host == "" {
		return "", errors.New("not a target: no host was given")
	}

	// A colon is not among these. An IPv6 literal is made of them, and
	// JoinHostPort below brackets one so that the target stays a host and
	// a port however many colons the host holds.
	if strings.ContainsAny(host, "\r\n\t ") || strings.ContainsFunc(host, isControl) {
		return "", fmt.Errorf("not a target: host %q holds a character a request head cannot carry", host)
	}

	n, err := strconv.Atoi(port)
	if err != nil {
		return "", fmt.Errorf("not a target: port %q is not a number", port)
	}
	if n < 1 || n > 65535 {
		return "", fmt.Errorf("not a target: port %d is outside 1 to 65535", n)
	}

	return net.JoinHostPort(host, strconv.Itoa(n)), nil
}

// isControl reports whether r is a character a request head cannot carry.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}
