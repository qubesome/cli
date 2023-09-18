package main

import (
	"fmt"
	"os"

	"github.com/qubesome/qubesome-cli/internal/docker"
	"github.com/qubesome/qubesome-cli/internal/workload"
)

// overlay (cli + workload) - profile
// cli overwrites workload

// --camera --audio --x11 --gpu
// --network="bridge"
// --profile="personal"
func main() {
	// if os.Args < 2 {
	//     fmt.Printf("Usage: %s\n", os.Args[0])
	//     os.Exit(1)
	// }

	wl := workload.Effective{
		Name:    "chrome",
		Image:   "ghcr.io/qubesome/chrome:latest",
		Command: "/usr/bin/google-chrome",
		Profile: "personal",
		Opts: workload.Opts{
			Camera:    true, // TODO: pick up from named device from profile
			SmartCard: true, // TODO: pick up from named device from profile
			Audio:     true,
			X11:       true,
		},
		SingleInstance: true,
		Path: []string{
			"/Downloads",
			"/.config/google-chrome",
		},
		NamedDevices: []string{
			"YubiKey",
			"Logitech USB Receiver",
		},
	}

	err := docker.Run(wl)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
