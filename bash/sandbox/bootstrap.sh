#!/usr/bin/env bash
# Build QEMU from source on a fresh Debian/Ubuntu host.
# Installs to the default prefix (/usr/local) so run-vm.sh can find it.
set -Eeuo pipefail

QEMU_VERSION="${QEMU_VERSION:-v11.1.2}"
JOBS="${JOBS:-$(nproc)}"
LTO="${LTO:-1}"
KEEP_OLD_BUILDS=0
FORCE_REBUILD=0
SKIP_DEPS=0
DRY_RUN=0

die() { printf 'bootstrap: %s\n' "$*" >&2; exit 1; }
log() { printf '==> %s\n' "$*"; }

usage() {
    cat <<'EOF'
Usage: ./bootstrap.sh [options]

  --qemu-version TAG   QEMU tag to build (default: v11.1.2; "latest" resolves the newest tag)
  --jobs N             Parallel make jobs (default: nproc)
  --lto / --no-lto     Enable/disable LTO (default: enabled, matching the original run)
  --keep-old-builds    Keep other /root/qemu-* source/build directories
  --force-rebuild      Rebuild even if this version is already installed
  --skip-deps          Do not run apt-get
  --dry-run            Print the plan without changing anything
  -h, --help           Show this help
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --qemu-version) QEMU_VERSION="${2:?missing value for --qemu-version}"; shift 2 ;;
        --jobs)         JOBS="${2:?missing value for --jobs}"; shift 2 ;;
        --lto)          LTO=1; shift ;;
        --no-lto)       LTO=0; shift ;;
        --keep-old-builds) KEEP_OLD_BUILDS=1; shift ;;
        --force-rebuild)   FORCE_REBUILD=1; shift ;;
        --skip-deps)    SKIP_DEPS=1; shift ;;
        --dry-run)      DRY_RUN=1; shift ;;
        -h|--help)      usage; exit 0 ;;
        *) die "unknown option: $1" ;;
    esac
done

[[ "$JOBS" =~ ^[1-9][0-9]*$ ]] || die "--jobs must be a positive integer"

# Resolve a tag. QEMU tags look like v11.1.2; accept either "11.1.2" or "v11.1.2".
if [ "$QEMU_VERSION" = "latest" ]; then
    TAG=""
    if command -v git >/dev/null 2>&1; then
        TAG="$(git ls-remote --tags --refs https://gitlab.com/qemu-project/qemu.git 'v*' 2>/dev/null \
            | awk -F/ '{print $NF}' \
            | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
            | sort -V | tail -1 || true)"
    fi
    if [ -z "$TAG" ]; then
        TAG="$(curl -fsSL 'https://gitlab.com/api/v4/projects/qemu-project%2Fqemu/repository/tags?per_page=100' 2>/dev/null \
            | grep -o '"name":"v[0-9][0-9.]*"' \
            | sed 's/.*"name":"//; s/"$//' \
            | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
            | sort -V | tail -1 || true)"
    fi
    [ -n "$TAG" ] || die "could not resolve the latest QEMU tag"
else
    TAG="v${QEMU_VERSION#v}"
fi

VER="${TAG#v}"
SRC_DIR="/root/qemu-${VER}"
TARBALL="/root/qemu-${VER}.tar.gz"
ARCHIVE_URL="https://gitlab.com/qemu-project/qemu/-/archive/${TAG}/qemu-${TAG}.tar.gz"
QEMU_BIN="/usr/local/bin/qemu-system-x86_64"

APT_PKGS=(
    build-essential pkg-config ninja-build git curl ca-certificates tar
    python3 python3-venv
    libglib2.0-dev libfdt-dev libpixman-1-dev zlib1g-dev
    libslirp-dev libaio-dev liburing-dev libseccomp-dev
    libcap-ng-dev libattr1-dev libnuma-dev
    passt cloud-image-utils openssh-client qemu-utils
)

CONF_OPTS=(--target-list=x86_64-softmmu --enable-strip --disable-docs --disable-werror)
if [ "$LTO" -eq 1 ]; then
    CONF_OPTS=(--enable-lto "${CONF_OPTS[@]}")
fi

installed_ok() {
    [ -x "$QEMU_BIN" ] || return 1
    "$QEMU_BIN" --version 2>/dev/null | grep -Eq "version ${VER}([^0-9]|$)" || return 1
    "$QEMU_BIN" -netdev help 2>&1 | grep -qi passt || return 1
}

if [ "$DRY_RUN" -eq 1 ]; then
    log "QEMU tag:        $TAG"
    log "source/build:    $SRC_DIR"
    log "install prefix:  /usr/local (default)"
    log "archive:         $ARCHIVE_URL"
    log "configure:       ${CONF_OPTS[*]}"
    log "make:            make -j$JOBS && make install"
    log "apt packages:    ${APT_PKGS[*]}"
    if [ "$KEEP_OLD_BUILDS" -eq 0 ]; then
        log "cleanup:         remove other /root/qemu-* builds after a successful install"
    fi
    exit 0
fi

[ "$(id -u)" -eq 0 ] || die "must run as root (apt and 'make install' to /usr/local)"

if [ "$SKIP_DEPS" -eq 0 ]; then
    log "installing build dependencies"
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${APT_PKGS[@]}"
fi

if [ "$FORCE_REBUILD" -eq 0 ] && installed_ok; then
    log "QEMU $VER is already installed at $QEMU_BIN; skipping build"
else
    if [ ! -x "$SRC_DIR/configure" ]; then
        log "downloading $ARCHIVE_URL"
        curl -fL --retry 3 --remove-on-error -o "$TARBALL" "$ARCHIVE_URL"
        rm -rf "$SRC_DIR"
        mkdir -p "$SRC_DIR"
        log "extracting to $SRC_DIR"
        tar -xzf "$TARBALL" -C "$SRC_DIR" --strip-components=1
    fi

    if [ "$FORCE_REBUILD" -eq 1 ]; then
        rm -rf "$SRC_DIR/build"
    fi

    mkdir -p "$SRC_DIR/build"
    cd "$SRC_DIR/build"
    if [ ! -f config-host.mak ]; then
        log "configuring QEMU $VER"
        "$SRC_DIR/configure" "${CONF_OPTS[@]}"
    fi

    log "building QEMU $VER with $JOBS jobs"
    make -j"$JOBS"
    log "installing to /usr/local"
    make install

    "$QEMU_BIN" --version
    "$QEMU_BIN" -netdev help 2>&1 | grep -qi passt \
        || die "the built QEMU does not advertise the passt netdev backend"
fi

if [ "$KEEP_OLD_BUILDS" -eq 0 ]; then
    shopt -s nullglob
    for dir in /root/qemu-*; do
        [ -d "$dir" ] || continue
        [ "$dir" = "$SRC_DIR" ] && continue
        case "$(basename "$dir")" in
            qemu-[0-9]*) log "removing old build $dir"; rm -rf -- "$dir" ;;
        esac
    done
    for file in /root/qemu-*.tar.gz; do
        [ -f "$file" ] || continue
        [ "$file" = "$TARBALL" ] && continue
        case "$(basename "$file")" in
            qemu-[0-9]*) log "removing old tarball $file"; rm -f -- "$file" ;;
        esac
    done
    shopt -u nullglob
fi

log "done: $("$QEMU_BIN" --version | head -1)"
