// Package seccomp builds a seccomp BPF filter from the containers/common
// default profile.
//
// The filter is handed to bwrap on a file descriptor. Qubesome inherits a
// maintained policy rather than authoring one.
package seccomp

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed seccomp.json
var profileJSON []byte

// Profile is the subset of the containers/common seccomp schema qubesome
// needs. Fields the sandbox cannot act on are deliberately absent. Unknown
// fields in the JSON are rejected during parsing so a schema addition shows
// up as a decode error rather than being silently ignored.
type Profile struct {
	DefaultAction   string  `json:"defaultAction"`
	DefaultErrnoRet *uint32 `json:"defaultErrnoRet"`
	DefaultErrno    string  `json:"defaultErrno"`
	ArchMap         []Arch  `json:"archMap"`
	Syscalls        []Rule  `json:"syscalls"`
}

type Arch struct {
	Architecture     string   `json:"architecture"`
	SubArchitectures []string `json:"subArchitectures"`
}

type Rule struct {
	Names    []string `json:"names"`
	Action   string   `json:"action"`
	ErrnoRet *uint32  `json:"errnoRet"`
	Errno    string   `json:"errno"`
	Args     []Arg    `json:"args"`
	Comment  string   `json:"comment"`
	Includes Filter   `json:"includes"`
	Excludes Filter   `json:"excludes"`
}

type Filter struct {
	Caps   []string `json:"caps"`
	Arches []string `json:"arches"`
}

type Arg struct {
	Index    uint32 `json:"index"`
	Value    uint64 `json:"value"`
	ValueTwo uint64 `json:"valueTwo"`
	Op       string `json:"op"`
}

// parse decodes a seccomp profile from JSON bytes.
func parse(data []byte) (*Profile, error) {
	p := &Profile{}

	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()

	if err := d.Decode(p); err != nil {
		return nil, fmt.Errorf("failed to parse seccomp profile: %w", err)
	}

	return p, nil
}

// Load parses the embedded profile.
func Load() (*Profile, error) {
	return parse(profileJSON)
}
