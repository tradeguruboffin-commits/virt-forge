#!/bin/sh
# =============================================================
#  virt-forge — Desktop Entry Installer
#  Creates a .desktop file so virt-forge appears in app menus
#
#  Usage:
#    ./installer/install-desktop.sh          # install
#    ./installer/install-desktop.sh remove   # uninstall
# =============================================================
set -eu

# ── Paths ─────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(dirname "$SCRIPT_DIR")"
BINARY="$ROOT/bin/virt-forge"
ICON="$SCRIPT_DIR/virt-forge.png"

APP_NAME="virt-forge"
DESKTOP_FILE="${APP_NAME}.desktop"

# ── Color helpers ─────────────────────────────────────────────
if [ -t 1 ]; then
    GREEN='\033[0;32m'; YELLOW='\033[1;33m'
    RED='\033[0;31m'; BLUE='\033[0;34m'
    BOLD='\033[1m'; RESET='\033[0m'
else
    GREEN=''; YELLOW=''; RED=''; BLUE=''; BOLD=''; RESET=''
fi

info()    { printf "${BLUE}ℹ${RESET}  %s\n" "$1"; }
success() { printf "${GREEN}✅${RESET} %s\n" "$1"; }
warn()    { printf "${YELLOW}⚠${RESET}  %s\n" "$1"; }
die()     { printf "${RED}❌${RESET} %s\n" "$1" >&2; exit 1; }
header()  { printf "\n${BOLD}%s${RESET}\n" "$1"; }

# ── Detect install scope ──────────────────────────────────────
# root → system-wide  /usr/share/...
# user → local only   ~/.local/share/...
if [ "$(id -u)" -eq 0 ]; then
    SCOPE="system"
    DESKTOP_DIR="/usr/share/applications"
    ICON_DIR="/usr/share/icons/hicolor/512x512/apps"
else
    SCOPE="user"
    DESKTOP_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
    ICON_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/512x512/apps"
fi

DESKTOP_PATH="$DESKTOP_DIR/$DESKTOP_FILE"
ICON_DEST="$ICON_DIR/${APP_NAME}.png"

# =============================================================
#  INSTALL
# =============================================================

do_install() {
    header "virt-forge — Desktop Entry Installer ($SCOPE)"

    # ── Preflight checks ──────────────────────────────────────
    if [ ! -f "$BINARY" ]; then
        die "Binary not found: $BINARY\n   Run: ./build/make gui"
    fi

    if [ ! -x "$BINARY" ]; then
        die "Binary is not executable: $BINARY\n   Run: chmod +x $BINARY"
    fi

    if [ ! -f "$ICON" ]; then
        warn "Icon not found: $ICON"
        warn "Desktop entry will be created without an icon"
        ICON_LINE=""
    else
        ICON_LINE="Icon=$ICON_DEST"
    fi

    # ── Create directories ────────────────────────────────────
    mkdir -p "$DESKTOP_DIR"
    mkdir -p "$ICON_DIR"

    # ── Install icon ──────────────────────────────────────────
    if [ -f "$ICON" ]; then
        cp "$ICON" "$ICON_DEST"
        success "Icon installed → $ICON_DEST"

        # Update icon cache (system install only, best-effort)
        if [ "$SCOPE" = "system" ] && command -v gtk-update-icon-cache > /dev/null 2>&1; then
            gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
        fi
    fi

    # ── Write .desktop file ───────────────────────────────────
    cat > "$DESKTOP_PATH" << DESKTOP
[Desktop Entry]
Version=1.1
Type=Application
Name=Virt-Forge
GenericName=QEMU Control Panel
Comment=Manage and launch QEMU virtual machines
Exec=$BINARY
${ICON_LINE}
Terminal=false
Categories=System;Emulator;Virtualization;
Keywords=qemu;vm;virtual;machine;kvm;emulator;
StartupNotify=true
StartupWMClass=virt-forge
DESKTOP

    chmod 644 "$DESKTOP_PATH"
    success "Desktop entry → $DESKTOP_PATH"

    # ── Update desktop database ───────────────────────────────
    if command -v update-desktop-database > /dev/null 2>&1; then
        update-desktop-database "$DESKTOP_DIR" 2>/dev/null || true
        info "Desktop database updated"
    fi

    header "Done!"
    success "Virt-Forge will appear in your application menu"
    if [ "$SCOPE" = "user" ]; then
        warn "You may need to log out and back in for the entry to appear"
    fi
    printf "\n${BOLD}To remove:${RESET} ${BLUE}$0 remove${RESET}\n\n"
}

# =============================================================
#  REMOVE
# =============================================================

do_remove() {
    header "virt-forge — Removing Desktop Entry ($SCOPE)"

    REMOVED=0

    if [ -f "$DESKTOP_PATH" ]; then
        rm -f "$DESKTOP_PATH"
        success "Removed: $DESKTOP_PATH"
        REMOVED=1
    else
        warn "Desktop entry not found: $DESKTOP_PATH"
    fi

    if [ -f "$ICON_DEST" ]; then
        rm -f "$ICON_DEST"
        success "Removed: $ICON_DEST"
        REMOVED=1
    else
        warn "Icon not found: $ICON_DEST"
    fi

    # Update caches
    if command -v update-desktop-database > /dev/null 2>&1; then
        update-desktop-database "$DESKTOP_DIR" 2>/dev/null || true
    fi
    if [ "$SCOPE" = "system" ] && command -v gtk-update-icon-cache > /dev/null 2>&1; then
        gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
    fi

    if [ "$REMOVED" -eq 1 ]; then
        success "Desktop entry removed successfully"
    else
        warn "Nothing was removed"
    fi
}

# =============================================================
#  MAIN
# =============================================================

CMD="${1:-install}"

case "$CMD" in
    install) do_install ;;
    remove)  do_remove  ;;
    *)
        printf 'Usage: %s [install|remove]\n' "$0"
        exit 1
        ;;
esac
