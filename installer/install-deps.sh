#!/bin/sh
# =============================================================
#  virt-forge — Dependency Installer
#  Installs: qemu-system-x86_64, qemu-system-aarch64, qemu-utils
#
#  Supported:
#    Debian / Ubuntu / Linux Mint / Pop!_OS     (apt)
#    Fedora / RHEL / AlmaLinux / Rocky          (dnf)
#    CentOS 7                                   (yum)
#    Arch Linux / Manjaro                       (pacman)
#    openSUSE Leap / Tumbleweed                 (zypper)
#    Alpine Linux                               (apk)
#    Void Linux                                 (xbps)
#    Gentoo                                     (emerge)
# =============================================================
set -eu

# ── Color helpers ─────────────────────────────────────────────
if [ -t 1 ]; then
    RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
    BLUE='\033[0;34m'; BOLD='\033[1m'; RESET='\033[0m'
else
    RED=''; GREEN=''; YELLOW=''; BLUE=''; BOLD=''; RESET=''
fi

info()    { printf "${BLUE}ℹ${RESET}  %s\n" "$1"; }
success() { printf "${GREEN}✅${RESET} %s\n" "$1"; }
warn()    { printf "${YELLOW}⚠${RESET}  %s\n" "$1"; }
die()     { printf "${RED}❌${RESET} %s\n" "$1" >&2; exit 1; }
header()  { printf "\n${BOLD}%s${RESET}\n" "$1"; }

# ── [1] Root check FIRST — exit before doing anything else ────
check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        die "This script must be run as root.\n   Try: sudo $0"
    fi
}

# ── Detect distro ─────────────────────────────────────────────
detect_distro() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        DISTRO_ID="${ID:-unknown}"
        DISTRO_LIKE="${ID_LIKE:-}"
        DISTRO_NAME="${PRETTY_NAME:-$ID}"
    elif [ -f /etc/redhat-release ]; then
        DISTRO_ID="rhel"
        DISTRO_LIKE=""
        DISTRO_NAME="$(cat /etc/redhat-release)"
    else
        DISTRO_ID="unknown"
        DISTRO_LIKE=""
        DISTRO_NAME="Unknown"
    fi
}

# ── KVM access check ──────────────────────────────────────────
check_kvm() {
    header "Checking KVM access..."
    if [ -e /dev/kvm ]; then
        success "KVM device found (/dev/kvm)"
        # [6] SUDO_USER → $USER fallback, never adds root to kvm group
        REAL_USER="${SUDO_USER:-$USER}"
        if [ "$REAL_USER" = "root" ]; then
            warn "Running as root directly — skipping kvm group add"
        elif groups "$REAL_USER" 2>/dev/null | grep -qw kvm; then
            success "User '$REAL_USER' already in kvm group"
        else
            if getent group kvm > /dev/null 2>&1; then
                usermod -aG kvm "$REAL_USER"
                warn "Added '$REAL_USER' to kvm group — re-login required"
            fi
        fi
    else
        warn "/dev/kvm not found — QEMU will use TCG (software emulation, slower)"
        warn "Enable virtualisation in BIOS/UEFI if available"
    fi
}

# ── [2] verify() — correct shell convention: 0=success 1=failure
verify() {
    header "Verifying installation..."
    ALL_OK=0
    for bin in qemu-system-x86_64 qemu-system-aarch64 qemu-img; do
        if command -v "$bin" > /dev/null 2>&1; then
            VER="$("$bin" --version 2>/dev/null | head -1)"
            success "$bin — $VER"
        else
            warn "$bin not found in PATH"
            ALL_OK=1
        fi
    done
    return $ALL_OK
}

# =============================================================
#  PACKAGE MANAGER INSTALLERS
# =============================================================

# [3] apt — with fallback for distros that bundle everything in qemu-system
install_apt() {
    info "Using apt (Debian/Ubuntu/Mint)"
    apt-get update -qq
    if apt-get install -y qemu-system-x86 qemu-system-arm qemu-utils; then
        : # success
    else
        warn "Specific packages failed — trying qemu-system (meta-package)"
        apt-get install -y qemu-system qemu-utils
    fi
}

install_dnf() {
    info "Using dnf (Fedora/RHEL/AlmaLinux/Rocky)"
    if echo "$DISTRO_ID $DISTRO_LIKE" | grep -qiE 'rhel|centos|almalinux|rocky'; then
        dnf install -y epel-release 2>/dev/null || true
    fi
    dnf install -y \
        qemu-system-x86 \
        qemu-system-aarch64 \
        qemu-img
}

install_yum() {
    info "Using yum (CentOS 7)"
    yum install -y epel-release 2>/dev/null || true
    yum install -y qemu-kvm qemu-img
    warn "CentOS 7: qemu-system-aarch64 may not be available"
}

# [4] Arch — qemu-full pulls everything including qemu-img
install_pacman() {
    info "Using pacman (Arch/Manjaro)"
    pacman -Sy --noconfirm qemu-full
}

# [5] openSUSE — use qemu meta-package, add qemu-tools for qemu-img
install_zypper() {
    info "Using zypper (openSUSE)"
    zypper --non-interactive install qemu qemu-tools
}

install_apk() {
    info "Using apk (Alpine Linux)"
    apk add --no-cache \
        qemu-system-x86_64 \
        qemu-system-aarch64 \
        qemu-img
}

install_xbps() {
    info "Using xbps (Void Linux)"
    xbps-install -Sy -y \
        qemu-system-x86_64 \
        qemu-system-aarch64 \
        qemu
}

install_emerge() {
    info "Using emerge (Gentoo)"
    emerge --ask=n app-emulation/qemu
    warn "Gentoo: ensure USE flags include:"
    warn "  qemu_softmmu_targets_x86_64"
    warn "  qemu_softmmu_targets_aarch64"
}

# ── Distro → installer mapping ────────────────────────────────
route_install() {
    case "$DISTRO_ID" in
        debian|ubuntu|linuxmint|pop|elementary|zorin|kali|parrot|raspbian)
            install_apt ;;
        fedora)
            install_dnf ;;
        rhel|centos|almalinux|rocky|ol)
            if command -v dnf > /dev/null 2>&1; then
                install_dnf
            else
                install_yum
            fi
            ;;
        arch|manjaro|endeavouros|garuda|artix)
            install_pacman ;;
        opensuse*|sles)
            install_zypper ;;
        alpine)
            install_apk ;;
        void)
            install_xbps ;;
        gentoo)
            install_emerge ;;
        *)
            case "$DISTRO_LIKE" in
                *debian*|*ubuntu*)  install_apt    ;;
                *fedora*|*rhel*)    install_dnf    ;;
                *arch*)             install_pacman ;;
                *suse*)             install_zypper ;;
                *)
                    die "Unsupported distro: $DISTRO_NAME\n   Please install manually:\n     qemu-system-x86_64  qemu-system-aarch64  qemu-utils"
                    ;;
            esac
            ;;
    esac
}

# =============================================================
#  MAIN
# =============================================================

header "virt-forge — Dependency Installer"

# [1] Root check before anything else
check_root

info "Detecting system..."
detect_distro
info "Distro: $DISTRO_NAME"

header "Installing QEMU packages..."
route_install

check_kvm

# [7] Fail hard if verification fails
if ! verify; then
    die "Installation verification failed — some binaries are missing"
fi

header "Done!"
success "QEMU dependencies installed successfully"
printf "\n${BOLD}Next steps:${RESET}\n"
printf "  1. Run: ${BLUE}./build/make${RESET}\n"
printf "  2. Run: ${BLUE}./bin/qemu${RESET}\n\n"
