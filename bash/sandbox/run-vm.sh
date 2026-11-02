#!/usr/bin/env bash
# Launch a Debian/Rocky cloud image with QEMU, KVM, passt and cloud-init.
set -Eeuo pipefail

VM_DIR="${VM_DIR:-/data}"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

DISTRO="${VM_DISTRO:-debian}"
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
BACKGROUND=0
WAIT_TIMEOUT="${VM_WAIT_TIMEOUT:-600}"
RECREATE_SEED=0
DRY_RUN=0
STOP=0
MTU="${VM_MTU:-1450}"
QEMU_BIN="${QEMU_BIN:-}"
QEMU_IMG="${QEMU_IMG:-}"

die() { printf 'run-vm: %s\n' "$*" >&2; exit 1; }
warn() { printf 'run-vm: warning: %s\n' "$*" >&2; }
log() { printf '==> %s\n' "$*"; }

usage() {
    cat <<'EOF'
Usage: ./run-vm.sh [options]

  -d, --distro debian|rocky     Base image (default: debian)
  -i, --image PATH              Use an existing image as the base
      --url URL                 Override the base image URL
  -c, --cpus N                  vCPUs (default: min(4, nproc))
  -m, --mem MiB                 Guest RAM in MiB (default: min(8192, available-1024))
  -s, --disk SIZE               Guest disk size (default: 90% of free space; K/M/G/T suffix)
      --user-data PATH          cloud-init user-data (default: ~/user-data.yaml)
      --seed PATH               cloud-init seed image (default: /data/seed.img)
      --disk-path PATH          Work disk (default: /data/disk.img)
      --keep-disk               Boot the existing work disk instead of resetting it
      --fresh-image             Re-download the base image before starting
      --net-backend passt|user  Network backend (default: passt)
      --accel kvm|tcg           Accelerator (default: auto-detect)
      --aio io_uring|threads|native
      --ssh-port PORT           Host SSH port (default: 8022)
      --http-port PORT          Host HTTP port (default: 8080)
      --https-port PORT         Host HTTPS port (default: 8443)
      --bind-addr ADDR          Bind forwarded ports to ADDR (default: all IPv4+IPv6)
      --ssh-user USER           User shown in the login hint (default: dev)
      --ssh-key PATH            Private key shown in the login hint
      --background              Run QEMU as a daemon and wait for SSH
      --wait-timeout SEC        Background startup timeout (default: 600)
      --recreate-seed           Rebuild seed.img even if it is current
      --stop                    Request a clean ACPI poweroff of the running VM
      --dry-run                 Print the QEMU command without changing anything
      --qemu-bin PATH           QEMU binary (default: /usr/local/bin/qemu-system-x86_64)
  -h, --help
EOF
}

parse_size() {
    local value="$1" n u
    if ! [[ "$value" =~ ^([0-9]+)([KkMmGgTt])?$ ]]; then
        die "invalid size: $value (use bytes or a K/M/G/T suffix)"
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

while [ $# -gt 0 ]; do
    case "$1" in
        -d|--distro)      DISTRO="${2:?missing value for --distro}"; shift 2 ;;
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
        --background)     BACKGROUND=1; shift ;;
        --wait-timeout)   WAIT_TIMEOUT="${2:?missing value for --wait-timeout}"; shift 2 ;;
        --recreate-seed)  RECREATE_SEED=1; shift ;;
        --stop)           STOP=1; shift ;;
        --dry-run)        DRY_RUN=1; shift ;;
        --qemu-bin)       QEMU_BIN="${2:?missing value for --qemu-bin}"; shift 2 ;;
        -h|--help)        usage; exit 0 ;;
        *) die "unknown option: $1" ;;
    esac
done

if [ "$STOP" -eq 1 ]; then
    if [ ! -f "$VM_DIR/qemu.pid" ]; then
        die "no pidfile at $VM_DIR/qemu.pid"
    fi
    pid="$(cat "$VM_DIR/qemu.pid" 2>/dev/null || true)"
    if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
        rm -f "$VM_DIR/qemu.pid" "$VM_DIR/monitor.sock"
        die "no running VM (stale pidfile removed)"
    fi
    log "requesting ACPI poweroff for pid $pid"
    if [ -S "$VM_DIR/monitor.sock" ] && command -v python3 >/dev/null 2>&1; then
        if ! stop_via_monitor "$VM_DIR/monitor.sock" 2>/dev/null; then
            warn "monitor command failed; sending SIGTERM"
            kill -TERM "$pid" 2>/dev/null || true
        fi
    else
        warn "monitor socket unavailable; sending SIGTERM"
        kill -TERM "$pid" 2>/dev/null || true
    fi
    for _ in $(seq 1 60); do
        if ! kill -0 "$pid" 2>/dev/null; then
            break
        fi
        sleep 1
    done
    if kill -0 "$pid" 2>/dev/null; then
        warn "guest did not power off in 60s; sending SIGTERM"
        kill -TERM "$pid" 2>/dev/null || true
        for _ in $(seq 1 10); do
            if ! kill -0 "$pid" 2>/dev/null; then
                break
            fi
            sleep 1
        done
    fi
    if kill -0 "$pid" 2>/dev/null; then
        warn "still running; sending SIGKILL"
        kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f "$VM_DIR/qemu.pid" "$VM_DIR/monitor.sock"
    log "VM stopped"
    exit 0
fi

case "$DISTRO" in
    debian)
        DEFAULT_URL="https://cloud.debian.org/images/cloud/trixie/latest/debian-13-generic-amd64.qcow2"
        DEFAULT_FORMAT="qcow2"
        ;;
    rocky)
        DEFAULT_URL="https://dl.rockylinux.org/pub/rocky/10/images/x86_64/Rocky-10-GenericCloud-Base.latest.x86_64.qcow2"
        DEFAULT_FORMAT="qcow2"
        ;;
    *) die "unsupported distro: $DISTRO (use debian or rocky)" ;;
esac

case "$NET_BACKEND" in passt|user) ;; *) die "--net-backend must be passt or user" ;; esac
case "$ACCEL" in auto|kvm|tcg) ;; *) die "--accel must be auto, kvm or tcg" ;; esac
case "$AIO" in io_uring|threads|native) ;; *) die "--aio must be io_uring, threads or native" ;; esac

if [ -n "$IMAGE" ] && [ -n "$URL" ]; then
    die "--image and --url cannot be used together"
fi
if [ -n "$IMAGE" ] && [ "$FRESH_IMAGE" -eq 1 ]; then
    die "--fresh-image only applies to downloaded base images"
fi

for port in "$SSH_PORT" "$HTTP_PORT" "$HTTPS_PORT"; do
    if ! [[ "$port" =~ ^[0-9]+$ ]] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
        die "invalid port: $port"
    fi
done
if [ "$SSH_PORT" = "$HTTP_PORT" ] || [ "$SSH_PORT" = "$HTTPS_PORT" ] || [ "$HTTP_PORT" = "$HTTPS_PORT" ]; then
    die "forwarded ports must be distinct"
fi

if [ -z "$QEMU_BIN" ]; then
    if [ -x /usr/local/bin/qemu-system-x86_64 ]; then
        QEMU_BIN=/usr/local/bin/qemu-system-x86_64
    else
        QEMU_BIN="$(command -v qemu-system-x86_64 || true)"
    fi
fi
if [ -z "$QEMU_BIN" ] || [ ! -x "$QEMU_BIN" ]; then
    die "qemu-system-x86_64 not found; run ./bootstrap.sh first"
fi

if [ -z "$QEMU_IMG" ]; then
    if [ -x /usr/local/bin/qemu-img ]; then
        QEMU_IMG=/usr/local/bin/qemu-img
    else
        QEMU_IMG="$(command -v qemu-img || true)"
    fi
fi
if [ -z "$QEMU_IMG" ] || [ ! -x "$QEMU_IMG" ]; then
    die "qemu-img not found; run ./bootstrap.sh first"
fi

for tool in cloud-localds curl ssh-keygen ssh-keyscan df numfmt sha256sum cmp; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        die "missing required tool: $tool (run ./bootstrap.sh)"
    fi
done
if ! command -v genisoimage >/dev/null 2>&1 \
    && ! command -v xorriso >/dev/null 2>&1 \
    && ! command -v mkisofs >/dev/null 2>&1; then
    die "cloud-localds needs genisoimage, xorriso or mkisofs (run ./bootstrap.sh)"
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
        die "/dev/kvm is not writable; use --accel tcg"
    fi
    if ! "$QEMU_BIN" -accel help 2>/dev/null | grep -qw kvm; then
        die "this QEMU build has no KVM accelerator; use --accel tcg"
    fi
    CPU_MODEL=host
else
    CPU_MODEL=max
    warn "TCG mode is much slower than KVM"
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
        die "passt backend is unavailable (qemu support: $HAS_PASST_QEMU, passt binary: $HAS_PASST_BIN)"
    fi
    warn "passt backend is unavailable; falling back to QEMU user networking"
    NET_BACKEND=user
fi

if [ -z "$CPUS" ]; then
    CPUS="$(nproc)"
    if [ "$CPUS" -gt 4 ]; then
        CPUS=4
    fi
fi
if ! [[ "$CPUS" =~ ^[1-9][0-9]*$ ]]; then
    die "--cpus must be a positive integer"
fi

if [ -z "$MEM_MIB" ]; then
    avail_kb="$(awk '/^MemAvailable:/ {print $2; exit}' /proc/meminfo)"
    [ -n "$avail_kb" ] || die "cannot read MemAvailable from /proc/meminfo"
    mem=$((avail_kb / 1024 - 1024))
    if [ "$mem" -gt 8192 ]; then
        mem=8192
    fi
    if [ "$mem" -lt 1024 ]; then
        die "less than 1 GiB of memory available; free memory or use --mem"
    fi
    MEM_MIB=$((mem / 128 * 128))
fi
if ! [[ "$MEM_MIB" =~ ^[1-9][0-9]*$ ]]; then
    die "--mem must be a positive integer (MiB)"
fi

mkdir -p "$VM_DIR" "$(dirname "$SEED")" "$(dirname "$DISK")"

if [ -n "$IMAGE" ]; then
    BASE="$IMAGE"
    if [ ! -s "$BASE" ]; then
        die "image not found: $BASE"
    fi
else
    URL="${URL:-$DEFAULT_URL}"
    name="${URL##*/}"
    name="${name%%\?*}"
    if [ -z "$name" ]; then
        die "cannot derive an image name from URL: $URL"
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
        die "cannot determine free space for $VM_DIR"
    fi
    DISK_BYTES=$((avail_bytes * 90 / 100))
    DISK_BYTES=$((DISK_BYTES / 1073741824 * 1073741824))
    if [ "$DISK_BYTES" -lt 1073741824 ]; then
        die "less than 1 GiB free for the guest disk; use --disk or free space"
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

# ---------------------------------------------------------------------------
# Dry run stops before any download, seed generation or port checks.
# ---------------------------------------------------------------------------
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
    log "base image:  $BASE"
    log "work disk:   $DISK (format: $DISK_FORMAT)"
    log "seed:        $SEED"
    if [ -n "$DISK_BYTES" ]; then
        log "disk size:   $(human_size "$DISK_BYTES")"
    else
        log "disk size:   keep existing"
    fi
    log "resources:   ${CPUS} vCPU, ${MEM_MIB} MiB RAM, ${ACCEL}, ${NET_BACKEND}"
    log "reset disk:  $([ "$KEEP_DISK" -eq 1 ] && echo no || echo yes)"
    printf 'qemu command:\n'
    printf '%q ' "$QEMU_BIN" "${QEMU_ARGS[@]}"
    printf '\n'
    exit 0
fi

# ---------------------------------------------------------------------------
# Preflight checks that must happen before touching the work disk.
# ---------------------------------------------------------------------------
if [ -f "$VM_DIR/qemu.pid" ]; then
    pid="$(cat "$VM_DIR/qemu.pid" 2>/dev/null || true)"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        die "a VM is already running with pid $pid (remove $VM_DIR/qemu.pid if stale)"
    fi
    rm -f "$VM_DIR/qemu.pid"
fi

check_port_free() {
    local port="$1"
    if command -v ss >/dev/null 2>&1 && ss -ltnH "sport = :${port}" 2>/dev/null | grep -q .; then
        printf 'run-vm: host port %s is already in use:\n' "$port" >&2
        ss -ltnp "sport = :${port}" >&2 || true
        return 1
    fi
    return 0
}

for port in "$SSH_PORT" "$HTTP_PORT" "$HTTPS_PORT"; do
    if ! check_port_free "$port"; then
        die "choose another port with --ssh-port/--http-port/--https-port"
    fi
done

# ---------------------------------------------------------------------------
# Base image and work disk.
# ---------------------------------------------------------------------------
if [ -z "$IMAGE" ]; then
    mkdir -p "$(dirname "$BASE")"
    if [ "$FRESH_IMAGE" -eq 1 ] && [ -f "$BASE" ]; then
        log "removing cached base image: $BASE"
        rm -f "$BASE"
    fi
    if [ ! -s "$BASE" ]; then
        log "downloading $URL"
        tmp="${BASE}.part"
        rm -f "$tmp"
        curl -fL --retry 3 --remove-on-error -o "$tmp" "$URL"
        mv "$tmp" "$BASE"
    fi
fi

BASE_FORMAT="$(img_format "$BASE")"
if [ -z "$BASE_FORMAT" ]; then
    die "cannot read image metadata: $BASE"
fi
if [ "$BASE_FORMAT" = qcow2 ]; then
    if ! "$QEMU_IMG" check "$BASE" >/dev/null 2>&1; then
        warn "qemu-img check reported issues in $BASE"
    fi
fi

if [ "$KEEP_DISK" -eq 0 ]; then
    log "resetting work disk from base (use --keep-disk to preserve state)"
    rm -f "$DISK"
    cp --reflink=auto --sparse=always -- "$BASE" "$DISK"
else
    if [ ! -s "$DISK" ]; then
        die "--keep-disk requested but $DISK does not exist"
    fi
    log "keeping existing work disk: $DISK"
fi

DISK_FORMAT="$(img_format "$DISK")"
if [ -z "$DISK_FORMAT" ]; then
    die "cannot read work disk metadata: $DISK"
fi

if [ -n "$DISK_BYTES" ]; then
    current_bytes="$(img_virtual_size "$DISK")"
    if [ -z "$current_bytes" ]; then
        die "cannot read virtual size of $DISK"
    fi
    if [ "$DISK_BYTES" -gt "$current_bytes" ]; then
        log "resizing work disk to $(human_size "$DISK_BYTES")"
        "$QEMU_IMG" resize -f "$DISK_FORMAT" "$DISK" "$DISK_BYTES"
    elif [ "$DISK_BYTES" -lt "$current_bytes" ] && [ "$DISK_SET" -eq 1 ]; then
        die "refusing to shrink $DISK (current: $(human_size "$current_bytes"))"
    fi
fi

# ---------------------------------------------------------------------------
# cloud-init seed.
# ---------------------------------------------------------------------------
if [ ! -f "$SCRIPT_DIR/user-data.yaml.tmpl" ]; then
    die "missing template: $SCRIPT_DIR/user-data.yaml.tmpl"
fi

if [ ! -s "$USER_DATA" ]; then
    log "creating $USER_DATA from template"
    mkdir -p "$(dirname "$USER_DATA")"
    if [ ! -f "$VM_DIR/id_ed25519" ]; then
        ssh-keygen -q -t ed25519 -N '' -f "$VM_DIR/id_ed25519" -C cloud-vm
    fi
    pub="$(cat "$VM_DIR/id_ed25519.pub")"
    tmp="$(mktemp)"
    sed "s|__SSH_PUBKEY__|${pub}|g" "$SCRIPT_DIR/user-data.yaml.tmpl" > "$tmp"
    install -m 600 "$tmp" "$USER_DATA"
    rm -f "$tmp"
    if [ -z "$SSH_KEY" ]; then
        SSH_KEY="$VM_DIR/id_ed25519"
    fi
fi

if [ -z "$SSH_KEY" ] && [ -f "$VM_DIR/id_ed25519" ] && [ -s "$VM_DIR/id_ed25519.pub" ]; then
    if grep -qF "$(cat "$VM_DIR/id_ed25519.pub")" "$USER_DATA" 2>/dev/null; then
        SSH_KEY="$VM_DIR/id_ed25519"
    fi
fi

if command -v cloud-init >/dev/null 2>&1; then
    if ! cloud-init schema -c "$USER_DATA" >/dev/null; then
        die "user-data failed cloud-init schema validation: $USER_DATA"
    fi
fi

hash="$(sha256sum "$USER_DATA" | awk '{print substr($1, 1, 12)}')"
meta_tmp="$(mktemp)"
printf 'instance-id: iid-%s\nlocal-hostname: cloud-vm\n' "$hash" > "$meta_tmp"
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
    log "building cloud-init seed: $SEED"
    cloud-localds "$SEED" "$USER_DATA" "$VM_DIR/meta-data"
fi

# ---------------------------------------------------------------------------
# Launch.
# ---------------------------------------------------------------------------
build_netdev
build_qemu_args

log "starting QEMU: ${CPUS} vCPU, ${MEM_MIB} MiB RAM, ${ACCEL}, ${NET_BACKEND}, disk ${DISK}"
printf 'login: %s\n' "$(print_ssh_hint)"

if [ "$BACKGROUND" -eq 1 ]; then
    : > "$VM_DIR/console.log"
    rm -f "$VM_DIR/monitor.sock"
    "$QEMU_BIN" "${QEMU_ARGS[@]}"
    if ! wait_for_ssh_port; then
        printf 'run-vm: VM did not become reachable within %ss\n' "$WAIT_TIMEOUT" >&2
        tail -n 40 "$VM_DIR/console.log" >&2 || true
        die "startup timed out; stop it with: kill \$(cat $VM_DIR/qemu.pid)"
    fi
    log "VM is up (console: $VM_DIR/console.log)"
else
    printf 'Press Ctrl-A X to quit.\n'
    exec "$QEMU_BIN" "${QEMU_ARGS[@]}"
fi
