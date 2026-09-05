package xauth

import (
	"encoding/binary"
	"fmt"
	"io"
)

// maxFieldLen bounds each variable length field of a record.
//
// The real values are a hostname, a display number, an auth scheme name
// and a cookie, none of which come close. The bound is here so that a
// corrupt file cannot ask for an arbitrary allocation.
const maxFieldLen = 4096

// record is a single entry of an xauth file.
//
// Every entry is a family followed by four length prefixed fields:
//
//	family(2) addressLen(2) address numberLen(2) number
//	nameLen(2) name dataLen(2) data
//
// The number is the display, held as its ASCII digits.
type record struct {
	family  uint16
	address []byte
	number  []byte
	name    []byte
	data    []byte
}

// readRecord reads one record from r.
func readRecord(r io.Reader) (record, error) {
	var rec record

	family, err := readUint16(r)
	if err != nil {
		return rec, fmt.Errorf("failed to read family: %w", err)
	}
	rec.family = family

	for _, f := range []struct {
		name string
		dst  *[]byte
	}{
		{"address", &rec.address},
		{"number", &rec.number},
		{"name", &rec.name},
		{"data", &rec.data},
	} {
		v, err := readField(r)
		if err != nil {
			return record{}, fmt.Errorf("failed to read %s: %w", f.name, err)
		}
		*f.dst = v
	}

	return rec, nil
}

// writeTo writes the record in the on-disk format.
func (rec record) writeTo(w io.Writer) error {
	if err := writeUint16(w, rec.family); err != nil {
		return err
	}

	for _, f := range [][]byte{rec.address, rec.number, rec.name, rec.data} {
		if len(f) > maxFieldLen {
			return fmt.Errorf("field is too long: %d", len(f))
		}
		if err := writeUint16(w, uint16(len(f))); err != nil { //nolint:gosec // bounded above.
			return err
		}
		if _, err := w.Write(f); err != nil {
			return err
		}
	}

	return nil
}

func readField(r io.Reader) ([]byte, error) {
	n, err := readUint16(r)
	if err != nil {
		return nil, err
	}
	if n > maxFieldLen {
		return nil, fmt.Errorf("field is too long: %d", n)
	}

	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}

	return b, nil
}

func readUint16(r io.Reader) (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}

	return binary.BigEndian.Uint16(b[:]), nil
}

func writeUint16(w io.Writer, v uint16) error {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)

	_, err := w.Write(b[:])

	return err
}
