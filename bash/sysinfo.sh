#!/usr/bin/env bash
#
# sysinfo.sh — offline replacement for the "Basic System Information" block of
# YABS (https://github.com/masonr/yet-another-bench-script), i.e. a local-only
# equivalent of:  curl -sL yabs.sh | bash -s -- -dig
#
# * No downloads, no network calls, no root required.
# * Needs only /proc plus coreutils; falls back gracefully if procps/iproute2
#   are missing.
# * The "IPv4/IPv6" line reports the presence of a global (public) address on
#   any interface — real connectivity cannot be verified offline.

export LC_ALL=C # stable parsing, same as YABS

# ---------------------------------------------------------------- banner -----

BORDER='# ## ## ## ## ## ## ## ## ## ## ## ## ## ## ## ## ## #'

center() { # print text centered between '#'s, using the border width
    local width=$(( ${#BORDER} - 2 )) text=$1 pad
    pad=$(( (width - ${#text}) / 2 ))
    printf '#%*s%s%*s#\n' "$pad" '' "$text" "$(( width - pad - ${#text} ))" ''
}

echo "$BORDER"
center 'Offline System Info (YABS-lite)'
echo "$BORDER"
echo
date
START=$(date +%s)

# ------------------------------------------------------- system information --

echo
echo "Basic System Information:"
echo "---------------------------------"

# uptime straight from the kernel (independent of `uptime` output wording)
UPTIME=$(awk '{ printf "%d days, %d hours, %d minutes",
    int($1 / 86400), int($1 % 86400 / 3600), int($1 % 3600 / 60) }' /proc/uptime 2>/dev/null)
echo "Uptime     : ${UPTIME:-unknown}"

# CPU model: /proc/cpuinfo first (x86), lscpu as fallback (ARM, etc.)
CPU_PROC=$(awk -F: '/model name/ {name=$2} END {print name}' /proc/cpuinfo 2>/dev/null | sed 's/^[ \t]*//;s/[ \t]*$//')
if [ -z "$CPU_PROC" ] && command -v lscpu >/dev/null 2>&1; then
    CPU_PROC=$(lscpu 2>/dev/null | sed -n 's/^Model name:[[:space:]]*//p' | head -1)
fi
echo "Processor  : ${CPU_PROC:-unknown}"

# logical core count + clock (last "cpu MHz" seen), lscpu as fallback
CPU_CORES=$(grep -c '^processor' /proc/cpuinfo 2>/dev/null)
[ "${CPU_CORES:-0}" -gt 0 ] 2>/dev/null || CPU_CORES=$(nproc 2>/dev/null)
CPU_FREQ=$(awk -F: ' /cpu MHz/ {f=$2} END {gsub(/^[ \t]+|[ \t]+$/, "", f); print f}' /proc/cpuinfo 2>/dev/null)
if [ -z "$CPU_FREQ" ] && command -v lscpu >/dev/null 2>&1; then
    CPU_FREQ=$(lscpu 2>/dev/null | sed -n 's/^CPU max MHz:[[:space:]]*//p' | head -1)
fi
if [ -z "$CPU_FREQ" ] && [ -r /sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq ]; then
    CPU_FREQ=$(awk '{printf "%.3f", $1 / 1000}' /sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq 2>/dev/null)
fi
[ -n "$CPU_FREQ" ] && CPU_FREQ="$CPU_FREQ MHz" || CPU_FREQ="n/a"
echo "CPU cores  : ${CPU_CORES:-?} @ $CPU_FREQ"

# CPU feature flags from /proc/cpuinfo
if grep -qm1 -w aes /proc/cpuinfo 2>/dev/null; then CPU_AES="\xE2\x9C\x94 Enabled"; else CPU_AES="\xE2\x9D\x8C Disabled"; fi
echo -e "AES-NI     : $CPU_AES"
if grep -Eqm1 '\b(vmx|svm)\b' /proc/cpuinfo 2>/dev/null; then CPU_VIRT="\xE2\x9C\x94 Enabled"; else CPU_VIRT="\xE2\x9D\x8C Disabled"; fi
echo -e "VM-x/AMD-V : $CPU_VIRT"

# memory: `free` (procps) with /proc/meminfo fallback; values are KiB
TOTAL_RAM=$(free 2>/dev/null | awk 'NR==2 {print $2}')
[ -n "$TOTAL_RAM" ] || TOTAL_RAM=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null)
TOTAL_SWAP=$(free 2>/dev/null | awk '/^Swap:/ {print $2}')
[ -n "$TOTAL_SWAP" ] || TOTAL_SWAP=$(awk '/^SwapTotal:/ {print $2}' /proc/meminfo 2>/dev/null)

# disk: sum all mounted local filesystems, counting each source device only
# once (bind mounts / btrfs subvolumes), via /proc/mounts + stat -f; df as a
# fallback for systems where stat -f is unavailable
DISK_FS_TYPES="simfs ext2 ext3 ext4 btrfs xfs vfat exfat ntfs swap"
TOTAL_DISK=$(
    while read -r src mnt fstype _; do
        case " $DISK_FS_TYPES " in *" $fstype "*)
            # mount points are octal-escaped in /proc/mounts (e.g. "\040")
            stat -f -c '%b %S' "$(printf '%b' "$mnt")" 2>/dev/null | awk -v s="$src" '{print s, $0}'
            ;;
        esac
    done < /proc/mounts 2>/dev/null \
        | awk 'NF==3 && !seen[$1]++ {total += $2 * $3 / 1024} END {if (total > 0) printf "%d\n", total}'
)
[ -n "$TOTAL_DISK" ] || TOTAL_DISK=$(df --total -t simfs -t ext2 -t ext3 -t ext4 -t btrfs -t xfs -t vfat -t exfat -t ntfs -t swap 2>/dev/null | awk '$1=="total" {print $2}')

# KiB -> largest unit, 1 decimal (same math as YABS format_size)
fmt_size() {
    awk -v k="${1:-0}" 'BEGIN {
        if      (k >= 1073741824) printf "%.1f TiB", k / 1073741824
        else if (k >= 1048576)   printf "%.1f GiB", k / 1048576
        else if (k >= 1024)      printf "%.1f MiB", k / 1024
        else                     printf "%.1f KiB", k
    }'
}

echo "RAM        : $(fmt_size "$TOTAL_RAM")"
echo "Swap       : $(fmt_size "$TOTAL_SWAP")"
echo "Disk       : $(fmt_size "$TOTAL_DISK")"

DISTRO=$(sed -n 's/^PRETTY_NAME="\(.*\)"$/\1/p' /etc/os-release 2>/dev/null | head -1)
echo "Distro     : ${DISTRO:-unknown}"

KERNEL=$(uname -r 2>/dev/null)
echo "Kernel     : ${KERNEL:-unknown}"

VIRT=$(systemd-detect-virt 2>/dev/null)
if [ -z "$VIRT" ]; then
    if [ -f /.dockerenv ]; then VIRT="docker"
    elif [ -f /run/.containerenv ]; then VIRT="podman"
    elif grep -qa 'container=lxc' /proc/1/environ 2>/dev/null; then VIRT="lxc"
    elif [ -r /sys/class/dmi/id/product_name ]; then
        case "$(cat /sys/class/dmi/id/product_name 2>/dev/null)" in
            *KVM*|*QEMU*)       VIRT="kvm" ;;
            *VMware*)           VIRT="vmware" ;;
            *VirtualBox*)       VIRT="oracle" ;;
            *Virtual\ Machine*) VIRT="microsoft" ;;
        esac
    fi
fi
if [ -n "$VIRT" ]; then VIRT="${VIRT^^}"; else VIRT="UNKNOWN"; fi
echo "VM Type    : $VIRT"

# IPv4/IPv6: presence of a global (public) address on any interface; this is
# the offline stand-in for YABS's ping/curl connectivity check
if command -v ip >/dev/null 2>&1; then
    GLOBAL_ADDRS=$(ip -o addr show scope global 2>/dev/null)
    IP4="\xE2\x9D\x8C None"; IP6="\xE2\x9D\x8C None"
    printf '%s\n' "$GLOBAL_ADDRS" | grep -q ' inet '  && IP4="\xE2\x9C\x94 Global"
    printf '%s\n' "$GLOBAL_ADDRS" | grep -q ' inet6 ' && IP6="\xE2\x9C\x94 Global"
    echo -e "IPv4/IPv6  : $IP4 / $IP6"
else
    echo "IPv4/IPv6  : n/a (iproute2 not found)"
fi

echo
echo "Completed in $(( $(date +%s) - START )) sec (offline)"
exit 0
