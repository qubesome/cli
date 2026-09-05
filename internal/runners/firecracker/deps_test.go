package firecracker

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownload(t *testing.T) {
	t.Parallel()

	body := []byte("kernel contents")
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])

	tests := []struct {
		name     string
		body     []byte
		oversize bool
		status   int
		sha      string
		wantErr  string
	}{
		{name: "matching checksum", body: body, status: http.StatusOK, sha: want},
		{name: "mismatching checksum", body: body, status: http.StatusOK, sha: strings.Repeat("0", 64), wantErr: "checksum mismatch"},
		{name: "bad status", body: body, status: http.StatusNotFound, sha: want, wantErr: "bad status"},
		{name: "over the size limit", oversize: true, status: http.StatusOK, sha: want, wantErr: "larger than"},
		{name: "expected checksum is not hex", body: body, status: http.StatusOK, sha: "nonsense", wantErr: "invalid expected checksum"},
		{name: "expected checksum is too short", body: body, status: http.StatusOK, sha: "abcd", wantErr: "invalid expected checksum"},
		{name: "expected checksum in upper case", body: body, status: http.StatusOK, sha: strings.ToUpper(want)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)

				if !tc.oversize {
					_, _ = w.Write(tc.body)
					return
				}

				// Stream past the limit in small chunks rather than
				// holding 100MB of test data in memory.
				chunk := make([]byte, 64*1024)
				for sent := 0; sent <= maxDownloadSize; sent += len(chunk) {
					if _, err := w.Write(chunk); err != nil {
						return
					}
				}
			}))
			t.Cleanup(srv.Close)

			target := filepath.Join(t.TempDir(), "vmlinux")
			err := download(srv.URL, target, tc.sha)

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %v, want error containing %q", err, tc.wantErr)
				}

				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Error("target must not exist after a failed download")
				}

				entries, err := os.ReadDir(filepath.Dir(target))
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Errorf("expected no leftover files, got %v", entries)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(tc.body) {
				t.Errorf("got %q, want %q", got, tc.body)
			}

			if _, err := os.Stat(target + ".part"); !os.IsNotExist(err) {
				t.Error("the partial file must not be left behind")
			}
		})
	}
}
