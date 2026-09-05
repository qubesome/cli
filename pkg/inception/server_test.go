package inception

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCheckRPCArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "no args"},
		{name: "plain args", args: []string{"https://example.com", "file.txt"}},
		{name: "double dash flag", args: []string{"--renderer-cmd-prefix=/bin/sh"}, wantErr: true},
		{name: "single dash flag", args: []string{"-marionette"}, wantErr: true},
		{name: "flag after a plain arg", args: []string{"https://example.com", "--headless"}, wantErr: true},
		{name: "dash within an arg", args: []string{"a-b"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := checkRPCArgs(tc.args)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			// A rejected argument is the caller's fault, so the client
			// must see it as such rather than as codes.Unknown.
			if got := status.Code(err); got != codes.InvalidArgument {
				t.Errorf("got code %v, want %v", got, codes.InvalidArgument)
			}
		})
	}
}
