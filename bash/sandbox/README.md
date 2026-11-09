# sandbox

[中文](README.zh.md) | English

Launch Debian 13 / Rocky 10 cloud images with QEMU, KVM and passt.

## Files

- `sandbox.sh`: the single implementation. Subcommands: `bootstrap`, `run`, `stop`; supports Chinese/English output and `curl | bash`.
- `user-data.yaml.tmpl`: used to generate `~/user-data.yaml` when it is missing. It contains the cloud-init Jinja logic and package lists. `sandbox.sh` embeds the same template for pipe mode.
- `.gitignore`: images, disks, seeds, logs and keys stay out of the repository.

## Quick start

### Local

```bash
./sandbox.sh bootstrap --qemu-version v11.1.2 --jobs "$(nproc)"
./sandbox.sh run --background
./sandbox.sh stop
```

`bootstrap` needs root (apt and installation to `/usr/local/`). Prefix it with `sudo` when needed. LTO is enabled by default; use `--no-lto` on low-memory hosts. Re-running with the same tag skips the build; upgrade by changing the tag:

```bash
./sandbox.sh bootstrap --qemu-version latest
```

QEMU sources and build trees live in `/root/qemu-<tag>`. After a successful install, other `qemu-*` build trees are removed by default; `--keep-old-builds` keeps them.

### curl

```bash
# Build QEMU (root required)
curl -fsSL https://raw.githubusercontent.com/kebyn/mini-scripts/master/bash/sandbox/sandbox.sh \
  | sudo bash -s -- bootstrap --qemu-version v11.1.2 --lang en

# Foreground console (interactive terminal)
curl -fsSL https://raw.githubusercontent.com/kebyn/mini-scripts/master/bash/sandbox/sandbox.sh \
  | bash -s -- run --foreground --lang en

# Background
curl -fsSL https://raw.githubusercontent.com/kebyn/mini-scripts/master/bash/sandbox/sandbox.sh \
  | bash -s -- run --background --lang en

# Clean shutdown
curl -fsSL https://raw.githubusercontent.com/kebyn/mini-scripts/master/bash/sandbox/sandbox.sh \
  | bash -s -- stop
```

Pipe mode uses the embedded user-data template, so it does not depend on local files. `curl | bash` executes remote code; review the commit or download the script first in production.

## Language

```bash
./sandbox.sh run --lang zh --dry-run
./sandbox.sh run --lang en --dry-run
VM_LANG=zh ./sandbox.sh run --dry-run
```

`--lang` accepts `auto`, `zh` or `en` and defaults to `auto`. `auto` checks `LC_ALL`, then `LC_MESSAGES`, then `LANG`; `zh*` selects Chinese, everything else English.

## run defaults

- Distro: Debian 13
- vCPUs: `nproc`
- RAM: `MemAvailable + SwapFree - 1024` MiB; no fixed upper limit
- Disk: 90% of free space on the filesystem containing `/data`
- Ports: SSH `8022`, HTTP `8080`, HTTPS `8443`; all IPv4 and IPv6 addresses by default
- Hostname: `local`; FQDN from `--fqdn` (default `kebyn.cc`)

Swap is counted by default. Guest RAM that exceeds available host RAM is allocated lazily and may be swapped, which is slower; use `--mem-swap off` to size from RAM only.

## Default guest environment

The generated user-data is a cloud-init Jinja template:

- Alpine gets `dev` shell `/bin/sh`; other distros get `/bin/bash`.
- SSH password login and keyboard-interactive auth are disabled; only keys are accepted.
- sysctl tuning is applied through `/etc/sysctl.d/99-sandbox.conf`, with `br_netfilter` and `overlay` in `/etc/modules-load.d/sandbox.conf`.
- `--profile dev` (default) installs Docker, Node.js and common tools through cloud-init `packages`; `runcmd` only starts services, adds `dev` to the docker group, applies sysctl/modules, and installs uv where no distro package exists.
- `--profile minimal` keeps SSH hardening, hostname and sysctl, and skips `packages` and dev-related `runcmd`.
- Node.js comes from the distro repository (Arch uses `nodejs-lts`). On RHEL-family images Docker CE is added via `yum_repos` and may install a newer kernel; a small `runcmd` step installs the running kernel's extra modules so Docker starts immediately.
- The completion marker is `/var/log/sandbox-provision.done`; cloud-init output is in `/var/log/cloud-init-output.log`.

Log in:

```bash
ssh -p 8022 dev@127.0.0.1
```

If `~/user-data.yaml` exists, it is used unchanged. The script cannot infer its user or key; use `--ssh-user` / `--ssh-key` to adjust the login hint. If it is missing, the script creates `~/user-data.yaml` and `/data/id_ed25519`.

Foreground mode needs a terminal:

```bash
./sandbox.sh run
```

Quit QEMU with `Ctrl-A X`.

## Supported distributions

| Alias | Version |
| --- | --- |
| `debian`, `debian-13` | Debian 13 |
| `ubuntu`, `ubuntu-24.04` | Ubuntu 24.04 LTS |
| `ubuntu-26.04` | Ubuntu 26.04 LTS |
| `rocky`, `rocky-10` | Rocky Linux 10 |
| `alma`, `almalinux`, `alma-10` | AlmaLinux 10 |
| `centos`, `centos-10` | CentOS Stream 10 |
| `fedora`, `fedora-44` | Fedora 44 |
| `opensuse`, `leap`, `leap-16.0` | openSUSE Leap 16.0 |
| `tumbleweed` | openSUSE Tumbleweed |
| `arch`, `archlinux` | Arch Linux |
| `alpine`, `alpine-3.23` | Alpine 3.23 |

```bash
./sandbox.sh run --distro ubuntu --background
./sandbox.sh run --distro alpine --background
./sandbox.sh run --list-distros
```

Aliases are pinned to the versions above. Use `--url` for a newer build or a custom image; `--image` still accepts a local qcow2/raw image.

Alpine's cloud image uses a partitionless disk and rejects a locked cloud-init account. The generated template sets `lock_passwd: false`, `passwd: "*"` and uses Jinja to select `/bin/sh` on Alpine and `/bin/bash` elsewhere; add the same settings if you use a custom `~/user-data.yaml`.

## Disk behavior

Every start overwrites `/data/disk.img` from the base image, so guest changes are lost. This is intended for sandboxes.

Keep the previous system disk:

```bash
./sandbox.sh run --keep-disk --background
./sandbox.sh stop
```

Force a fresh base image download (the work disk still follows `--keep-disk`):

```bash
./sandbox.sh run --fresh-image --background
```

## Common options

```bash
./sandbox.sh run --distro rocky --cpus 4 --mem 8192 --disk 100G
./sandbox.sh run --mem-swap off              # size RAM from host RAM only
./sandbox.sh run --list-distros              # supported aliases
./sandbox.sh run --accel tcg                 # no KVM
./sandbox.sh run --net-backend user          # passt unavailable
./sandbox.sh run --bind-addr 127.0.0.1       # localhost only
./sandbox.sh run --fqdn local.kebyn.cc       # custom FQDN
./sandbox.sh run --profile minimal           # skip Docker/Node/uv
./sandbox.sh run --foreground                # foreground console in pipe mode
./sandbox.sh run --dry-run                   # print the QEMU command
./sandbox.sh run --help                      # all options
```

The Rocky image is about 520 MB; Debian is about 440 MB. `--disk` is sized dynamically and does not preallocate the full virtual size.

## Pipe mode and the foreground console

- `bash <(curl ...) run`: stdin stays attached to the terminal, so this runs in the foreground:
  ```bash
  bash <(curl -fsSL https://raw.githubusercontent.com/kebyn/mini-scripts/master/bash/sandbox/sandbox.sh) run
  ```
- `curl ... | bash -s -- run --foreground`: attaches QEMU stdin to `/dev/tty`; a controlling terminal and terminal output are required. `--foreground` and `--background` are mutually exclusive.
- `curl ... | bash -s -- run`: automatically uses background mode when stdin is not a TTY, matching the previous behavior.
- CI, cron and `setsid` environments have no controlling terminal; use `--background`.
- For a foreground console you can also download the script first:
  ```bash
  curl -fsSL https://raw.githubusercontent.com/kebyn/mini-scripts/master/bash/sandbox/sandbox.sh -o sandbox.sh
  chmod +x sandbox.sh
  ./sandbox.sh run
  ```
- `stop`, `--dry-run` and `bootstrap` work normally in pipe mode.

## Troubleshooting

**`passt` unavailable**: make sure `passt` is installed and QEMU's `-netdev help` lists `passt`. `run` falls back to QEMU user networking by default; an explicit `--net-backend passt` fails instead.

**KVM unavailable**: `/dev/kvm` must be readable and writable. Without nested virtualization use `--accel tcg`; boot will be much slower.

**Port in use**: the script checks 8022/8080/8443 before starting and prints the owner. Change ports with `--ssh-port` and friends.

**Stopping the background VM**: prefer `./sandbox.sh stop`, which requests an ACPI shutdown through the QEMU monitor. `kill` or `Ctrl-A X` terminates QEMU abruptly; with `--keep-disk`, an unclean ext4 can lose the Debian cloud image's SSH host keys and break the next `sshd` start.

**cloud-init changes ignored**: the instance-id is derived from the user-data hash. Restart after editing user-data, or add `--recreate-seed` to force a new seed.
