#!/bin/sh
# Provision a sandbox VM: common tools, Docker, Node.js LTS and uv.
set -eu
set -x

LOG=/var/log/sandbox-provision.log
DONE=/var/log/sandbox-provision.done
rm -f "$DONE"
exec >>"$LOG" 2>&1
echo "=== sandbox provision $(date -u) ==="

fail() {
    echo "FAILED: $*"
    exit 1
}

# shellcheck disable=SC1091
. /etc/os-release
case " ${ID:-} ${ID_LIKE:-} " in
    *debian*|*ubuntu*)                              FAMILY=debian ;;
    *rhel*|*fedora*|*centos*|*rocky*|*almalinux*)   FAMILY=rhel ;;
    *suse*)                                         FAMILY=suse ;;
    *arch*)                                         FAMILY=arch ;;
    *alpine*)                                       FAMILY=alpine ;;
    *) fail "unsupported distro: ${ID:-unknown}" ;;
esac
echo "family=$FAMILY id=${ID:-} like=${ID_LIKE:-}"

# ---------------------------------------------------------------------------
# Common tools.
# ---------------------------------------------------------------------------
case "$FAMILY" in
    debian)
        apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get install -y \
            ca-certificates curl git jq build-essential python3 python3-pip
        ;;
    rhel)
        dnf install -y ca-certificates curl git jq gcc gcc-c++ make \
            python3 python3-pip dnf-plugins-core
        ;;
    suse)
        zypper --non-interactive install ca-certificates curl git jq \
            gcc gcc-c++ make python3 python3-pip
        ;;
    arch)
        pacman -Sy --noconfirm ca-certificates curl git jq sudo base-devel python python-pip
        ;;
    alpine)
        apk add --no-cache ca-certificates curl git jq sudo build-base python3 py3-pip
        ;;
esac

# ---------------------------------------------------------------------------
# Docker.
# ---------------------------------------------------------------------------
if ! command -v docker >/dev/null 2>&1; then
    case "$FAMILY" in
        debian)
            curl -fsSL --retry 3 https://get.docker.com -o /tmp/get-docker.sh
            sh /tmp/get-docker.sh
            ;;
        rhel)
            case " ${ID:-} " in
                *fedora*) DOCKER_REPO=https://download.docker.com/linux/fedora/docker-ce.repo ;;
                *)        DOCKER_REPO=https://download.docker.com/linux/centos/docker-ce.repo ;;
            esac
            dnf config-manager --add-repo "$DOCKER_REPO"
            dnf install -y docker-ce docker-ce-cli containerd.io \
                docker-buildx-plugin docker-compose-plugin
            # Docker CE may install a newer kernel; ensure the running kernel
            # has the netfilter modules needed by dockerd.
            dnf install -y "kernel-modules-extra-$(uname -r)" \
                "kernel-modules-$(uname -r)" || true
            modprobe xt_addrtype 2>/dev/null || true
            modprobe iptable_nat 2>/dev/null || true
            modprobe nf_nat 2>/dev/null || true
            ;;
        suse)
            zypper --non-interactive install docker
            ;;
        arch)
            pacman -Sy --noconfirm docker docker-compose
            ;;
        alpine)
            apk add --no-cache docker docker-cli-compose
            ;;
    esac
fi

case "$FAMILY" in
    alpine)
        # Docker on Alpine needs cgroup v2 mounted in unified mode.
        sed -i 's/^#rc_cgroup_mode="unified"/rc_cgroup_mode="unified"/' /etc/rc.conf 2>/dev/null || true
        rm -f /run/openrc/starting/cgroups /run/openrc/stopping/cgroups
        rc-update add cgroups default 2>/dev/null || true
        service cgroups start 2>/dev/null || true
        if ! grep -q ' /sys/fs/cgroup ' /proc/mounts; then
            mkdir -p /sys/fs/cgroup
            mount -t cgroup2 none /sys/fs/cgroup 2>/dev/null || true
        fi
        if ! grep -q ' /sys/fs/cgroup ' /proc/mounts; then
            fail "cgroup2 is not mounted; Docker cannot start"
        fi
        rc-update add docker default 2>/dev/null || true
        service docker start
        addgroup dev docker 2>/dev/null || true
        ;;
    *)
        systemctl enable --now docker
        usermod -aG docker dev
        ;;
esac

# ---------------------------------------------------------------------------
# Node.js LTS.
# ---------------------------------------------------------------------------
case "$FAMILY" in
    debian)
        if curl -fsSL --retry 3 https://deb.nodesource.com/setup_lts.x -o /tmp/nodesource.sh \
            && bash /tmp/nodesource.sh; then
            apt-get install -y nodejs
        else
            echo "NodeSource failed; falling back to distro nodejs"
            apt-get install -y nodejs npm
        fi
        ;;
    rhel)
        if curl -fsSL --retry 3 https://rpm.nodesource.com/setup_lts.x -o /tmp/nodesource.sh \
            && bash /tmp/nodesource.sh; then
            dnf install -y nodejs
        else
            echo "NodeSource failed; falling back to distro nodejs"
            dnf install -y nodejs npm
        fi
        ;;
    suse)
        zypper --non-interactive install nodejs npm
        ;;
    arch)
        pacman -Sy --noconfirm nodejs-lts npm
        ;;
    alpine)
        apk add --no-cache nodejs npm
        ;;
esac

# ---------------------------------------------------------------------------
# uv.
# ---------------------------------------------------------------------------
if ! command -v uv >/dev/null 2>&1; then
    if curl -LsSf --retry 3 https://astral.sh/uv/install.sh -o /tmp/uv-install.sh; then
        UV_INSTALL_DIR=/usr/local/bin sh /tmp/uv-install.sh \
            || sh /tmp/uv-install.sh \
            || true
    fi
    if ! command -v uv >/dev/null 2>&1 && [ -x /root/.local/bin/uv ]; then
        ln -sf /root/.local/bin/uv /usr/local/bin/uv
    fi
fi

# ---------------------------------------------------------------------------
# Verify.
# ---------------------------------------------------------------------------
for cmd in docker node npm uv; do
    command -v "$cmd" >/dev/null 2>&1 || fail "missing $cmd"
done

sleep 2
docker --version
docker compose version || true
node --version
npm --version
uv --version
docker info >/dev/null 2>&1 || fail "docker daemon not ready"

touch "$DONE"
echo "=== provision complete ==="
