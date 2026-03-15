#!/bin/sh
# =============================================================
#  virt-forge — Dependency Installer
#
#  Usage:
#    sudo ./installer/install-deps.sh engine-deps   Install QEMU packages
#    sudo ./installer/install-deps.sh build-deps    Install python3 + python3-venv + go
#    sudo ./installer/install-deps.sh               Install both (default)
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

# ── Root check ────────────────────────────────────────────────
check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        die "This script must be run as root.\n   Try: sudo $0 $*"
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

# ── Verify engine binaries ────────────────────────────────────
verify_engine() {
    header "Verifying QEMU installation..."
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

# ── Verify build tools ────────────────────────────────────────
verify_build() {
    header "Verifying build tools..."
    ALL_OK=0
    for bin in python3 go; do
        if command -v "$bin" > /dev/null 2>&1; then
            VER="$("$bin" --version 2>/dev/null)"
            success "$bin — $VER"
        else
            warn "$bin not found in PATH"
            ALL_OK=1
        fi
    done
    # venv check
    if python3 -m venv --help > /dev/null 2>&1; then
        success "python3-venv — ok"
    else
        warn "python3 venv module not available"
        ALL_OK=1
    fi
    return $ALL_OK
}

# =============================================================
#  ENGINE-DEPS: QEMU package installers
# =============================================================

engine_apt() {
    info "Using apt (Debian/Ubuntu/Mint)"
    apt-get update -qq
    if apt-get install -y qemu-system-x86 qemu-system-arm qemu-utils; then
        :
    else
        warn "Specific packages failed — trying qemu-system (meta-package)"
        apt-get install -y qemu-system qemu-utils
    fi
}

engine_dnf() {
    info "Using dnf (Fedora/RHEL/AlmaLinux/Rocky)"
    if echo "$DISTRO_ID $DISTRO_LIKE" | grep -qiE 'rhel|centos|almalinux|rocky'; then
        dnf install -y epel-release 2>/dev/null || true
    fi
    dnf install -y qemu-system-x86 qemu-system-aarch64 qemu-img
}

engine_yum() {
    info "Using yum (CentOS 7)"
    yum install -y epel-release 2>/dev/null || true
    yum install -y qemu-kvm qemu-img
    warn "CentOS 7: qemu-system-aarch64 may not be available"
}

engine_pacman() {
    info "Using pacman (Arch/Manjaro)"
    pacman -Sy --noconfirm qemu-full
}

engine_zypper() {
    info "Using zypper (openSUSE)"
    zypper --non-interactive install qemu qemu-tools
}

engine_apk() {
    info "Using apk (Alpine Linux)"
    apk add --no-cache qemu-system-x86_64 qemu-system-aarch64 qemu-img
}

engine_xbps() {
    info "Using xbps (Void Linux)"
    xbps-install -Sy -y qemu-system-x86_64 qemu-system-aarch64 qemu
}

engine_emerge() {
    info "Using emerge (Gentoo)"
    emerge --ask=n app-emulation/qemu
    warn "Gentoo: ensure USE flags include:"
    warn "  qemu_softmmu_targets_x86_64"
    warn "  qemu_softmmu_targets_aarch64"
}

route_engine() {
    case "$DISTRO_ID" in
        debian|ubuntu|linuxmint|pop|elementary|zorin|kali|parrot|raspbian)
            engine_apt ;;
        fedora)
            engine_dnf ;;
        rhel|centos|almalinux|rocky|ol)
            if command -v dnf > /dev/null 2>&1; then engine_dnf
            else engine_yum; fi ;;
        arch|manjaro|endeavouros|garuda|artix)
            engine_pacman ;;
        opensuse*|sles)
            engine_zypper ;;
        alpine)
            engine_apk ;;
        void)
            engine_xbps ;;
        gentoo)
            engine_emerge ;;
        *)
            case "$DISTRO_LIKE" in
                *debian*|*ubuntu*)  engine_apt    ;;
                *fedora*|*rhel*)    engine_dnf    ;;
                *arch*)             engine_pacman ;;
                *suse*)             engine_zypper ;;
                *)
                    die "Unsupported distro: $DISTRO_NAME\n   Please install manually:\n     qemu-system-x86_64  qemu-system-aarch64  qemu-utils"
                    ;;
            esac ;;
    esac
}

# =============================================================
#  BUILD-DEPS: python3 + python3-venv + go
# =============================================================

build_apt() {
    info "Using apt (Debian/Ubuntu/Mint)"
    apt-get update -qq
    apt-get install -y python3 python3-venv golang-go
}

build_dnf() {
    info "Using dnf (Fedora/RHEL/AlmaLinux/Rocky)"
    dnf install -y python3 python3-virtualenv golang
}

build_yum() {
    info "Using yum (CentOS 7)"
    yum install -y python3 golang
    warn "CentOS 7: install python3-virtualenv manually if missing"
}

build_pacman() {
    info "Using pacman (Arch/Manjaro)"
    pacman -Sy --noconfirm python python-virtualenv go
}

build_zypper() {
    info "Using zypper (openSUSE)"
    zypper --non-interactive install python3 python3-virtualenv go
}

build_apk() {
    info "Using apk (Alpine Linux)"
    apk add --no-cache python3 py3-virtualenv go
}

build_xbps() {
    info "Using xbps (Void Linux)"
    xbps-install -Sy -y python3 python3-virtualenv go
}

build_emerge() {
    info "Using emerge (Gentoo)"
    emerge --ask=n dev-lang/python dev-python/virtualenv dev-lang/go
}

route_build() {
    case "$DISTRO_ID" in
        debian|ubuntu|linuxmint|pop|elementary|zorin|kali|parrot|raspbian)
            build_apt ;;
        fedora)
            build_dnf ;;
        rhel|centos|almalinux|rocky|ol)
            if command -v dnf > /dev/null 2>&1; then build_dnf
            else build_yum; fi ;;
        arch|manjaro|endeavouros|garuda|artix)
            build_pacman ;;
        opensuse*|sles)
            build_zypper ;;
        alpine)
            build_apk ;;
        void)
            build_xbps ;;
        gentoo)
            build_emerge ;;
        *)
            case "$DISTRO_LIKE" in
                *debian*|*ubuntu*)  build_apt    ;;
                *fedora*|*rhel*)    build_dnf    ;;
                *arch*)             build_pacman ;;
                *suse*)             build_zypper ;;
                *)
                    die "Unsupported distro: $DISTRO_NAME\n   Please install manually:\n     python3  python3-venv  go"
                    ;;
            esac ;;
    esac
}

# =============================================================
#  MAIN
# =============================================================

CMD="${1:-all}"

case "$CMD" in
    engine-deps|build-deps|all) ;;
    --help|-h)
        printf 'Usage: sudo %s [engine-deps|build-deps|all]\n' "$0"
        printf '\n'
        printf '  engine-deps   Install QEMU packages\n'
        printf '                (qemu-system-x86_64, qemu-system-aarch64, qemu-utils)\n'
        printf '  build-deps    Install Python build tools\n'
        printf '                (python3, python3-venv)\n'
        printf '  all           Install both (default)\n'
        exit 0
        ;;
    *)
        die "Unknown command: $CMD\n   Usage: sudo $0 [engine-deps|build-deps|all]"
        ;;
esac

header "virt-forge — Dependency Installer"
check_root
info "Detecting system..."
detect_distro
info "Distro: $DISTRO_NAME"

# ── engine-deps ───────────────────────────────────────────────
if [ "$CMD" = "engine-deps" ] || [ "$CMD" = "all" ]; then
    header "Installing QEMU engine packages..."
    route_engine
    check_kvm
    if ! verify_engine; then
        die "QEMU installation verification failed — some binaries are missing"
    fi
fi

# ── build-deps ────────────────────────────────────────────────
if [ "$CMD" = "build-deps" ] || [ "$CMD" = "all" ]; then
    header "Installing Python build tools..."
    route_build
    if ! verify_build; then
        die "Build tools verification failed — python3, venv, or go is missing"
    fi
fi

header "Done!"
success "Dependencies installed successfully"
printf "\n${BOLD}Next steps:${RESET}\n"
if [ "$CMD" = "engine-deps" ]; then
    printf "  Run: ${BLUE}make${RESET}\n\n"
elif [ "$CMD" = "build-deps" ]; then
    printf "  Run: ${BLUE}make gui${RESET}\n\n"
else
    printf "  1. Run: ${BLUE}make${RESET}\n"
    printf "  2. Run: ${BLUE}./virt-forge${RESET}\n\n"
fi
