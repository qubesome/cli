package bwrap

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/qubesome/cli/internal/files"
)

const (
	// sshConfigFile is the drop-in's name in the profile directory, beside
	// the other files a launch writes for a workload to read.
	sshConfigFile = "ssh_gateway.conf"

	// sshConfigDst is where the drop-in lands in the sandbox.
	//
	// The system ssh_config an image ships already includes this
	// directory, so a file here is read without the image being changed
	// and without anything of the image's being covered over. The user's
	// own ~/.ssh/config is read before it and so still wins, which is the
	// right way round: a workload's dotfiles may have reasons of their
	// own for how a host is reached.
	//
	// The number keeps it early among any siblings. ssh takes the first
	// value it is given for a keyword rather than the last.
	sshConfigDst = "/etc/ssh/ssh_config.d/10-qubesome-gateway.conf"
)

// sshGatewayConfig returns the ssh drop-in for a workload on the gateway.
//
// The gateway drops every port but 80, 443 and 53, so ssh cannot connect
// out and has to ask for a tunnel instead. Nothing in an image knows that,
// and the endpoint to ask is not knowable until a launch has allocated an
// address, so the workload is told here rather than in its image or in
// anybody's dotfiles.
//
// The endpoint itself is deliberately absent. The command reads it from
// QUBESOME_GATEWAY_PROXY, which the same launch sets, so this file says
// only how to reach the gateway and never which one.
func sshGatewayConfig() string {
	return `# Written by qubesome for a workload attached to the session gateway.
#
# The gateway drops every port but 80, 443 and 53, so ssh reaches a host
# by asking the gateway to carry the connection. The endpoint to ask is in
# QUBESOME_GATEWAY_PROXY, which the command below reads for itself.
Host *
	ProxyCommand ` + files.InProfileBinary + ` tunnel %h %p
`
}

// writeSSHConfig puts the drop-in where the sandbox mounts it from.
//
// It is rewritten on every launch rather than kept, because the file is
// qubesome's own and a stale one left by an older version would be
// mounted unchanged.
func writeSSHConfig(in input) error {
	if err := os.MkdirAll(in.ProfileDir, files.DirMode); err != nil {
		return fmt.Errorf("failed to ensure profile dir: %w", err)
	}

	path := filepath.Join(in.ProfileDir, sshConfigFile)
	if err := os.WriteFile(path, []byte(sshGatewayConfig()), files.FileMode); err != nil {
		return fmt.Errorf("failed to write %s: %w", sshConfigFile, err)
	}

	return nil
}
