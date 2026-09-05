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

// familyLocal marks an entry as matching any local connection.
const familyLocal = 0xffff

var cookieFunc = newCookie

func AuthPair(display uint8, parent io.Reader, server, client io.Writer) error {
	rec, err := readRecord(parent)
	if err != nil {
		return fmt.Errorf("failed to read parent auth file: %w", err)
	}

	c, err := cookieFunc()
	if err != nil {
		return err
	}

	// The display is stored as its ASCII digits, so its field grows with
	// the number. Reading the parent record instead of indexing fixed
	// offsets also keeps this correct for any host address length: the
	// address is the hostname, which is rarely the same length twice.
	rec.number = []byte(strconv.Itoa(int(display)))
	rec.data = c

	if err := rec.writeTo(server); err != nil {
		return fmt.Errorf("failed to write server auth file: %w", err)
	}

	// The workload's copy is family local, which is what lets it connect
	// from inside a container.
	rec.family = familyLocal

	if err := rec.writeTo(client); err != nil {
		return fmt.Errorf("failed to write workload auth file: %w", err)
	}

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
