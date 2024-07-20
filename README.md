## qubesome

Welcome to qubesome! This project is a command-line interface (CLI) tool aimed
to simplify managing Linux desktop configurations. It works by virtualizing
both the Window Manager and the workloads based on a declarative state from
a git repository.

How can this be useful?

- Test-drive Window Manager configurations without having to commit to them
or needing to impact your existing setup.
- Version control your window manager and workloads.
- Bump configuration and software versions via PRs - and roll them back in
the same way.
- Provide isolation across profiles and workloads (clipboard, network,
storage, etc).

### Quick Start
#### Install

```
go install https://github.com/qubesome/cli@latest
```

#### Start Profile

Start one of the two profiles in the sample-dotfiles (`i3` or `awesome`) repo:
```
qubesome start -git https://github.com/qubesome/sample-dotfiles <PROFILE>
```

> **_NOTE:_** Press `Ctrl`+`Shift` to key and mouse grab in and out of
the qubesome profile.

> **_NOTE 2:_** Each profile has a different `display` set in [qubesome.config](qubesome.config),
therefore their clipboards are isolated between themselves and the host.
To transfer clipboards between profiles use `qubesome clipboard`.

### Usage

Check whether dependency requirements are met:
```
qubesome deps show
```

Use a local copy, and if not found fallback to a fresh clone:
```
qubesome start -git https://github.com/qubesome/sample-dotfiles -local <local_git_path> <profile>
```

Copy clipboard from the host to the i3 profile:
```
qubesome clipboard --from-host i3
```

#### Available Commands

- `qubesome start`: Start a qubesome environment for a given profile.
- `qubesome run`: Run qubesome workloads.
- `qubesome clipboard`: Manage the images within your workloads.
- `qubesome images`: Manage the images within your workloads.
- `qubesome xdg`: Handle xdg-open based via qubesome.

For more information on each command, run `qubesome <command> --help`.


### Requirements

#### Minimum

Qubesome requires `docker` and `xrandr` installed on a machine
running Xorg. To install them using zypper:
```
sudo zypper install -y docker xrandr
```

#### GPU pass-through

To enable GPU workloads (e.g. Google meet with background filters),
install NVidia's [container-toolkit].

First install the [NVIDIA drivers]. For Tumbleweed users:
```
zypper install openSUSE-repos-Tumbleweed-NVIDIA
zypper install-new-recommends --repo repo-non-free
```

Followed up by installing and configuring the nvidia container toolkit:
```
zypper modifyrepo --enable nvidia-container-toolkit-experimental
zypper ar https://nvidia.github.io/libnvidia-container/stable/rpm/nvidia-container-toolkit.repo
nvidia-ctk runtime configure --runtime=docker
systemctl restart docker
```

[NVIDIA drivers]: https://en.opensuse.org/SDB:NVIDIA_drivers
[container-toolkit]: https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html#installing-with-zypper

### FAQ

#### Does it provide any sort of isolation across profiles?
This largely depends on the configuration, but overall the main supported runner
is based on docker, which comes the limitations of container-level isolation.
But here are a few highlights:

##### Xorg instance isolation
Each Qubesome profile can be executed on its own Xorg display, which
translates into Clipboard isolation across workloads across different
profiles.

##### Host Access
Each profile can define host access (e.g. device, network, dbus) allowed for
its workloads. For example, having a Work profile and a Personal profile, it
is possible to limit what parts of the disk (or external storage) can be mounted
to each.

##### Per-workload Network Access (Experimental)
Ability to control network/internet access for each workload, and run the window
manager without internet access. Auditing access violations, for visibility of when
workloads are trying to access things they should not.

#### Is Rootless docker support?
Not at this point, potentially this could be introduced in the future.

### Why do I need to run xhost +SI:localuser:${USER}?
Some Linux distros (e.g. Tumbleweed) have X11 access controls enabled
by default. Just the local current user will need to be granted access
for qubesome to work:
```
xhost +SI:localuser:${USER}
```

If `xhost` is not present, it can be installed with `sudo zypper install xhost`.

### License
This project is licensed under the Apache 2.0 License. Refer to the [LICENSE](LICENSE)
file for more information.
