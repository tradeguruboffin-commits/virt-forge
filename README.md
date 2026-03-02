# Virt-Forge

A QEMU virtual machine manager — interactive TUI launcher, control utility, disk manager, and PyQt6 GUI, written in Go and Python.

---

## Project Structure

```
virt-forge/
├── assets/                  # Go source files
│   ├── go.mod
│   ├── go.sum
│   ├── main.go              # qemu-run — main VM launcher (interactive TUI)
│   ├── qemu-ctl.go          # qemu-ctl — VM status / stop / list
│   └── qemu-disk.go         # qemu-disk — disk image manager
│
├── bin/                     # Build output (binaries + disk images)
│   ├── qemu-run             # VM launcher binary
│   ├── qemu-ctl             # Control binary
│   ├── qemu-disk            # Disk manager binary
│   ├── alpine.qcow2         # (example) disk image
│   └── debian12.qcow2       # (example) disk image
│
├── build/
│   ├── make                 # Main build script (Go + GUI)
│   └── make-gui             # GUI-only build script (PyInstaller)
│
├── gui/
│   └── virt-forge.py        # PyQt6 GUI source
│
├── installer/
│   ├── install-deps.sh      # QEMU system dependency installer
│   ├── install-desktop.sh   # Desktop menu entry installer
│   └── virt-forge.png       # App icon (512×512)
│
├── _internal/               # GUI runtime libs — must stay beside virt-forge binary
├── virt-forge               # GUI binary (PyInstaller one-dir)
└── README.md
```

> **Important:** `_internal/` must always remain in the same directory as the
> `virt-forge` binary. Moving or deleting it will prevent the GUI from starting.

---

## Step 1 — Install QEMU Dependencies

The following system binaries are required:
`qemu-system-x86_64`, `qemu-system-aarch64`, `qemu-utils`

```sh
sudo ./installer/install-deps.sh
```

**Supported distributions:**

| Package Manager | Distributions |
|---|---|
| `apt` | Debian, Ubuntu, Mint, Pop!\_OS, Kali, Raspbian |
| `dnf` | Fedora, RHEL 8+, AlmaLinux, Rocky |
| `yum` | CentOS 7 |
| `pacman` | Arch, Manjaro, EndeavourOS |
| `zypper` | openSUSE Leap / Tumbleweed |
| `apk` | Alpine Linux |
| `xbps` | Void Linux |
| `emerge` | Gentoo |

The installer verifies each binary after installation and exits with an error if
anything is missing. If `/dev/kvm` is found, the current user is automatically
added to the `kvm` group (re-login required).

---

## Step 2 — Build the Project

### Requirements

| Tool | Purpose |
|---|---|
| `go` 1.21+ | Build Go binaries |
| `python3` + `pip` | Build the GUI |
| `PyInstaller` | `pip install pyinstaller` |
| `PyQt6` | `pip install PyQt6` |

### Build Commands

```sh
# Build everything (Go binaries + GUI)
./build/make

# Build individually
./build/make qemu-run    # VM launcher only
./build/make qemu-ctl    # Control binary only
./build/make qemu-disk   # Disk manager only
./build/make gui         # GUI only (PyInstaller one-dir)

# Remove all build output (disk images are preserved)
./build/make clean
```

After a successful build the project root will contain:

```
virt-forge        ← GUI executable  (run this)
_internal/        ← runtime libs    (must stay here)
```

---

## Step 3 — Install Desktop Entry (Optional)

Add Virt-Forge to your application menu:

```sh
# User-level install (no sudo required)
./installer/install-desktop.sh

# System-wide install
sudo ./installer/install-desktop.sh

# Remove
./installer/install-desktop.sh remove
sudo ./installer/install-desktop.sh remove
```

---

## Usage

### Launching the GUI

```sh
./virt-forge
```

Or search for **Virt-Forge** in your application menu if the desktop entry is installed.

---

### VM Launcher Tab

Click **Launch VM in Terminal** to open a terminal window running the `qemu-run`
interactive TUI. All VM configuration is handled interactively inside the terminal.

#### Terminal Setup Steps

| Step | Prompt | Example |
|---|---|---|
| 1 | Select profile | `1` Normal / `2` Low RAM / `3` Load saved |
| 2 | Architecture | `1` x86\_64 &nbsp; `2` aarch64 |
| 3 | Select disk | Choose from list or enter full path |
| 4 | RAM | `4096` (MB) |
| 5 | CPU cores | `2` |
| 6 | Boot from ISO? | `y` for first-time OS install / `n` otherwise |
| 7 | Enable VNC? | `y` / `n` |
| 8 | Enable SPICE? | `y` / `n` |
| 9 | SSH port | `4444` |
| 10 | SPICE password | Enter a password or press Enter for random |
| 11 | Extra port forwards | Optional additional port mappings |
| 12 | Daemon mode? | `y` = background / `n` = foreground |
| 13 | Save profile? | `y` to reuse settings next time |

#### Connecting to a Running VM

```sh
# SSH
ssh user@localhost -p 4444

# SPICE
remote-viewer spice://localhost:5902

# VNC (display 1 = port 5901)
```

---

### Control Tab

Monitor and stop running VMs.

| Button | Action |
|---|---|
| ⟳ Refresh | Update the VM list (also auto-refreshes every 5 seconds) |
| 📊 Status | Show details for all running VMs (PID, ports, lock files) |
| 🛑 Stop Selected | Stop the selected VM |
| 💀 Stop All | Stop all running VMs |

---

### Disk Manager Tab

| Button | Action |
|---|---|
| ➕ Create Image | Create a new QCOW2 disk image |
| ℹ Image Info | Display detailed information about an image |
| 📏 Resize Image | Expand or shrink an existing image |
| 🔄 Convert Image | Convert between formats: qcow2 / raw / vmdk / vdi / vpc / vhdx |

> **Note:** Enter only the filename (`myvm.qcow2`) — the image is automatically
> created in the `bin/` directory and will appear in the VM Launcher disk list.

---

### CLI — Direct Binary Usage

All tools can be used directly from the terminal without the GUI:

```sh
# Launch a VM (interactive TUI)
./bin/qemu-run

# List running VMs
./bin/qemu-ctl list

# Show detailed VM status
./bin/qemu-ctl status

# Stop a VM
./bin/qemu-ctl stop

# Create a disk image
./bin/qemu-disk create

# Show disk image info
./bin/qemu-disk info

# Resize a disk image
./bin/qemu-disk resize

# Convert a disk image
./bin/qemu-disk convert
```

---

## Lock File System

When a VM starts, lock files are created in `~/.virt-forge-locks/`:

```
~/.virt-forge-locks/
├── qemu_4444.pid      # SSH port lock (stores the QEMU PID)
├── ssh_4444.lock
├── vnc_1.lock
└── spice_5902.lock
```

- Lock files are removed automatically when the VM stops
- Stale locks from crashed sessions are swept on the next `qemu-run` launch
- Attempting to start a VM on an already-used port will produce a conflict error

---

## KVM Hardware Acceleration

KVM significantly improves VM performance. To enable it:

```sh
# Check if KVM is available
ls /dev/kvm

# Check if your user is in the kvm group
groups $USER

# Add yourself to the kvm group (re-login required)
sudo usermod -aG kvm $USER
```

Without KVM, QEMU falls back to TCG (software emulation) — functional but slower.

---

## Environment Variables

```sh
# Override the project root (default: auto-detected from binary location)
VIRT_FORGE_ROOT=/opt/virt-forge ./virt-forge

# Override the bin directory
VIRT_FORGE_BIN=/custom/bin ./virt-forge
```

---

## Dependency Summary

| Component | Dependencies |
|---|---|
| VM Launcher / CTL / Disk | `qemu-system-x86_64` `qemu-system-aarch64` `qemu-utils` |
| Build (Go) | `go` 1.21+ |
| Build (GUI) | `python3` `pyinstaller` `PyQt6` |
| GUI Runtime | `_internal/` (bundled — no separate install needed) |
| Terminal (for VM launch) | `xterm` or `konsole` or `gnome-terminal` |
