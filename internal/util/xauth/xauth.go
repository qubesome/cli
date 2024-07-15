// Package xauth removes the need to shell out to xauth for managing
// xauth cookies.
package xauth

import (
	"crypto/rand"
	"fmt"
	"io"
	"strconv"
)

// For upstream xauth implementation refer to:
// https://gitlab.freedesktop.org/xorg/app/xauth/-/blob/master/process.c?ref_type=heads
// https://gitlab.freedesktop.org/xorg/app/xauth/-/blob/master/xauth.h?ref_type=heads

var cookieFunc = newCookie

func AuthPair(display uint8, parent io.Reader, server, client io.Writer) error {
	data := make([]byte, 50)
	n, err := parent.Read(data)
	if err != nil {
		return fmt.Errorf("failed to read from parent auth file: %w", err)
	}
	if n < 50 {
		return fmt.Errorf("auth file must be at least 50 chars long: was %d instead", n)
	}

	_, _ = server.Write(data[0:2])
	// Set family to local, which is required to enable access from a container.
	_, _ = client.Write([]byte{255, 255}) // ffff

	mw := io.MultiWriter(server, client)
	_, _ = mw.Write(data[2:11])

	// Set the display which this auth is going to be used for.
	_, _ = mw.Write([]byte(strconv.Itoa(int(display))))
	_, _ = mw.Write(data[12:34])

	c, err := cookieFunc()
	if err != nil {
		return err
	}

	_, _ = mw.Write(c)

	return nil
}

func ToNumeric(data []byte) string {
	if len(data) < 34 {
		return ""
	}

	return fmt.Sprintf("%04x %04x %x %04x %02x %04x %x %04x %x",
		data[0:2], data[2:4], data[4:9], data[9:11], data[11:12],
		data[12:14], data[14:32], data[32:34], data[34:])
}

func newCookie() ([]byte, error) {
	c := make([]byte, 16)
	_, err := rand.Read(c)
	if err != nil {
		return nil, err
	}
	return c, nil
}
