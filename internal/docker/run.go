package docker

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qubesome/qubesome-cli/internal/workload"
	"golang.org/x/sys/execabs"
)

var (
	command = "docker"
)

func Run(wl workload.Effective) error {
	ndevs, err := namedDevices(wl.NamedDevices)
	if err != nil {
		return fmt.Errorf("failed to get named devices: %w", err)
	}

	devices := []string{
		"-v=/run/user/1000/pipewire-0:/run/user/1000/pipewire-0",
		"--device=/dev/dri",
		"--device=/dev/snd",
		"--group-add=audio",
		"--device=/dev/video0",
		"--group-add=video",
		"-v=/dev:/dev",
		"-v=/dev/shm:/dev/shm",
	}

	//TODO: auto map profiles dir
	paths := []string{
		"-v=/etc/localtime:/etc/localtime:ro",
		"-v=/etc/machine-id:/etc/machine-id:ro",
		"-v=/tmp/.X11-unix:/tmp/.X11-unix",
		"-v=/run/dbus/system_bus_socket:/run/dbus/system_bus_socket",
		"-v=/run/user/1000/bus:/run/user/1000/bus",
		"-v=/var/lib/dbus:/var/lib/dbus",
		"-v=/run/user/1000/dbus-1:/run/user/1000/dbus-1",
		"-v=/home/paulo/.qubesome/profiles/personal/homedir/Downloads:/home/chrome/Downloads",
		"-v=/home/paulo/.qubesome/profiles/personal/homedir/.config/google-chrome:/home/chrome/.config/google-chrome",
	}
	envs := []string{
		"-e=DISPLAY",
		"-e=DBUS_SESSION_BUS_ADDRESS",
		"-e=XDG_RUNTIME_DIR",
		"-e=XDG_SESSION_ID",
	}
	args := []string{
		"run",
		"--rm",
		"-d",
		"--security-opt", "seccomp=unconfined",
	}

	args = append(args, paths...)
	args = append(args, devices...)
	for _, ndev := range ndevs {
		args = append(args, fmt.Sprintf("--device=%s", ndev))
	}

	args = append(args, envs...)

	args = append(args, fmt.Sprintf("--name=%s-%s", wl.Name, wl.Profile))
	args = append(args, wl.Image)
	args = append(args, wl.Command)
	args = append(args, wl.Args...)

	fmt.Println(command, strings.Join(args, " "))

	cmd := execabs.Command(command, args...)

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	return cmd.Run()
}

var HidrawPrefix = "HID_NAME="

func namedDevices(names []string) ([]string, error) {
	devs := []string{}

	hidraws, err := filepath.Glob("/sys/class/hidraw/*/device/uevent")
	if err != nil {
		return nil, fmt.Errorf("failed to get hidraw files: %w", err)
	}

	for _, fn := range hidraws {
		d, err := readFile(fn, names)
		if err != nil {
			return nil, fmt.Errorf("failed to get hidraw files: %w", err)
		}
		devs = append(devs, d...)
	}

	return devs, nil
}

func readFile(fn string, names []string) ([]string, error) {
	devs := []string{}

	f, err := os.Open(fn)
	if err != nil {
		return nil, fmt.Errorf("failed to open hidraw file: %w", err)
	}

	r := bufio.NewScanner(f)
	for r.Scan() {
		val := r.Text()

		if !strings.HasPrefix(val, HidrawPrefix) {
			continue
		}
		devName := strings.TrimPrefix(val, HidrawPrefix)

		for _, n := range names {
			if strings.HasPrefix(devName, n) {
				fdn := "/dev/" + strings.TrimPrefix(fn, "/sys/class/hidraw/")
				fdn = strings.TrimSuffix(fdn, "/device/uevent")
				devs = append(devs, fdn)
			}
		}
	}

	return devs, nil
}
