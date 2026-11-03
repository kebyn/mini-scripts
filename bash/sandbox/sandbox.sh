#!/usr/bin/env bash
# Unified sandbox helper: build QEMU, run a cloud VM, or stop it.
set -Eeuo pipefail

VM_DIR="${VM_DIR:-/data}"
VM_LANG="${VM_LANG:-auto}"
LANG_CODE=en

# ---------------------------------------------------------------------------
# Language and common output helpers.
# ---------------------------------------------------------------------------
t() {
    if [ "$LANG_CODE" = zh ]; then
        printf '%s' "$1"
    else
        printf '%s' "$2"
    fi
}

die() { printf 'sandbox: %s %s\n' "$(t '错误:' 'error:')" "$*" >&2; exit 1; }
warn() { printf 'sandbox: %s %s\n' "$(t '警告:' 'warning:')" "$*" >&2; }
log() { printf '==> %s\n' "$*"; }

resolve_lang() {
    local opt="${1:-auto}" locale
    case "$opt" in
        zh|zh_*|zh-*) LANG_CODE=zh ;;
        en|en_*|en-*) LANG_CODE=en ;;
        auto)
            locale="${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}"
            case "$locale" in
                zh*|ZH*) LANG_CODE=zh ;;
                *) LANG_CODE=en ;;
            esac
            ;;
        *)
            LANG_CODE=en
            die "invalid --lang: $opt (use auto, zh or en)"
            ;;
    esac
}

usage_zh() {
    cat <<'EOF'
用法:
  sandbox.sh <命令> [选项]

命令:
  bootstrap   安装依赖并从源码编译 QEMU
  run         准备镜像和磁盘并启动 QEMU
  stop        正常关闭后台 VM

全局选项:
  --lang auto|zh|en   输出语言（默认 auto）
  -h, --help          显示帮助

运行 'sandbox.sh <命令> --help' 查看子命令选项。
EOF
}

usage_en() {
    cat <<'EOF'
Usage:
  sandbox.sh <command> [options]

Commands:
  bootstrap   Install dependencies and build QEMU from source
  run         Prepare the image/disk and start QEMU
  stop        Request a clean shutdown of the background VM

Global options:
  --lang auto|zh|en   Output language (default: auto)
  -h, --help          Show this help

Run 'sandbox.sh <command> --help' for command-specific options.
EOF
}

bootstrap_usage_zh() {
    cat <<'EOF'
用法:
  sandbox.sh bootstrap [选项]

  --qemu-version TAG   要编译的 QEMU tag（默认 v11.1.2；latest 解析最新 tag）
  --jobs N             并行 make 任务数（默认 nproc）
  --lto / --no-lto     启用/禁用 LTO（默认启用）
  --keep-old-builds    保留其它 /root/qemu-* 源码/构建目录
  --force-rebuild      已安装同版本时也重新编译
  --skip-deps          跳过 apt-get
  --dry-run            只打印计划
  --lang auto|zh|en    输出语言（默认 auto）
  -h, --help           显示帮助
EOF
}

bootstrap_usage_en() {
    cat <<'EOF'
Usage:
  sandbox.sh bootstrap [options]

  --qemu-version TAG   QEMU tag to build (default: v11.1.2; "latest" resolves the newest tag)
  --jobs N             Parallel make jobs (default: nproc)
  --lto / --no-lto     Enable/disable LTO (default: enabled)
  --keep-old-builds    Keep other /root/qemu-* source/build directories
  --force-rebuild      Rebuild even if this version is already installed
  --skip-deps          Do not run apt-get
  --dry-run            Print the plan without changing anything
  --lang auto|zh|en    Output language (default: auto)
  -h, --help           Show this help
EOF
}

run_usage_zh() {
    cat <<'EOF'
用法:
  sandbox.sh run [选项]

  -d, --distro NAME            发行版别名（默认 debian；--list-distros 查看）
      --list-distros           列出支持的发行版别名、版本和镜像地址
  -i, --image PATH             使用已有镜像作为基底
      --url URL                覆盖基底下载地址
  -c, --cpus N                 vCPU 数（默认 min(4, nproc)）
  -m, --mem MiB                guest 内存 MiB（默认 min(8192, 可用-1024)）
  -s, --disk SIZE              guest 磁盘大小（默认剩余空间 90%；K/M/G/T）
      --user-data PATH         cloud-init user-data（默认 ~/user-data.yaml）
      --seed PATH              seed 镜像（默认 /data/seed.img）
      --disk-path PATH         工作盘（默认 /data/disk.img）
      --keep-disk              启动现有工作盘，不重置
      --fresh-image            启动前重新下载基底镜像
      --net-backend passt|user 网络后端（默认 passt）
      --accel kvm|tcg          加速器（默认自动检测）
      --aio io_uring|threads|native
      --ssh-port PORT          主机 SSH 端口（默认 8022）
      --http-port PORT         主机 HTTP 端口（默认 8080）
      --https-port PORT        主机 HTTPS 端口（默认 8443）
      --bind-addr ADDR         转发端口绑定地址（默认 IPv4+IPv6 全部）
      --ssh-user USER          登录提示中的用户（默认 dev）
      --ssh-key PATH           登录提示中的私钥
      --fqdn FQDN              cloud-init FQDN（默认 kebyn.cc）
      --profile dev|minimal    预装配置（默认 dev；minimal 跳过开发工具）
      --provision-url URL      provision.sh 地址（默认 GitHub raw）
      --foreground             管道模式下把 QEMU stdin 接到 /dev/tty
      --background             后台运行并等待 SSH
      --wait-timeout SEC       后台启动超时（默认 600）
      --recreate-seed          强制重建 seed.img
      --stop                   正常关闭运行中的 VM
      --dry-run                只打印 QEMU 命令
      --qemu-bin PATH          QEMU 可执行文件（默认 /usr/local/bin/qemu-system-x86_64）
      --lang auto|zh|en        输出语言（默认 auto）
  -h, --help
EOF
}

run_usage_en() {
    cat <<'EOF'
Usage:
  sandbox.sh run [options]

  -d, --distro NAME            Distro alias (default: debian; see --list-distros)
      --list-distros           List supported distro aliases, versions and image URLs
  -i, --image PATH             Use an existing image as the base
      --url URL                Override the base image URL
  -c, --cpus N                 vCPUs (default: min(4, nproc))
  -m, --mem MiB                Guest RAM in MiB (default: min(8192, available-1024))
  -s, --disk SIZE              Guest disk size (default: 90% of free space; K/M/G/T suffix)
      --user-data PATH         cloud-init user-data (default: ~/user-data.yaml)
      --seed PATH              cloud-init seed image (default: /data/seed.img)
      --disk-path PATH         Work disk (default: /data/disk.img)
      --keep-disk              Boot the existing work disk instead of resetting it
      --fresh-image            Re-download the base image before starting
      --net-backend passt|user Network backend (default: passt)
      --accel kvm|tcg          Accelerator (default: auto-detect)
      --aio io_uring|threads|native
      --ssh-port PORT          Host SSH port (default: 8022)
      --http-port PORT         Host HTTP port (default: 8080)
      --https-port PORT        Host HTTPS port (default: 8443)
      --bind-addr ADDR         Bind forwarded ports to ADDR (default: all IPv4+IPv6)
      --ssh-user USER          User shown in the login hint (default: dev)
      --ssh-key PATH           Private key shown in the login hint
      --fqdn FQDN              cloud-init FQDN (default: kebyn.cc)
      --profile dev|minimal    Provisioning profile (default: dev; minimal skips dev tools)
      --provision-url URL      provision.sh URL (default: GitHub raw)
      --foreground             Attach QEMU stdin to /dev/tty in pipe mode
      --background             Run QEMU as a daemon and wait for SSH
      --wait-timeout SEC       Background startup timeout (default: 600)
      --recreate-seed          Rebuild seed.img even if it is current
      --stop                   Request a clean ACPI poweroff of the running VM
      --dry-run                Print the QEMU command without changing anything
      --qemu-bin PATH          QEMU binary (default: /usr/local/bin/qemu-system-x86_64)
      --lang auto|zh|en        Output language (default: auto)
  -h, --help
EOF
}

stop_usage_zh() {
    cat <<'EOF'
用法:
  sandbox.sh stop [--lang auto|zh|en]

通过 QEMU monitor 请求 guest 正常关机。
EOF
}

stop_usage_en() {
    cat <<'EOF'
Usage:
  sandbox.sh stop [--lang auto|zh|en]

Request a clean guest shutdown through the QEMU monitor.
EOF
}

usage() {
    if [ "$LANG_CODE" = zh ]; then usage_zh; else usage_en; fi
}

bootstrap_usage() {
    if [ "$LANG_CODE" = zh ]; then bootstrap_usage_zh; else bootstrap_usage_en; fi
}

run_usage() {
    if [ "$LANG_CODE" = zh ]; then run_usage_zh; else run_usage_en; fi
}

stop_usage() {
    if [ "$LANG_CODE" = zh ]; then stop_usage_zh; else stop_usage_en; fi
}

# ---------------------------------------------------------------------------
# Detect whether the script is a file (local mode) or a pipe (curl mode).
# ---------------------------------------------------------------------------
SELF="${BASH_SOURCE[0]:-}"
if [ -n "$SELF" ] && [ -f "$SELF" ]; then
    SCRIPT_DIR="$(cd -- "$(dirname -- "$SELF")" && pwd)"
else
    SCRIPT_DIR="$PWD"
fi

# Keep this in sync with user-data.yaml.tmpl. It is the fallback for
#   curl .../sandbox.sh | bash
write_embedded_user_data() {
    cat <<'EOF'
## template: jinja
#cloud-config
{% set sandbox_shell = '/bin/sh' if v1.distro == 'alpine' else '/bin/bash' %}
users:
  - name: dev
    shell: "{{ sandbox_shell }}"
    lock_passwd: false
    passwd: "*"
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - __SSH_PUBKEY__
ssh_pwauth: false
disable_root: true
package_update: false

hostname: local
fqdn: "{{ ds.meta_data.sandbox.fqdn }}"
preserve_hostname: false
create_hostname_file: true
prefer_fqdn_over_hostname: false
manage_etc_hosts: true

write_files:
  - path: /etc/ssh/sshd_config.d/99-sandbox-hardening.conf
    permissions: '0644'
    content: |
      PasswordAuthentication no
      KbdInteractiveAuthentication no
      PubkeyAuthentication yes
      PermitRootLogin no
  - path: /etc/sysctl.d/99-sandbox.conf
    permissions: '0644'
    content: |
      vm.swappiness = 10
      vm.max_map_count = 262144
      fs.inotify.max_user_watches = 524288
      fs.inotify.max_user_instances = 512
      net.core.somaxconn = 65535
      net.ipv4.ip_forward = 1
  - path: /etc/modules-load.d/sandbox.conf
    permissions: '0644'
    content: |
      br_netfilter
      overlay
  - path: /etc/sandbox-profile
    permissions: '0644'
    content: "{{ ds.meta_data.sandbox.profile }}"
# __PROVISION_FILE__

runcmd:
  - [ "{{ sandbox_shell }}", "-c", 'if command -v systemctl >/dev/null 2>&1; then systemctl restart systemd-modules-load.service 2>/dev/null || true; sysctl --system 2>/dev/null || true; else for m in $(cat /etc/modules-load.d/sandbox.conf 2>/dev/null); do modprobe "$m" 2>/dev/null || true; done; sysctl -p /etc/sysctl.d/99-sandbox.conf 2>/dev/null || true; fi' ]
  - [ "{{ sandbox_shell }}", "-c", 'sysctl -w net.bridge.bridge-nf-call-iptables=1 2>/dev/null || true; sysctl -w net.bridge.bridge-nf-call-ip6tables=1 2>/dev/null || true; sysctl -w net.core.default_qdisc=fq 2>/dev/null || true; sysctl -w net.ipv4.tcp_congestion_control=bbr 2>/dev/null || true' ]
  - [ "{{ sandbox_shell }}", "-c", '{% if v1.distro == "alpine" %}rc-service sshd reload{% else %}systemctl reload ssh 2>/dev/null || systemctl reload sshd{% endif %}' ]
  - [ "{{ sandbox_shell }}", "-c", 'if [ "$(cat /etc/sandbox-profile 2>/dev/null)" = dev ]; then {{ sandbox_shell }} /usr/local/sbin/sandbox-provision; fi' ]
EOF
}

# ---------------------------------------------------------------------------
# Shared helpers.
# ---------------------------------------------------------------------------
parse_size() {
    local value="$1" n u
    if ! [[ "$value" =~ ^([0-9]+)([KkMmGgTt])?$ ]]; then
        die "$(t '无效大小' 'invalid size'): $value ($(t '使用字节或 K/M/G/T 后缀' 'use bytes or a K/M/G/T suffix'))"
    fi
    n="${BASH_REMATCH[1]}"
    u="${BASH_REMATCH[2]:-}"
    case "$u" in
        K|k) n=$((n * 1024)) ;;
        M|m) n=$((n * 1024 * 1024)) ;;
        G|g) n=$((n * 1024 * 1024 * 1024)) ;;
        T|t) n=$((n * 1024 * 1024 * 1024 * 1024)) ;;
    esac
    printf '%s\n' "$n"
}

human_size() {
    numfmt --to=iec-i --suffix=B "$1" 2>/dev/null || printf '%s bytes\n' "$1"
}

stop_via_monitor() {
    python3 - "$1" <<'PY'
import socket
import sys
import time

sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.settimeout(3)
sock.connect(sys.argv[1])
time.sleep(0.2)
try:
    sock.recv(65536)
except OSError:
    pass
sock.sendall(b"system_powerdown\n")
time.sleep(0.5)
sock.close()
PY
}

img_format() {
    "$QEMU_IMG" info "$1" 2>/dev/null | sed -n 's/^file format: //p' | head -1
}

img_virtual_size() {
    "$QEMU_IMG" info "$1" 2>/dev/null \
        | sed -n 's/^virtual size: .* (\([0-9][0-9]*\) bytes)$/\1/p' \
        | head -1
}

probe_host() {
    local host="$BIND_ADDR"
    if [ -z "$host" ]; then
        host="127.0.0.1"
    fi
    case "$host" in
        0.0.0.0) host="127.0.0.1" ;;
        ::) host="::1" ;;
    esac
    printf '%s\n' "$host"
}

print_ssh_hint() {
    local host cmd
    host="$(probe_host)"
    cmd="ssh -p ${SSH_PORT}"
    if [ -n "$SSH_KEY" ]; then
        cmd+=" -i ${SSH_KEY}"
    fi
    cmd+=" ${SSH_USER}@${host}"
    printf '%s\n' "$cmd"
}

wait_for_ssh_port() {
    local i pid host
    host="$(probe_host)"
    for ((i = 0; i < WAIT_TIMEOUT; i += 2)); do
        # A successful authenticated connection is the strongest readiness
        # signal: it covers both host keys and the cloud-init user.
        if [ -n "$SSH_KEY" ] && [ -f "$SSH_KEY" ]; then
            if ssh -i "$SSH_KEY" -p "$SSH_PORT" \
                -o BatchMode=yes -o StrictHostKeyChecking=no \
                -o UserKnownHostsFile=/dev/null -o ConnectTimeout=3 \
                -o LogLevel=ERROR "$SSH_USER@$host" true >/dev/null 2>&1; then
                sleep 5
                if ssh -i "$SSH_KEY" -p "$SSH_PORT" \
                    -o BatchMode=yes -o StrictHostKeyChecking=no \
                    -o UserKnownHostsFile=/dev/null -o ConnectTimeout=3 \
                    -o LogLevel=ERROR "$SSH_USER@$host" true >/dev/null 2>&1; then
                    return 0
                fi
            fi
        fi
        # ssh-keyscan performs a real SSH handshake, so it does not report a
        # half-open forward before sshd is listening. sshd still restarts
        # during cloud-init, so require a second success after a short wait.
        if ssh-keyscan -T 2 -p "$SSH_PORT" "$host" >/dev/null 2>&1; then
            sleep 6
            if ssh-keyscan -T 2 -p "$SSH_PORT" "$host" >/dev/null 2>&1; then
                return 0
            fi
        fi
        if [ -f "$VM_DIR/qemu.pid" ]; then
            pid="$(cat "$VM_DIR/qemu.pid" 2>/dev/null || true)"
            if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
                return 1
            fi
        fi
        sleep 2
    done
    return 1
}

do_stop() {
    local pid
    if [ ! -f "$VM_DIR/qemu.pid" ]; then
        die "$(t '找不到 pidfile' 'no pidfile at'): $VM_DIR/qemu.pid"
    fi
    pid="$(cat "$VM_DIR/qemu.pid" 2>/dev/null || true)"
    if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
        rm -f "$VM_DIR/qemu.pid" "$VM_DIR/monitor.sock"
        die "$(t '没有运行中的 VM（已清理失效 pidfile）' 'no running VM (stale pidfile removed)')"
    fi
    log "$(t '正在请求 ACPI 关机，pid' 'requesting ACPI poweroff for pid') $pid"
    if [ -S "$VM_DIR/monitor.sock" ] && command -v python3 >/dev/null 2>&1; then
        if ! stop_via_monitor "$VM_DIR/monitor.sock" 2>/dev/null; then
            warn "$(t 'monitor 命令失败；发送 SIGTERM' 'monitor command failed; sending SIGTERM')"
            kill -TERM "$pid" 2>/dev/null || true
        fi
    else
        warn "$(t 'monitor socket 不可用；发送 SIGTERM' 'monitor socket unavailable; sending SIGTERM')"
        kill -TERM "$pid" 2>/dev/null || true
    fi
    for _ in $(seq 1 60); do
        if ! kill -0 "$pid" 2>/dev/null; then
            break
        fi
        sleep 1
    done
    if kill -0 "$pid" 2>/dev/null; then
        warn "$(t 'guest 60 秒内没有关机；发送 SIGTERM' 'guest did not power off in 60s; sending SIGTERM')"
        kill -TERM "$pid" 2>/dev/null || true
        for _ in $(seq 1 10); do
            if ! kill -0 "$pid" 2>/dev/null; then
                break
            fi
            sleep 1
        done
    fi
    if kill -0 "$pid" 2>/dev/null; then
        warn "$(t 'VM 仍在运行；发送 SIGKILL' 'still running; sending SIGKILL')"
        kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f "$VM_DIR/qemu.pid" "$VM_DIR/monitor.sock"
    log "$(t 'VM 已停止' 'VM stopped')"
}

# ---------------------------------------------------------------------------
# bootstrap
# ---------------------------------------------------------------------------
bootstrap_installed_ok() {
    [ -x "$BOOT_QEMU_BIN" ] || return 1
    "$BOOT_QEMU_BIN" --version 2>/dev/null | grep -Eq "version ${BOOT_VER}([^0-9]|$)" || return 1
    "$BOOT_QEMU_BIN" -netdev help 2>&1 | grep -qi passt || return 1
}

bootstrap_main() {
    BOOT_QEMU_VERSION="${QEMU_VERSION:-v11.1.2}"
    BOOT_JOBS="${JOBS:-$(nproc)}"
    BOOT_LTO="${LTO:-1}"
    BOOT_KEEP_OLD_BUILDS=0
    BOOT_FORCE_REBUILD=0
    BOOT_SKIP_DEPS=0
    BOOT_DRY_RUN=0

    while [ $# -gt 0 ]; do
        case "$1" in
            --qemu-version) BOOT_QEMU_VERSION="${2:?missing value for --qemu-version}"; shift 2 ;;
            --jobs)         BOOT_JOBS="${2:?missing value for --jobs}"; shift 2 ;;
            --lto)          BOOT_LTO=1; shift ;;
            --no-lto)       BOOT_LTO=0; shift ;;
            --keep-old-builds) BOOT_KEEP_OLD_BUILDS=1; shift ;;
            --force-rebuild)   BOOT_FORCE_REBUILD=1; shift ;;
            --skip-deps)    BOOT_SKIP_DEPS=1; shift ;;
            --dry-run)      BOOT_DRY_RUN=1; shift ;;
            --lang)         resolve_lang "${2:?missing value for --lang}"; shift 2 ;;
            --lang=*)       resolve_lang "${1#*=}"; shift ;;
            -h|--help)      bootstrap_usage; exit 0 ;;
            *) die "$(t '未知选项' 'unknown option'): $1" ;;
        esac
    done

    if ! [[ "$BOOT_JOBS" =~ ^[1-9][0-9]*$ ]]; then
        die "$(t '--jobs 必须是正整数' '--jobs must be a positive integer')"
    fi

    if [ "$BOOT_QEMU_VERSION" = "latest" ]; then
        BOOT_TAG=""
        if command -v git >/dev/null 2>&1; then
            BOOT_TAG="$(git ls-remote --tags --refs https://gitlab.com/qemu-project/qemu.git 'v*' 2>/dev/null \
                | awk -F/ '{print $NF}' \
                | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
                | sort -V | tail -1 || true)"
        fi
        if [ -z "$BOOT_TAG" ]; then
            BOOT_TAG="$(curl -fsSL 'https://gitlab.com/api/v4/projects/qemu-project%2Fqemu/repository/tags?per_page=100' 2>/dev/null \
                | grep -o '"name":"v[0-9][0-9.]*"' \
                | sed 's/.*"name":"//; s/"$//' \
                | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
                | sort -V | tail -1 || true)"
        fi
        [ -n "$BOOT_TAG" ] || die "$(t '无法解析最新的 QEMU tag' 'could not resolve the latest QEMU tag')"
    else
        BOOT_TAG="v${BOOT_QEMU_VERSION#v}"
    fi

    BOOT_VER="${BOOT_TAG#v}"
    BOOT_SRC_DIR="/root/qemu-${BOOT_VER}"
    BOOT_TARBALL="/root/qemu-${BOOT_VER}.tar.gz"
    BOOT_ARCHIVE_URL="https://gitlab.com/qemu-project/qemu/-/archive/${BOOT_TAG}/qemu-${BOOT_TAG}.tar.gz"
    BOOT_QEMU_BIN="/usr/local/bin/qemu-system-x86_64"

    local -a boot_pkgs=(
        build-essential pkg-config ninja-build git curl ca-certificates tar
        python3 python3-venv
        libglib2.0-dev libfdt-dev libpixman-1-dev zlib1g-dev
        libslirp-dev libaio-dev liburing-dev libseccomp-dev
        libcap-ng-dev libattr1-dev libnuma-dev
        passt cloud-image-utils openssh-client qemu-utils
    )

    local -a boot_conf=(
        --target-list=x86_64-softmmu --enable-strip --disable-docs --disable-werror
    )
    if [ "$BOOT_LTO" -eq 1 ]; then
        boot_conf=(--enable-lto "${boot_conf[@]}")
    fi

    if [ "$BOOT_DRY_RUN" -eq 1 ]; then
        log "$(t 'QEMU tag:' 'QEMU tag:') $BOOT_TAG"
        log "$(t '源码/构建:' 'source/build:') $BOOT_SRC_DIR"
        log "$(t '安装前缀:' 'install prefix:') /usr/local (default)"
        log "$(t '压缩包:' 'archive:') $BOOT_ARCHIVE_URL"
        log "$(t 'configure:' 'configure:') ${boot_conf[*]}"
        log "$(t 'make:' 'make:') make -j$BOOT_JOBS && make install"
        log "$(t 'apt 软件包:' 'apt packages:') ${boot_pkgs[*]}"
        if [ "$BOOT_KEEP_OLD_BUILDS" -eq 0 ]; then
            log "$(t '清理:' 'cleanup:') $(t '安装成功后删除其它 /root/qemu-* 构建' 'remove other /root/qemu-* builds after a successful install')"
        fi
        exit 0
    fi

    [ "$(id -u)" -eq 0 ] || die "$(t '需要 root（apt 和安装到 /usr/local）' "must run as root (apt and install to /usr/local)")"

    if [ "$BOOT_SKIP_DEPS" -eq 0 ]; then
        log "$(t '正在安装编译依赖' 'installing build dependencies')"
        apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${boot_pkgs[@]}"
    fi

    if [ "$BOOT_FORCE_REBUILD" -eq 0 ] && bootstrap_installed_ok; then
        log "$(t "QEMU $BOOT_VER 已安装在 $BOOT_QEMU_BIN；跳过编译" "QEMU $BOOT_VER is already installed at $BOOT_QEMU_BIN; skipping build")"
    else
        if [ ! -x "$BOOT_SRC_DIR/configure" ]; then
            log "$(t '正在下载' 'downloading') $BOOT_ARCHIVE_URL"
            curl -fL --retry 3 --remove-on-error -o "$BOOT_TARBALL" "$BOOT_ARCHIVE_URL"
            rm -rf "$BOOT_SRC_DIR"
            mkdir -p "$BOOT_SRC_DIR"
            log "$(t '正在解压到' 'extracting to') $BOOT_SRC_DIR"
            tar -xzf "$BOOT_TARBALL" -C "$BOOT_SRC_DIR" --strip-components=1
        fi

        if [ "$BOOT_FORCE_REBUILD" -eq 1 ]; then
            rm -rf "$BOOT_SRC_DIR/build"
        fi

        mkdir -p "$BOOT_SRC_DIR/build"
        cd "$BOOT_SRC_DIR/build"
        if [ ! -f config-host.mak ]; then
            log "$(t "正在配置 QEMU $BOOT_VER" "configuring QEMU $BOOT_VER")"
            "$BOOT_SRC_DIR/configure" "${boot_conf[@]}"
        fi

        log "$(t "正在编译 QEMU $BOOT_VER，$BOOT_JOBS 个任务" "building QEMU $BOOT_VER with $BOOT_JOBS jobs")"
        make -j"$BOOT_JOBS"
        log "$(t '正在安装到 /usr/local' 'installing to /usr/local')"
        make install

        "$BOOT_QEMU_BIN" --version
        "$BOOT_QEMU_BIN" -netdev help 2>&1 | grep -qi passt \
            || die "$(t '编译出的 QEMU 不支持 passt netdev 后端' 'the built QEMU does not advertise the passt netdev backend')"
    fi

    if [ "$BOOT_KEEP_OLD_BUILDS" -eq 0 ]; then
        shopt -s nullglob
        for dir in /root/qemu-*; do
            [ -d "$dir" ] || continue
            [ "$dir" = "$BOOT_SRC_DIR" ] && continue
            case "$(basename "$dir")" in
                qemu-[0-9]*) log "$(t '删除旧构建' 'removing old build') $dir"; rm -rf -- "$dir" ;;
            esac
        done
        for file in /root/qemu-*.tar.gz; do
            [ -f "$file" ] || continue
            [ "$file" = "$BOOT_TARBALL" ] && continue
            case "$(basename "$file")" in
                qemu-[0-9]*) log "$(t '删除旧压缩包' 'removing old tarball') $file"; rm -f -- "$file" ;;
            esac
        done
        shopt -u nullglob
    fi

    local version
    version="$("$BOOT_QEMU_BIN" --version | head -1)"
    log "$(t '完成:' 'done:') $version"
}

# ---------------------------------------------------------------------------
# Distro registry and listing.
# ---------------------------------------------------------------------------
distro_info() {
    local name="$1"
    DISTRO_FORMAT=qcow2
    case "$name" in
        debian|debian-13)
            DISTRO_LABEL="Debian 13"
            DISTRO_ALIASES="debian, debian-13"
            DISTRO_URL="https://cloud.debian.org/images/cloud/trixie/latest/debian-13-generic-amd64.qcow2"
            ;;
        ubuntu|ubuntu-24.04)
            DISTRO_LABEL="Ubuntu 24.04 LTS"
            DISTRO_ALIASES="ubuntu, ubuntu-24.04"
            DISTRO_URL="https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
            ;;
        ubuntu-26.04)
            DISTRO_LABEL="Ubuntu 26.04 LTS"
            DISTRO_ALIASES="ubuntu-26.04"
            DISTRO_URL="https://cloud-images.ubuntu.com/resolute/current/resolute-server-cloudimg-amd64.img"
            ;;
        rocky|rocky-10)
            DISTRO_LABEL="Rocky Linux 10"
            DISTRO_ALIASES="rocky, rocky-10"
            DISTRO_URL="https://dl.rockylinux.org/pub/rocky/10/images/x86_64/Rocky-10-GenericCloud-Base.latest.x86_64.qcow2"
            ;;
        alma|almalinux|alma-10)
            DISTRO_LABEL="AlmaLinux 10"
            DISTRO_ALIASES="alma, almalinux, alma-10"
            DISTRO_URL="https://repo.almalinux.org/almalinux/10/cloud/x86_64/images/AlmaLinux-10-GenericCloud-latest.x86_64.qcow2"
            ;;
        centos|centos-10)
            DISTRO_LABEL="CentOS Stream 10"
            DISTRO_ALIASES="centos, centos-10"
            DISTRO_URL="https://cloud.centos.org/centos/10-stream/x86_64/images/CentOS-Stream-GenericCloud-10-latest.x86_64.qcow2"
            ;;
        fedora|fedora-44)
            DISTRO_LABEL="Fedora 44"
            DISTRO_ALIASES="fedora, fedora-44"
            DISTRO_URL="https://download.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2"
            ;;
        opensuse|leap|leap-16.0)
            DISTRO_LABEL="openSUSE Leap 16.0"
            DISTRO_ALIASES="opensuse, leap, leap-16.0"
            DISTRO_URL="https://download.opensuse.org/distribution/leap/16.0/appliances/Leap-16.0-Minimal-VM.x86_64-Cloud.qcow2"
            ;;
        tumbleweed)
            DISTRO_LABEL="openSUSE Tumbleweed"
            DISTRO_ALIASES="tumbleweed"
            DISTRO_URL="https://download.opensuse.org/tumbleweed/appliances/openSUSE-Tumbleweed-Minimal-VM.x86_64-Cloud.qcow2"
            ;;
        arch|archlinux)
            DISTRO_LABEL="Arch Linux"
            DISTRO_ALIASES="arch, archlinux"
            DISTRO_URL="https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2"
            ;;
        alpine|alpine-3.23)
            DISTRO_LABEL="Alpine 3.23"
            DISTRO_ALIASES="alpine, alpine-3.23"
            DISTRO_URL="https://dl-cdn.alpinelinux.org/alpine/v3.23/releases/cloud/nocloud_alpine-3.23.4-x86_64-bios-cloudinit-r0.qcow2"
            ;;
        *) return 1 ;;
    esac
    return 0
}

list_distros() {
    local alias
    printf '%-30s %-20s %s\n' "$(t '别名' 'ALIAS')" "$(t '版本' 'VERSION')" "$(t '镜像地址' 'URL')"
    for alias in debian ubuntu ubuntu-26.04 rocky alma centos fedora opensuse tumbleweed arch alpine; do
        if distro_info "$alias"; then
            printf '%-30s %-20s %s\n' "$DISTRO_ALIASES" "$DISTRO_LABEL" "$DISTRO_URL"
        fi
    done
}

validate_user_data() {
    local file="$1" tmpd
    if ! command -v cloud-init >/dev/null 2>&1; then
        return 0
    fi
    if head -n 1 "$file" 2>/dev/null | grep -q '## template: jinja'; then
        if ! command -v python3 >/dev/null 2>&1 \
            || ! python3 -c 'import jinja2' >/dev/null 2>&1; then
            warn "$(t 'Jinja2 不可用，跳过 user-data schema 校验' 'Jinja2 unavailable; skipping user-data schema validation')"
            return 0
        fi
        tmpd="$(mktemp -d)"
        if ! python3 - "$file" "$tmpd" "$FQDN" "$PROFILE" "$PROVISION_URL" <<'PY'
import pathlib
import sys

import jinja2

src, outdir, fqdn, profile, url = sys.argv[1:6]
text = pathlib.Path(src).read_text()
env = jinja2.Environment(undefined=jinja2.StrictUndefined)
for distro in ("debian", "alpine"):
    ctx = {
        "v1": {"distro": distro},
        "ds": {
            "meta_data": {
                "sandbox": {
                    "fqdn": fqdn,
                    "profile": profile,
                    "provision_url": url,
                }
            }
        },
    }
    rendered = env.from_string(text).render(**ctx)
    lines = rendered.splitlines()
    if lines and lines[0].startswith("## template: jinja"):
        lines = lines[1:]
    pathlib.Path(outdir, f"user-data-{distro}.yaml").write_text("\n".join(lines) + "\n")
PY
        then
            rm -rf "$tmpd"
            die "$(t 'user-data 模板渲染失败' 'user-data template rendering failed'): $file"
        fi
        if ! cloud-init schema -c "$tmpd/user-data-debian.yaml" >/dev/null \
            || ! cloud-init schema -c "$tmpd/user-data-alpine.yaml" >/dev/null; then
            rm -rf "$tmpd"
            die "$(t 'user-data 未通过 cloud-init schema 校验' 'user-data failed cloud-init schema validation'): $USER_DATA"
        fi
        rm -rf "$tmpd"
    else
        if ! cloud-init schema -c "$file" >/dev/null; then
            die "$(t 'user-data 未通过 cloud-init schema 校验' 'user-data failed cloud-init schema validation'): $USER_DATA"
        fi
    fi
}

# ---------------------------------------------------------------------------
# run
# ---------------------------------------------------------------------------
run_main() {
    DISTRO="${VM_DISTRO:-debian}"
    LIST_DISTROS=0
    CPUS="${VM_CPUS:-}"
    MEM_MIB="${VM_MEM_MIB:-}"
    DISK_SPEC="${VM_DISK:-}"
    DISK_SET=0
    IMAGE="${VM_IMAGE:-}"
    URL="${VM_URL:-}"
    USER_DATA="${VM_USER_DATA:-$HOME/user-data.yaml}"
    SEED="${VM_SEED:-$VM_DIR/seed.img}"
    DISK="${VM_DISK_PATH:-$VM_DIR/disk.img}"
    FRESH_IMAGE=0
    KEEP_DISK=0
    NET_BACKEND="${VM_NET_BACKEND:-passt}"
    NET_BACKEND_SET=0
    ACCEL="${VM_ACCEL:-auto}"
    AIO="${VM_AIO:-io_uring}"
    SSH_PORT="${VM_SSH_PORT:-8022}"
    HTTP_PORT="${VM_HTTP_PORT:-8080}"
    HTTPS_PORT="${VM_HTTPS_PORT:-8443}"
    BIND_ADDR="${VM_BIND_ADDR:-}"
    SSH_USER="${VM_SSH_USER:-dev}"
    SSH_KEY="${VM_SSH_KEY:-}"
    FQDN="${VM_FQDN:-kebyn.cc}"
    PROFILE="${VM_PROFILE:-dev}"
    PROVISION_URL="${VM_PROVISION_URL:-https://raw.githubusercontent.com/kebyn/sandbox/main/provision.sh}"
    FOREGROUND=0
    TTY_STDIN=0
    BACKGROUND=0
    WAIT_TIMEOUT="${VM_WAIT_TIMEOUT:-600}"
    RECREATE_SEED=0
    DRY_RUN=0
    STOP=0
    MTU="${VM_MTU:-1450}"
    QEMU_BIN="${QEMU_BIN:-}"
    QEMU_IMG="${QEMU_IMG:-}"

    while [ $# -gt 0 ]; do
        case "$1" in
            -d|--distro)      DISTRO="${2:?missing value for --distro}"; shift 2 ;;
            --list-distros)   LIST_DISTROS=1; shift ;;
            -i|--image)       IMAGE="${2:?missing value for --image}"; shift 2 ;;
            --url)            URL="${2:?missing value for --url}"; shift 2 ;;
            -c|--cpus)        CPUS="${2:?missing value for --cpus}"; shift 2 ;;
            -m|--mem)         MEM_MIB="${2:?missing value for --mem}"; shift 2 ;;
            -s|--disk)        DISK_SPEC="${2:?missing value for --disk}"; DISK_SET=1; shift 2 ;;
            --user-data)      USER_DATA="${2:?missing value for --user-data}"; shift 2 ;;
            --seed)           SEED="${2:?missing value for --seed}"; shift 2 ;;
            --disk-path)      DISK="${2:?missing value for --disk-path}"; shift 2 ;;
            --keep-disk)      KEEP_DISK=1; shift ;;
            --fresh-image)    FRESH_IMAGE=1; shift ;;
            --net-backend)    NET_BACKEND="${2:?missing value for --net-backend}"; NET_BACKEND_SET=1; shift 2 ;;
            --accel)          ACCEL="${2:?missing value for --accel}"; shift 2 ;;
            --aio)            AIO="${2:?missing value for --aio}"; shift 2 ;;
            --ssh-port)       SSH_PORT="${2:?missing value for --ssh-port}"; shift 2 ;;
            --http-port)      HTTP_PORT="${2:?missing value for --http-port}"; shift 2 ;;
            --https-port)     HTTPS_PORT="${2:?missing value for --https-port}"; shift 2 ;;
            --bind-addr)      BIND_ADDR="${2:?missing value for --bind-addr}"; shift 2 ;;
            --ssh-user)       SSH_USER="${2:?missing value for --ssh-user}"; shift 2 ;;
            --ssh-key)        SSH_KEY="${2:?missing value for --ssh-key}"; shift 2 ;;
            --fqdn)           FQDN="${2:?missing value for --fqdn}"; shift 2 ;;
            --profile)        PROFILE="${2:?missing value for --profile}"; shift 2 ;;
            --provision-url)  PROVISION_URL="${2:?missing value for --provision-url}"; shift 2 ;;
            --foreground)     FOREGROUND=1; shift ;;
            --background)     BACKGROUND=1; shift ;;
            --wait-timeout)   WAIT_TIMEOUT="${2:?missing value for --wait-timeout}"; shift 2 ;;
            --recreate-seed)  RECREATE_SEED=1; shift ;;
            --stop)           STOP=1; shift ;;
            --dry-run)        DRY_RUN=1; shift ;;
            --qemu-bin)       QEMU_BIN="${2:?missing value for --qemu-bin}"; shift 2 ;;
            --lang)           resolve_lang "${2:?missing value for --lang}"; shift 2 ;;
            --lang=*)         resolve_lang "${1#*=}"; shift ;;
            -h|--help)        run_usage; exit 0 ;;
            *) die "$(t '未知选项' 'unknown option'): $1" ;;
        esac
    done

    if [ "$LIST_DISTROS" -eq 1 ]; then
        list_distros
        exit 0
    fi

    if [ "$STOP" -eq 1 ]; then
        do_stop
        exit 0
    fi

    if [ "$FOREGROUND" -eq 1 ] && [ "$BACKGROUND" -eq 1 ]; then
        die "$(t '--foreground 和 --background 不能同时使用' '--foreground and --background cannot be used together')"
    fi

    if [ "$FOREGROUND" -eq 1 ] && [ "$DRY_RUN" -eq 0 ] && [ ! -t 0 ]; then
        if [ -r /dev/tty ] && [ -t 1 ]; then
            TTY_STDIN=1
        else
            die "$(t '--foreground 需要控制终端（/dev/tty）和终端输出；改用 --background 或 bash <(curl ...)' '--foreground requires a controlling terminal (/dev/tty) and terminal output; use --background or bash <(curl ...)')"
        fi
    fi

    if [ "$BACKGROUND" -eq 0 ] && [ "$DRY_RUN" -eq 0 ] && [ "$FOREGROUND" -eq 0 ] && [ ! -t 0 ]; then
        BACKGROUND=1
        log "$(t '标准输入不是终端，自动切换到后台模式' 'stdin is not a TTY; switching to background mode')"
    fi

    case "$PROFILE" in
        dev|minimal) ;;
        *) die "$(t '--profile 必须是 dev 或 minimal' '--profile must be dev or minimal')" ;;
    esac
    if [ -z "$FQDN" ]; then
        die "$(t '--fqdn 不能为空' '--fqdn must not be empty')"
    fi
    if [ "$PROFILE" = dev ] && [ -z "$PROVISION_URL" ]; then
        die "$(t 'dev profile 需要 --provision-url' 'dev profile requires --provision-url')"
    fi

    if ! distro_info "$DISTRO"; then
        if [ -n "$IMAGE" ] || [ -n "$URL" ]; then
            DISTRO_LABEL="$DISTRO"
            DISTRO_ALIASES="$DISTRO"
            DISTRO_URL=""
            DISTRO_FORMAT=qcow2
        else
            die "$(t '不支持的发行版' 'unsupported distro'): $DISTRO ($(t '使用 --list-distros 查看' 'use --list-distros'))"
        fi
    fi
    DEFAULT_URL="$DISTRO_URL"
    DEFAULT_FORMAT="$DISTRO_FORMAT"

    case "$NET_BACKEND" in passt|user) ;; *) die "$(t '--net-backend 必须是 passt 或 user' '--net-backend must be passt or user')" ;; esac
    case "$ACCEL" in auto|kvm|tcg) ;; *) die "$(t '--accel 必须是 auto、kvm 或 tcg' '--accel must be auto, kvm or tcg')" ;; esac
    case "$AIO" in io_uring|threads|native) ;; *) die "$(t '--aio 必须是 io_uring、threads 或 native' '--aio must be io_uring, threads or native')" ;; esac

    if [ -n "$IMAGE" ] && [ -n "$URL" ]; then
        die "$(t '--image 和 --url 不能同时使用' '--image and --url cannot be used together')"
    fi
    if [ -n "$IMAGE" ] && [ "$FRESH_IMAGE" -eq 1 ]; then
        die "$(t '--fresh-image 只适用于下载的基底镜像' '--fresh-image only applies to downloaded base images')"
    fi

    for port in "$SSH_PORT" "$HTTP_PORT" "$HTTPS_PORT"; do
        if ! [[ "$port" =~ ^[0-9]+$ ]] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
            die "$(t '无效端口' 'invalid port'): $port"
        fi
    done
    if [ "$SSH_PORT" = "$HTTP_PORT" ] || [ "$SSH_PORT" = "$HTTPS_PORT" ] || [ "$HTTP_PORT" = "$HTTPS_PORT" ]; then
        die "$(t '转发端口不能重复' 'forwarded ports must be distinct')"
    fi

    if [ -z "$QEMU_BIN" ]; then
        if [ -x /usr/local/bin/qemu-system-x86_64 ]; then
            QEMU_BIN=/usr/local/bin/qemu-system-x86_64
        else
            QEMU_BIN="$(command -v qemu-system-x86_64 || true)"
        fi
    fi
    if [ -z "$QEMU_BIN" ] || [ ! -x "$QEMU_BIN" ]; then
        die "$(t '找不到 qemu-system-x86_64；先运行 sandbox.sh bootstrap' 'qemu-system-x86_64 not found; run sandbox.sh bootstrap first')"
    fi

    if [ -z "$QEMU_IMG" ]; then
        if [ -x /usr/local/bin/qemu-img ]; then
            QEMU_IMG=/usr/local/bin/qemu-img
        else
            QEMU_IMG="$(command -v qemu-img || true)"
        fi
    fi
    if [ -z "$QEMU_IMG" ] || [ ! -x "$QEMU_IMG" ]; then
        die "$(t '找不到 qemu-img；先运行 sandbox.sh bootstrap' 'qemu-img not found; run sandbox.sh bootstrap first')"
    fi

    for tool in cloud-localds curl ssh-keygen ssh-keyscan df numfmt sha256sum cmp; do
        if ! command -v "$tool" >/dev/null 2>&1; then
            die "$(t '缺少工具' 'missing required tool'): $tool ($(t '运行 sandbox.sh bootstrap' 'run sandbox.sh bootstrap'))"
        fi
    done
    if ! command -v genisoimage >/dev/null 2>&1 \
        && ! command -v xorriso >/dev/null 2>&1 \
        && ! command -v mkisofs >/dev/null 2>&1; then
        die "$(t 'cloud-localds 需要 genisoimage、xorriso 或 mkisofs' 'cloud-localds needs genisoimage, xorriso or mkisofs')"
    fi

    if [ "$ACCEL" = auto ]; then
        if [ -r /dev/kvm ] && [ -w /dev/kvm ] \
            && "$QEMU_BIN" -accel help 2>/dev/null | grep -qw kvm; then
            ACCEL=kvm
        else
            ACCEL=tcg
        fi
    fi
    if [ "$ACCEL" = kvm ]; then
        if [ ! -r /dev/kvm ] || [ ! -w /dev/kvm ]; then
            die "$(t '/dev/kvm 不可写；使用 --accel tcg' '/dev/kvm is not writable; use --accel tcg')"
        fi
        if ! "$QEMU_BIN" -accel help 2>/dev/null | grep -qw kvm; then
            die "$(t '此 QEMU 构建没有 KVM 加速器；使用 --accel tcg' 'this QEMU build has no KVM accelerator; use --accel tcg')"
        fi
        CPU_MODEL=host
    else
        CPU_MODEL=max
        warn "$(t 'TCG 模式比 KVM 慢很多' 'TCG mode is much slower than KVM')"
    fi

    HAS_PASST_QEMU=0
    if "$QEMU_BIN" -netdev help 2>&1 | grep -qi passt; then
        HAS_PASST_QEMU=1
    fi
    HAS_PASST_BIN=0
    if command -v passt >/dev/null 2>&1; then
        HAS_PASST_BIN=1
    fi
    if [ "$NET_BACKEND" = passt ] && { [ "$HAS_PASST_QEMU" -eq 0 ] || [ "$HAS_PASST_BIN" -eq 0 ]; }; then
        if [ "$NET_BACKEND_SET" -eq 1 ]; then
            die "$(t 'passt 后端不可用' 'passt backend is unavailable') (qemu: $HAS_PASST_QEMU, passt: $HAS_PASST_BIN)"
        fi
        warn "$(t 'passt 后端不可用；回退到 QEMU user 网络' 'passt backend is unavailable; falling back to QEMU user networking')"
        NET_BACKEND=user
    fi

    if [ -z "$CPUS" ]; then
        CPUS="$(nproc)"
        if [ "$CPUS" -gt 4 ]; then
            CPUS=4
        fi
    fi
    if ! [[ "$CPUS" =~ ^[1-9][0-9]*$ ]]; then
        die "$(t '--cpus 必须是正整数' '--cpus must be a positive integer')"
    fi

    if [ -z "$MEM_MIB" ]; then
        avail_kb="$(awk '/^MemAvailable:/ {print $2; exit}' /proc/meminfo)"
        [ -n "$avail_kb" ] || die "$(t '无法从 /proc/meminfo 读取 MemAvailable' 'cannot read MemAvailable from /proc/meminfo')"
        mem=$((avail_kb / 1024 - 1024))
        if [ "$mem" -gt 8192 ]; then
            mem=8192
        fi
        if [ "$mem" -lt 1024 ]; then
            die "$(t '可用内存不足 1 GiB；释放内存或使用 --mem' 'less than 1 GiB of memory available; free memory or use --mem')"
        fi
        MEM_MIB=$((mem / 128 * 128))
    fi
    if ! [[ "$MEM_MIB" =~ ^[1-9][0-9]*$ ]]; then
        die "$(t '--mem 必须是正整数（MiB）' '--mem must be a positive integer (MiB)')"
    fi

    mkdir -p "$VM_DIR" "$(dirname "$SEED")" "$(dirname "$DISK")"

    if [ -n "$IMAGE" ]; then
        BASE="$IMAGE"
        if [ ! -s "$BASE" ]; then
            die "$(t '镜像不存在' 'image not found'): $BASE"
        fi
    else
        URL="${URL:-$DEFAULT_URL}"
        name="${URL##*/}"
        name="${name%%\?*}"
        if [ -z "$name" ]; then
            die "$(t '无法从 URL 推导镜像文件名' 'cannot derive an image name from URL'): $URL"
        fi
        BASE="$VM_DIR/images/$name"
    fi

    if [ "$BASE" = "$DISK" ]; then
        KEEP_DISK=1
    fi

    if [ "$DISK_SET" -eq 1 ]; then
        DISK_BYTES="$(parse_size "$DISK_SPEC")"
    elif [ "$KEEP_DISK" -eq 1 ]; then
        DISK_BYTES=""
    else
        avail_bytes="$(df -B1 --output=avail "$VM_DIR" | tail -1 | tr -d '[:space:]')"
        if ! [[ "$avail_bytes" =~ ^[0-9]+$ ]]; then
            die "$(t '无法确定可用空间' 'cannot determine free space for'): $VM_DIR"
        fi
        DISK_BYTES=$((avail_bytes * 90 / 100))
        DISK_BYTES=$((DISK_BYTES / 1073741824 * 1073741824))
        if [ "$DISK_BYTES" -lt 1073741824 ]; then
            die "$(t '可用空间不足 1 GiB 给 guest 磁盘；使用 --disk 或释放空间' 'less than 1 GiB free for the guest disk; use --disk or free space')"
        fi
    fi

    build_netdev() {
        local p gp
        NETDEV=""
        if [ "$NET_BACKEND" = passt ]; then
            NETDEV="passt,id=net,vhost-user=on,quiet=off"
            # QEMU joins repeated tcp-ports options into one comma-separated
            # argument for passt; passt accepts the bind address only once, at
            # the start of that list.
            if [ -n "$BIND_ADDR" ]; then
                NETDEV+=",tcp-ports=${BIND_ADDR}/${SSH_PORT}:22"
            else
                NETDEV+=",tcp-ports=${SSH_PORT}:22"
            fi
            NETDEV+=",tcp-ports=${HTTP_PORT}:80"
            NETDEV+=",tcp-ports=${HTTPS_PORT}:443"
            NET_DEVICE="virtio-net-pci,netdev=net,mq=on,host_mtu=${MTU}"
        else
            NETDEV="user,id=net"
            for pair in "${SSH_PORT}:22" "${HTTP_PORT}:80" "${HTTPS_PORT}:443"; do
                p="${pair%%:*}"
                gp="${pair##*:}"
                if [ -n "$BIND_ADDR" ]; then
                    NETDEV+=",hostfwd=tcp:${BIND_ADDR}:${p}-:${gp}"
                else
                    NETDEV+=",hostfwd=tcp::${p}-:${gp}"
                fi
            done
            NET_DEVICE="virtio-net-pci,netdev=net,host_mtu=${MTU}"
        fi
    }

    build_qemu_args() {
        QEMU_ARGS=(
            -name cloud-vm
            -machine "q35,accel=${ACCEL}"
            -cpu "${CPU_MODEL}"
            -smp "${CPUS},sockets=1,cores=${CPUS},threads=1"
            -m "${MEM_MIB}"
            -object "memory-backend-memfd,id=mem,size=${MEM_MIB}M,share=on,prealloc=on"
            -numa "node,memdev=mem"
            -object "iothread,id=iothread"
            -device "virtio-blk-pci,drive=hd0,iothread=iothread"
            -drive "file=${DISK},format=${DISK_FORMAT},cache=none,discard=on,detect-zeroes=on,aio=${AIO},if=none,id=hd0"
            -cdrom "${SEED}"
            -device "${NET_DEVICE}"
            -netdev "${NETDEV}"
            -rtc "base=utc,driftfix=slew"
            -pidfile "${VM_DIR}/qemu.pid"
        )
        if [ "$BACKGROUND" -eq 1 ]; then
            QEMU_ARGS+=(
                -display none
                -serial "file:${VM_DIR}/console.log"
                -monitor "unix:${VM_DIR}/monitor.sock,server=on,wait=off"
                -daemonize
            )
        else
            QEMU_ARGS+=(-nographic -serial mon:stdio)
        fi
    }

    if [ "$DRY_RUN" -eq 1 ]; then
        DISK_FORMAT="$DEFAULT_FORMAT"
        if [ -s "$BASE" ]; then
            detected="$(img_format "$BASE" || true)"
            if [ -n "$detected" ]; then
                DISK_FORMAT="$detected"
            fi
        fi
        build_netdev
        build_qemu_args
        log "$(t '基底镜像:' 'base image:') $BASE"
        log "$(t '工作盘:' 'work disk:') $DISK ($(t '格式:' 'format:') $DISK_FORMAT)"
        log "$(t 'seed:' 'seed:') $SEED"
        if [ -n "$DISK_BYTES" ]; then
            log "$(t '磁盘大小:' 'disk size:') $(human_size "$DISK_BYTES")"
        else
            log "$(t '磁盘大小:' 'disk size:') $(t '保留现有' 'keep existing')"
        fi
        log "$(t '资源:' 'resources:') ${CPUS} vCPU, ${MEM_MIB} MiB RAM, ${ACCEL}, ${NET_BACKEND}"
        if [ "$KEEP_DISK" -eq 1 ]; then
            reset_text="$(t '否' 'no')"
        else
            reset_text="$(t '是' 'yes')"
        fi
        log "$(t '重置磁盘:' 'reset disk:') $reset_text"
        printf '%s\n' "$(t 'qemu 命令:' 'qemu command:')"
        printf '%q ' "$QEMU_BIN" "${QEMU_ARGS[@]}"
        printf '\n'
        exit 0
    fi

    if [ -f "$VM_DIR/qemu.pid" ]; then
        pid="$(cat "$VM_DIR/qemu.pid" 2>/dev/null || true)"
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            die "$(t '已有 VM 在运行，pid' 'a VM is already running with pid') $pid ($(t '若已失效请删除' 'remove if stale'): $VM_DIR/qemu.pid)"
        fi
        rm -f "$VM_DIR/qemu.pid"
    fi

    check_port_free() {
        local port="$1"
        if command -v ss >/dev/null 2>&1 && ss -ltnH "sport = :${port}" 2>/dev/null | grep -q .; then
            printf 'sandbox: %s %s\n' "$(t '主机端口已被占用:' 'host port is already in use:')" "$port" >&2
            ss -ltnp "sport = :${port}" >&2 || true
            return 1
        fi
        return 0
    }

    for port in "$SSH_PORT" "$HTTP_PORT" "$HTTPS_PORT"; do
        if ! check_port_free "$port"; then
            die "$(t '用 --ssh-port/--http-port/--https-port 换端口' 'choose another port with --ssh-port/--http-port/--https-port')"
        fi
    done

    if [ -z "$IMAGE" ]; then
        mkdir -p "$(dirname "$BASE")"
        if [ "$FRESH_IMAGE" -eq 1 ] && [ -f "$BASE" ]; then
            log "$(t '删除缓存的基底镜像:' 'removing cached base image:') $BASE"
            rm -f "$BASE"
        fi
        if [ ! -s "$BASE" ]; then
            log "$(t '正在下载' 'downloading') $URL"
            tmp="${BASE}.part"
            rm -f "$tmp"
            curl -fL --retry 3 --remove-on-error -o "$tmp" "$URL"
            mv "$tmp" "$BASE"
        fi
    fi

    BASE_FORMAT="$(img_format "$BASE")"
    if [ -z "$BASE_FORMAT" ]; then
        die "$(t '无法读取镜像元数据' 'cannot read image metadata'): $BASE"
    fi
    if [ "$BASE_FORMAT" = qcow2 ]; then
        if ! "$QEMU_IMG" check "$BASE" >/dev/null 2>&1; then
            warn "$(t 'qemu-img check 报告镜像有问题' 'qemu-img check reported issues in') $BASE"
        fi
    fi

    if [ "$KEEP_DISK" -eq 0 ]; then
        log "$(t '正在用基底重置工作盘（用 --keep-disk 保留状态）' 'resetting work disk from base (use --keep-disk to preserve state)')"
        rm -f "$DISK"
        cp --reflink=auto --sparse=always -- "$BASE" "$DISK"
    else
        if [ ! -s "$DISK" ]; then
            die "$(t '指定了 --keep-disk，但工作盘不存在' '--keep-disk requested but work disk does not exist'): $DISK"
        fi
        log "$(t '保留现有工作盘:' 'keeping existing work disk:') $DISK"
    fi

    DISK_FORMAT="$(img_format "$DISK")"
    if [ -z "$DISK_FORMAT" ]; then
        die "$(t '无法读取工作盘元数据' 'cannot read work disk metadata'): $DISK"
    fi

    if [ -n "$DISK_BYTES" ]; then
        current_bytes="$(img_virtual_size "$DISK")"
        if [ -z "$current_bytes" ]; then
            die "$(t '无法读取虚拟大小' 'cannot read virtual size of') $DISK"
        fi
        if [ "$DISK_BYTES" -gt "$current_bytes" ]; then
            log "$(t '正在把工作盘扩容到' 'resizing work disk to') $(human_size "$DISK_BYTES")"
            "$QEMU_IMG" resize -f "$DISK_FORMAT" "$DISK" "$DISK_BYTES"
        elif [ "$DISK_BYTES" -lt "$current_bytes" ] && [ "$DISK_SET" -eq 1 ]; then
            die "$(t '拒绝缩小工作盘' 'refusing to shrink') $DISK ($(t '当前:' 'current:') $(human_size "$current_bytes"))"
        fi
    fi

    if [ ! -s "$USER_DATA" ]; then
        log "$(t '正在从模板生成' 'creating') $USER_DATA"
        mkdir -p "$(dirname "$USER_DATA")"
        if [ ! -f "$VM_DIR/id_ed25519" ]; then
            ssh-keygen -q -t ed25519 -N '' -f "$VM_DIR/id_ed25519" -C cloud-vm
        fi
        pub="$(cat "$VM_DIR/id_ed25519.pub")"
        template="$SCRIPT_DIR/user-data.yaml.tmpl"
        template_tmp=0
        if [ ! -f "$template" ]; then
            template="$(mktemp)"
            write_embedded_user_data > "$template"
            template_tmp=1
        fi

        provision_file="$SCRIPT_DIR/provision.sh"
        provision_tmp=0
        provision_block_tmp=0
        if [ "$PROFILE" = dev ]; then
            if [ ! -f "$provision_file" ]; then
                provision_file="$(mktemp)"
                if ! curl -fsSL --retry 3 "$PROVISION_URL" -o "$provision_file"; then
                    rm -f "$provision_file"
                    if [ "$template_tmp" -eq 1 ]; then
                        rm -f "$template"
                    fi
                    die "$(t '无法下载 provision.sh' 'cannot download provision.sh'): $PROVISION_URL"
                fi
                provision_tmp=1
            fi
            provision_block="$(mktemp)"
            provision_block_tmp=1
            {
                printf '  - path: /usr/local/sbin/sandbox-provision\n'
                printf "    permissions: '0755'\n"
                printf '    content: |\n'
                sed 's/^/      /' "$provision_file"
            } > "$provision_block"
        else
            provision_block=/dev/null
        fi

        rendered="$(mktemp)"
        tmp="$(mktemp)"
        sed "s|__SSH_PUBKEY__|${pub}|g" "$template" > "$rendered"
        awk -v block="$provision_block" '
            /^# __PROVISION_FILE__$/ {
                while ((getline line < block) > 0) print line
                close(block)
                next
            }
            { print }
        ' "$rendered" > "$tmp"
        install -m 600 "$tmp" "$USER_DATA"
        rm -f "$tmp" "$rendered"
        if [ "$provision_block_tmp" -eq 1 ]; then
            rm -f "$provision_block"
        fi
        if [ "$template_tmp" -eq 1 ]; then
            rm -f "$template"
        fi
        if [ "$provision_tmp" -eq 1 ]; then
            rm -f "$provision_file"
        fi
        if [ -z "$SSH_KEY" ]; then
            SSH_KEY="$VM_DIR/id_ed25519"
        fi
    fi

    if [ -z "$SSH_KEY" ] && [ -f "$VM_DIR/id_ed25519" ] && [ -s "$VM_DIR/id_ed25519.pub" ]; then
        if grep -qF "$(cat "$VM_DIR/id_ed25519.pub")" "$USER_DATA" 2>/dev/null; then
            SSH_KEY="$VM_DIR/id_ed25519"
        fi
    fi

    validate_user_data "$USER_DATA"

    hash="$(printf '%s\n%s\n%s\n%s\n' "$(cat "$USER_DATA")" "$FQDN" "$PROFILE" "$PROVISION_URL" \
        | sha256sum | awk '{print substr($1, 1, 12)}')"
    meta_tmp="$(mktemp)"
    printf 'instance-id: iid-%s\nlocal-hostname: local\nsandbox:\n  fqdn: "%s"\n  profile: "%s"\n  provision_url: "%s"\n' \
        "$hash" "$FQDN" "$PROFILE" "$PROVISION_URL" > "$meta_tmp"
    if ! cmp -s "$meta_tmp" "$VM_DIR/meta-data" 2>/dev/null; then
        mv "$meta_tmp" "$VM_DIR/meta-data"
    else
        rm -f "$meta_tmp"
    fi

    need_seed=0
    if [ ! -s "$SEED" ]; then
        need_seed=1
    fi
    if [ "$USER_DATA" -nt "$SEED" ]; then
        need_seed=1
    fi
    if [ "$VM_DIR/meta-data" -nt "$SEED" ]; then
        need_seed=1
    fi
    if [ "$RECREATE_SEED" -eq 1 ]; then
        need_seed=1
    fi
    if [ "$need_seed" -eq 1 ]; then
        log "$(t '正在生成 cloud-init seed:' 'building cloud-init seed:') $SEED"
        cloud-localds "$SEED" "$USER_DATA" "$VM_DIR/meta-data"
    fi

    build_netdev
    build_qemu_args

    log "$(t '正在启动 QEMU:' 'starting QEMU:') ${CPUS} vCPU, ${MEM_MIB} MiB RAM, ${ACCEL}, ${NET_BACKEND}, disk ${DISK}"
    printf '%s %s\n' "$(t '登录:' 'login:')" "$(print_ssh_hint)"

    if [ "$BACKGROUND" -eq 1 ]; then
        : > "$VM_DIR/console.log"
        rm -f "$VM_DIR/monitor.sock"
        "$QEMU_BIN" "${QEMU_ARGS[@]}"
        if ! wait_for_ssh_port; then
            printf 'sandbox: %s %ss\n' "$(t 'VM 在以下时间内仍不可达:' 'VM did not become reachable within')" "$WAIT_TIMEOUT" >&2
            tail -n 40 "$VM_DIR/console.log" >&2 || true
            die "$(t '启动超时；使用 sandbox.sh stop 停止' 'startup timed out; use sandbox.sh stop to stop it')"
        fi
        log "$(t 'VM 已启动（控制台:' 'VM is up (console:') $VM_DIR/console.log)"
    else
        printf '%s\n' "$(t '按 Ctrl-A X 退出。' 'Press Ctrl-A X to quit.')"
        if [ "$TTY_STDIN" -eq 1 ]; then
            log "$(t '把 QEMU stdin 接到 /dev/tty' 'attaching QEMU stdin to /dev/tty')"
            exec "$QEMU_BIN" "${QEMU_ARGS[@]}" < /dev/tty
        else
            exec "$QEMU_BIN" "${QEMU_ARGS[@]}"
        fi
    fi
}

# ---------------------------------------------------------------------------
# stop
# ---------------------------------------------------------------------------
stop_main() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --lang) resolve_lang "${2:?missing value for --lang}"; shift 2 ;;
            --lang=*) resolve_lang "${1#*=}"; shift ;;
            -h|--help) stop_usage; exit 0 ;;
            *) die "$(t '未知选项' 'unknown option'): $1" ;;
        esac
    done
    do_stop
}

# ---------------------------------------------------------------------------
# Pre-scan --lang so help and errors use the requested language regardless of
# whether --lang appears before or after the subcommand.
# ---------------------------------------------------------------------------
LANG_OPT="$VM_LANG"
argv=("$@")
for ((i = 0; i < ${#argv[@]}; i++)); do
    case "${argv[$i]}" in
        --lang)
            if [ $((i + 1)) -lt ${#argv[@]} ]; then
                LANG_OPT="${argv[$((i + 1))]}"
            fi
            ;;
        --lang=*) LANG_OPT="${argv[$i]#*=}" ;;
    esac
done
resolve_lang "$LANG_OPT"

# ---------------------------------------------------------------------------
# Dispatch.
# ---------------------------------------------------------------------------
while [ $# -gt 0 ]; do
    case "$1" in
        --lang) resolve_lang "${2:?missing value for --lang}"; shift 2 ;;
        --lang=*) resolve_lang "${1#*=}"; shift ;;
        -h|--help) usage; exit 0 ;;
        bootstrap|run|stop) break ;;
        *) die "$(t '未知命令' 'unknown command'): $1" ;;
    esac
done

if [ $# -eq 0 ]; then
    usage
    exit 0
fi

command_name="$1"
shift

case "$command_name" in
    bootstrap) bootstrap_main "$@" ;;
    run)       run_main "$@" ;;
    stop)      stop_main "$@" ;;
    *)         die "$(t '未知命令' 'unknown command'): $command_name" ;;
esac
