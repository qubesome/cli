## qubesome [![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/qubesome/cli/badge)](https://scorecard.dev/viewer/?uri=github.com/qubesome/cli) [![OpenSSF Best Practices](https://www.bestpractices.dev/projects/10162/badge)](https://www.bestpractices.dev/projects/10162)

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
##### Using Go
```
go install github.com/qubesome/cli/cmd/qubesome@latest
```

##### For Leap and Tumbleweed users
```
zypper install -y qubesome
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
qubesome deps
```

Use a local copy, and if not found fallback to a fresh clone:
```
qubesome start -git https://github.com/qubesome/sample-dotfiles -local <local_git_path> <profile>
```

Copy clipboard from the host to the i3 profile:
```
qubesome clip from-host i3
```

Copy clipboard from the i3 profile to the host:
```
qubesome clip to-host i3
```

#### Available Commands

- `qubesome start`: Start a qubesome environment for a given profile.
- `qubesome run`: Run qubesome workloads.
- `qubesome workloads`: List the running workloads, and kill or restart them.
- `qubesome host-run`: Run commands on the host but display them in a qubesome profile.
- `qubesome clip`: Manage the images within your workloads.
- `qubesome images`: Manage the images within your workloads.
- `qubesome xdg`: Handle xdg-open based via qubesome.

For more information on each command, run `qubesome <command> --help`.


### Requirements

#### Minimum

Qubesome requires `docker` (or `podman`) and `xrandr` installed on a machine
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

#### Is rootless supported?
Yes, but only with podman.

### Why do I need to run xhost +SI:localuser:${USER}?
Some Linux distros (e.g. Tumbleweed) have X11 access controls enabled
by default. Just the local current user will need to be granted access
for qubesome to work:
```
xhost +SI:localuser:${USER}
```

If `xhost` is not present, it can be installed with `sudo zypper install xhost`.

### Gateway injection from the host keyring

In the gateway policy named by `gateway.config`, an `inject` rule can refer
to an entry in the host keyring:

```yaml
workloads:
  code-work: # workload code, profile work
    inject:
      - host: api.github.com
        header: Authorization
        format: "Bearer %s"
        valueFrom:
          keyring:
            service: qubesome/work/github
            account: token
```

Keep the gateway's existing `upstream` and TLS CA configuration. HTTPS
injection requires the gateway CA to be trusted inside the workload and
the gateway to verify the upstream server's certificate.

On Linux, the host backend reads the login Secret Service collection using
the attributes `service` and `username` (`account` in the policy). For
example, with libsecret's `secret-tool`, create the entry on the host using
its hidden prompt ([secret-tool options](https://github.com/GNOME/libsecret/blob/main/tool/secret-tool.c)):

```sh
secret-tool store --collection=login --label='Qubesome work GitHub' \
  service qubesome/work/github username token
```

The CLI reads only the entries named by injection rules and sends their
values over the authenticated gateway control channel. Values stay in
gateway memory; they are not written to staging files, environment variables,
command arguments, or workload mounts. The gateway receives no host keyring
socket. The service name is organizational; the effective workload's
injection rules determine which requests may use a token.

Keyring-backed rules match the exact hostname by default. Add
`includeSubdomains: true` beside `host` to include descendants. Existing
`valueFrom.file` and `valueFrom.env` rules retain their subdomain matching.
Only one source may be specified. Empty values and values containing CR,
LF, or NUL are refused. Requests carrying placeholder authorization headers
have those headers replaced before forwarding.

Tokens refresh when a workload launch reuses the gateway, or explicitly:

```sh
qubesome gateway reload
```

A successful refresh replaces the complete credential snapshot without a
cache delay. Removed injection grants also stop forwarding on existing
proxy connections. A missing or locked entry, invalid policy, or failed
refresh stops the launch/reload and retains the previous complete running
snapshot; it does not silently fall back to unauthenticated requests. To
revoke a grant, remove its rule and successfully reload, or stop the gateway.

Both the CLI and gateway image must include host-keyring support. After
upgrading the image, run `qubesome gateway stop`; the next workload launch
starts the new image. Older gateways cannot acknowledge keyring delivery,
so keyring-enabled launches fail rather than assume the token was installed.
For development across these two repositories, use a Go workspace containing
both `cli` and `gateway` (for example, `GOWORK=/absolute/path/to/cli/go.work`
when building the gateway). Before releasing a standalone gateway image,
update its CLI module dependency to the release containing this protocol.

The gateway is trusted with the supplied tokens across profiles. Keep the
policy and Qubesome session directory outside workload-writable mounts, and
do not grant workloads host keyring or control-channel access through broad
host runtime mounts. Injection grants let a workload exercise a token's
authority at the configured destination; that destination receives the token.

### License
This project is licensed under the Apache 2.0 License. Refer to the [LICENSE](LICENSE)
file for more information.
