# Virt-Forge

A QEMU virtual machine manager — CLI launcher, control utility, disk manager, and PyQt6 GUI, written in Go and Python.

---

## Project Structure

```
virt-forge/
├── assets/                  # Go source files
│   ├── go.mod
│   ├── go.sum
│   ├── main.go              # qemu-run — VM launcher (CLI args)
│   ├── qemu-ctl.go          # qemu-ctl — VM status / stop / list
│   └── qemu-disk.go         # qemu-disk — disk image manager (CLI args)
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

Configure and launch a VM directly from the GUI — no terminal required.

| Field | Description |
|---|---|
| **Profile** | `normal` (4 GB RAM, 2 CPU) / `lowram` (2 GB RAM, 1 CPU) / saved profile |
| **Architecture** | `x86_64` or `aarch64` |
| **Disk** | Select from `bin/` or browse / enter a full path |
| **Boot ISO** | Optional — for first-time OS install only |
| **RAM / CPU** | Override the profile defaults |
| **SSH port** | Host port forwarded to guest port 22 (default: `4444`) |
| **Extra forwards** | Additional port mappings, e.g. `8080:8080,5432:5432` |
| **VNC** | Enable remote display; set port in the `5900–5999` range |
| **SPICE** | Enable SPICE display; requires a password |
| **Audio** | Enable PulseAudio passthrough (off by default) |
| **Mode** | Daemon (background) or foreground |

Click **🚀 Launch VM** — the binary starts QEMU directly and reports success or
failure in the console area below the button.

#### Connecting to a Running VM

```sh
# SSH
ssh user@localhost -p 4444

# VNC (connect to the port you set, e.g. 5909)
# Use any VNC viewer: TigerVNC, Remmina, etc.

# SPICE
remote-viewer spice://localhost:5910
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
| 📏 Resize Image | Expand an existing image |
| 🔄 Convert Image | Convert between formats: `qcow2` / `raw` / `vmdk` / `vdi` / `vpc` / `vhdx` |

Use the **Browse** button in each dialog to pick a file, or enter a full path
directly. Bare filenames (no directory) are resolved relative to `bin/`.

---

### CLI — Direct Binary Usage

All tools accept CLI arguments directly — no interactive prompts.

#### qemu-run

```sh
./bin/qemu-run --disk <path> [options]

Options:
  --profile  normal|lowram|<saved-name>   (default: normal)
  --arch     x86_64|aarch64
  --disk     <path>                        required
  --iso      <path>
  --ram      <MB>
  --cpu      <n>
  --ssh      <port>                        (default: 4444)
  --vnc      <port>                        (default: 5909)
  --no-vnc
  --spice    <port>
  --spice-pass <password>
  --no-spice
  --audio
  --no-audio
  --fg                                     run in foreground (default: daemon)
  --extra-fwds hostport:guestport,...
```

Examples:

```sh
# Quick launch with defaults
./bin/qemu-run --disk bin/alpine.qcow2

# Custom RAM, VNC port, foreground
./bin/qemu-run --disk bin/debian12.qcow2 --ram 8192 --cpu 4 --vnc 5901 --fg

# With SPICE and extra port forward
./bin/qemu-run --disk bin/alpine.qcow2 --spice 5910 --spice-pass hunter2 --extra-fwds 8080:8080
```

#### qemu-ctl

```sh
./bin/qemu-ctl list      # list running VMs
./bin/qemu-ctl status    # show PID, ports, lock files
./bin/qemu-ctl stop      # stop a VM (interactive selection)
./bin/qemu-ctl debug     # raw parsed fields + lock dir contents
```

#### qemu-disk

```sh
./bin/qemu-disk create  --name <file> --size <size>
./bin/qemu-disk info    --name <file>
./bin/qemu-disk resize  --name <file> --size <size>
./bin/qemu-disk convert --src <file>  --dst <file> --fmt <format>
```

Size format: number followed by `K`, `M`, `G`, or `T` — e.g. `20G`, `512M`.

Examples:

```sh
./bin/qemu-disk create  --name bin/debian.qcow2 --size 20G
./bin/qemu-disk info    --name bin/alpine.qcow2
./bin/qemu-disk resize  --name bin/alpine.qcow2 --size 30G
./bin/qemu-disk convert --src bin/alpine.qcow2 --dst bin/alpine.raw --fmt raw
```

---

## Lock File System

When a VM starts, lock files are created in `~/.virt-forge-locks/`:

```
~/.virt-forge-locks/
├── qemu_4444.pid      # QEMU process ID (keyed by SSH port)
├── ssh_4444.lock
├── vnc_5909.lock      # keyed by VNC port (not display number)
└── spice_5910.lock
```

- Lock files are removed automatically when the VM stops
- Stale locks from crashed sessions are detected and reported by `qemu-ctl status`
- Starting a VM on an already-used port produces a conflict error

---

## KVM Hardware Acceleration

KVM significantly improves VM performance. To enable it:

```sh
# Check if KVM is available
ls /dev/kvm

# Check your group membership
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
