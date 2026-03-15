# =============================================================
#  Virt-Forge — Makefile
#
#  Build:
#    make              Build all (Go binaries + GUI one-dir)
#    make onefile      Build all (Go binaries + GUI single binary)
#    make gui          GUI only (one-dir)
#    make gui-onefile  GUI only (single binary)
#    make qemu-run     VM launcher binary only
#    make qemu-ctl     Control binary only
#    make qemu-disk    Disk manager binary only
#    make clean        Remove all build output
#
#  Install:
#    make install-deps            Install QEMU + Python + Go build tools (sudo)
#    make install-engine-deps     Install QEMU packages only (sudo)
#    make install-build-deps      Install python3 + python3-venv + go only (sudo)
#    make install-desktop         Install desktop menu entry (user-level)
#    make install-desktop-sys     Install desktop menu entry (system-wide, sudo)
#    make remove-desktop          Remove desktop menu entry (user-level)
#    make remove-desktop-sys      Remove desktop menu entry (system-wide, sudo)
#
#  Info:
#    make help         Show this help
# =============================================================

.PHONY: all onefile gui gui-onefile \
        qemu-run qemu-ctl qemu-disk \
        clean \
        install-deps install-engine-deps install-build-deps \
        install-desktop install-desktop-sys \
        remove-desktop  remove-desktop-sys \
        help

BUILD   := ./build/make
DEPS    := ./installer/install-deps.sh
DESKTOP := ./installer/install-desktop.sh

# ── Build targets ─────────────────────────────────────────────

all:
	@sh $(BUILD) all

onefile:
	@sh $(BUILD) all --onefile

gui:
	@sh $(BUILD) gui

gui-onefile:
	@sh $(BUILD) gui --onefile

qemu-run:
	@sh $(BUILD) qemu-run

qemu-ctl:
	@sh $(BUILD) qemu-ctl

qemu-disk:
	@sh $(BUILD) qemu-disk

clean:
	@sh $(BUILD) clean

# ── Installer targets ─────────────────────────────────────────

install-deps:
	@sudo sh $(DEPS) all

install-engine-deps:
	@sudo sh $(DEPS) engine-deps

install-build-deps:
	@sudo sh $(DEPS) build-deps

install-desktop:
	@sh $(DESKTOP) install

install-desktop-sys:
	@sudo sh $(DESKTOP) install

remove-desktop:
	@sh $(DESKTOP) remove

remove-desktop-sys:
	@sudo sh $(DESKTOP) remove

# ── Help ──────────────────────────────────────────────────────

help:
	@printf '\nVirt-Forge — available targets:\n'
	@printf '\n  Build:\n'
	@printf '    make                     Build all — Go binaries + GUI (one-dir)\n'
	@printf '    make onefile             Build all — Go binaries + GUI (single binary)\n'
	@printf '    make gui                 Build GUI only (one-dir)\n'
	@printf '    make gui-onefile         Build GUI only (single binary)\n'
	@printf '    make qemu-run            Build VM launcher binary only\n'
	@printf '    make qemu-ctl            Build control binary only\n'
	@printf '    make qemu-disk           Build disk manager binary only\n'
	@printf '    make clean               Remove all build output\n'
	@printf '\n  Install:\n'
	@printf '    make install-deps            Install QEMU + Python + Go build tools (sudo)\n'
	@printf '    make install-engine-deps     Install QEMU packages only (sudo)\n'
	@printf '    make install-build-deps      Install python3 + python3-venv + go only (sudo)\n'
	@printf '    make install-desktop         Add to app menu (user-level)\n'
	@printf '    make install-desktop-sys     Add to app menu (system-wide, sudo)\n'
	@printf '    make remove-desktop          Remove from app menu (user-level)\n'
	@printf '    make remove-desktop-sys      Remove from app menu (system-wide, sudo)\n'
	@printf '\n  Info:\n'
	@printf '    make help                Show this help\n'
	@printf '\n'
