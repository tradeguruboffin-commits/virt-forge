# Virt-Forge

A QEMU virtual machine manager for Linux — CLI launcher, controller, disk manager, and PyQt6 GUI, written in Go and Python.

---

## Features

- **VM Launcher** — launch QEMU VMs with full control over profile, RAM, CPU, display, network, and audio
- **Snapshot management** — create, list, apply, and delete internal QCOW2 snapshots
- **Live migration** — transfer a running VM to another machine without shutting it down
- **Disk Manager** — create, inspect, resize, and convert QCOW2 disk images
- **Control panel** — monitor running VMs with PID, uptime, disk name, and port info
- **Multi-arch** — supports `x86_64` and `aarch64`
- **KVM + TCG** — hardware acceleration where available, TCG fallback otherwise

---

## Project Structure

```
virt-forge/
├── assets/
│   ├── go.mod
│   ├── go.sum
│   ├── main.go              # qemu-run  — VM launcher
│   ├── qemu-ctl.go          # qemu-ctl  — list / stop / status
│   └── qemu-disk.go         # qemu-disk — disk image manager
│
├── bin/                     # Build output: binaries + disk images
│   ├── qemu-run
│   ├── qemu-ctl
│   ├── qemu-disk
│   └── *.qcow2              # disk images live here
│
├── build/
│   ├── make                 # Main build script (Go + GUI)
│   └── make-gui             # GUI-only build script (PyInstaller)
│
├── gui/
│   └── virt-forge.py        # PyQt6 GUI source
│
├── installer/
│   ├── install-deps.sh      # System dependency installer
│   ├── install-desktop.sh   # Desktop menu entry installer
│   └── virt-forge.png       # App icon (512×512)
│
├── Makefile                 # Convenience targets (wraps build/ and installer/)
├── _internal/               # GUI runtime libs — must stay beside the binary
├── virt-forge               # GUI executable
└── README.md
```

> **Note:** `_internal/` is only present in one-dir builds. It must always
> remain in the same directory as the `virt-forge` binary. In one-file builds
> it is not needed — everything is bundled into the single binary.

---

## Quick Start

```sh
# 1. Install QEMU + Python build tools
make install-deps

# 2. Build everything
make

# 3. Run
./virt-forge
```

---

## Step 1 — Install Dependencies

`install-deps.sh` handles two separate dependency groups:

| Target | What it installs |
|---|---|
| `engine-deps` | `qemu-system-x86_64`, `qemu-system-aarch64`, `qemu-utils` |
| `build-deps` | `python3`, `python3-venv`, `go` |

```sh
# Via Makefile
make install-deps            # install both (default)
make install-engine-deps     # QEMU packages only
make install-build-deps      # python3 + python3-venv + go only

# Or directly
sudo ./installer/install-deps.sh               # both
sudo ./installer/install-deps.sh engine-deps   # QEMU only
sudo ./installer/install-deps.sh build-deps    # Python + Go only
```

`engine-deps` automatically adds the current user to the `kvm` group if
`/dev/kvm` is present (re-login required).

**Supported package managers:**

| Manager | Distributions |
|---|---|
| `apt` | Debian, Ubuntu, Mint, Pop!\_OS, Kali, Raspbian |
| `dnf` | Fedora, RHEL 8+, AlmaLinux, Rocky |
| `yum` | CentOS 7 |
| `pacman` | Arch, Manjaro, EndeavourOS |
| `zypper` | openSUSE Leap / Tumbleweed |
| `apk` | Alpine Linux |
| `xbps` | Void Linux |
| `emerge` | Gentoo |

---

## Step 2 — Build

**Requirements:**

| Tool | Purpose |
|---|---|
| `go` 1.21+ | Build Go binaries |
| `python3` + `python3-venv` | Build the GUI (auto-venv handles PyInstaller + PyQt6) |

The GUI build script automatically creates a temporary `.venv` in the project
root, installs `PyInstaller` and `PyQt6` inside it, builds the binary, then
removes the `.venv` when done. No manual pip install needed.

### Using Make (recommended)

```sh
make              # Build all — Go binaries + GUI (one-dir, default)
make onefile      # Build all — Go binaries + GUI (single portable binary)
make gui          # GUI only (one-dir)
make gui-onefile  # GUI only (single portable binary)
make qemu-run     # VM launcher binary only
make qemu-ctl     # Control binary only
make qemu-disk    # Disk manager binary only
make clean        # Remove all build output
make help         # Show all available targets
```

### Using the build script directly

```sh
./build/make              # same as make
./build/make gui --onefile
./build/make clean
```

### One-dir vs One-file

| Mode | Output | Notes |
|---|---|---|
| `onedir` (default) | `virt-forge` + `_internal/` | Faster startup; `_internal/` must stay beside the binary |
| `onefile` | `virt-forge` only | Single portable binary; slightly slower first launch |

After a successful one-dir build:

```
virt-forge        ← run this
_internal/        ← keep this alongside the binary
```

After a successful one-file build:

```
virt-forge        ← single self-contained binary, copy anywhere
```

---

## Step 3 — Desktop Entry (Optional)

Add Virt-Forge to your application menu:

```sh
# Via Makefile
make install-desktop         # user-level (no sudo)
make install-desktop-sys     # system-wide (sudo)
make remove-desktop          # remove user-level entry
make remove-desktop-sys      # remove system-wide entry

# Or directly
./installer/install-desktop.sh           # install (user-level)
sudo ./installer/install-desktop.sh      # install (system-wide)
./installer/install-desktop.sh remove    # remove
```

---

## GUI Usage

```sh
./virt-forge
```

Or launch **Virt-Forge** from your application menu if the desktop entry is
installed. Click **❓ Help** in the VM Launcher tab for a full in-app guide.

---

### VM Launcher Tab

| Field | Description |
|---|---|
| **Profile** | `normal` (4 GB RAM, 2 CPU) · `lowram` (2 GB RAM, 1 CPU) · saved profiles from `~/.vm_profiles/` |
| **Architecture** | `x86_64` or `aarch64` |
| **Disk** | Select from `bin/` or enter/browse a full path |
| **Boot ISO** | Optional — for OS installation only |
| **RAM / CPU** | Override profile defaults |
| **SSH port** | Host port forwarded to guest port 22 (default: `4444`) |
| **Extra forwards** | Additional port mappings e.g. `8080:8080,5432:5432` |
| **VNC** | Remote display; port in `5900–5999` range |
| **SPICE** | Better-performance remote display; requires a password |
| **Audio** | PulseAudio passthrough (off by default) |
| **Mode** | Daemon (background) or foreground |
| **Snapshot** | Boot into a saved snapshot by name |
| **Live Migration** | Send, Receive, or Monitor-only mode (see below) |

#### Connecting to a Running VM

```sh
ssh user@localhost -p 4444
vncviewer 127.0.0.1:5909
remote-viewer spice://localhost:5910
```

---

### Control Tab

Displays all running VMs with PID, uptime, disk name, and port info.
Auto-refreshes every 5 seconds.

| Button | Action |
|---|---|
| ⟳ Refresh | Force-refresh the VM list |
| 📊 Status | Show detailed status for the selected VM |
| 🛑 Stop Selected | Stop the selected VM |
| 💀 Stop All | Stop all running VMs |

---

### Disk Manager Tab

| Button | Action |
|---|---|
| ➕ Create Image | Create a new QCOW2 disk image |
| ℹ Image Info | Show virtual/actual size and format |
| 📏 Resize Image | Grow an existing image (shrinking not supported) |
| 🔄 Convert Image | Convert between `qcow2` / `raw` / `vmdk` / `vdi` / `vpc` / `vhdx` |
| 📋 List Snapshots | List all snapshots inside a disk image |
| 📸 Create Snapshot | Save the current VM state as a named snapshot |
| ⏪ Apply Snapshot | Restore the disk to a saved snapshot |
| 🗑 Delete Snapshot | Remove a snapshot from the image |

> Shut down the VM before creating a snapshot to ensure a consistent disk state.

---

## Live Migration

Live migration transfers a running VM from one machine to another without
shutting it down. RAM and CPU state are streamed over the network. The disk
image must already exist on the destination (copy it manually or use shared
storage such as NFS).

### Automatic (recommended)

**System B — destination (run first):**

```sh
# GUI: Live Migration → Receive, Listen port: 5555 → Launch VM
qemu-run --disk vm.qcow2 --incoming tcp:0:5555
```

**System A — source:**

```sh
# GUI: Live Migration → Send, Destination: 192.168.1.x:5555 → Launch VM
qemu-run --disk vm.qcow2 --migrate 192.168.1.x:5555
```

`qemu-run` opens an internal monitor, issues the migrate command, polls
status every second, and shuts down the source QEMU automatically on
completion.

### Manual (advanced)

```sh
# System A — expose the QEMU monitor
qemu-run --disk vm.qcow2 --monitor 4445

# From another terminal on System A
telnet localhost 4445
(qemu) migrate tcp:192.168.1.x:5555
```

### Notes

- Both machines should run the same QEMU version and the same acceleration
  mode (KVM or TCG).
- Open the migration port (e.g. `5555`) in the firewall on System B.
- After migration completes, reconnect your VNC/SPICE client to System B.

---

## Snapshots

Snapshots are stored inside the QCOW2 file — no extra files needed.

```sh
./bin/qemu-disk snapshot list    --name bin/debian.qcow2
./bin/qemu-disk snapshot create  --name bin/debian.qcow2 --snap before-upgrade
./bin/qemu-disk snapshot apply   --name bin/debian.qcow2 --snap before-upgrade
./bin/qemu-disk snapshot delete  --name bin/debian.qcow2 --snap before-upgrade

# Boot into a snapshot
./bin/qemu-run --disk bin/debian.qcow2 --snapshot before-upgrade
```

---

## CLI Reference

All binaries use explicit flags — no interactive prompts.

### qemu-run

```
qemu-run --disk <path> [options]

Required:
  --disk <path>

Profile:
  --profile normal|lowram|<saved-name>     default: normal

VM config:
  --arch    x86_64|aarch64                 default: x86_64
  --ram     <MB>
  --cpu     <n>
  --iso     <path>

Network:
  --ssh          <port>                    default: 4444
  --extra-fwds   hostport:guestport,...

Display:
  --vnc          <port>                    default: 5909
  --no-vnc
  --spice        <port>
  --spice-pass   <password>
  --no-spice

Audio:
  --audio
  --no-audio

Mode:
  --fg                                     foreground (default: daemon)

Snapshot:
  --snapshot <n>                           boot into saved snapshot

Live Migration:
  --migrate  <ip:port>                     send this VM to destination
  --incoming tcp:0:<port>                  receive an incoming VM
  --monitor  <port>                        expose QEMU monitor via TCP
```

**Examples:**

```sh
qemu-run --disk bin/alpine.qcow2
qemu-run --disk bin/debian.qcow2 --ram 8192 --cpu 4 --vnc 5901 --fg
qemu-run --disk bin/alpine.qcow2 --spice 5910 --spice-pass hunter2 \
         --extra-fwds 8080:8080
qemu-run --disk bin/debian.qcow2 --snapshot before-upgrade
qemu-run --disk bin/debian.qcow2 --migrate 192.168.1.50:5555
qemu-run --disk bin/debian.qcow2 --incoming tcp:0:5555
```

---

### qemu-ctl

```sh
qemu-ctl list      # list running VMs with PID, ports, and uptime
qemu-ctl status    # detailed status for all VMs
qemu-ctl stop      # stop a VM
qemu-ctl debug     # raw parsed fields and lock directory contents
```

---

### qemu-disk

```sh
qemu-disk create           --name <file> --size <size>
qemu-disk info             --name <file>
qemu-disk resize           --name <file> --size <size>
qemu-disk convert          --src <file>  --dst <file> --fmt <format>
qemu-disk snapshot list    --name <file>
qemu-disk snapshot create  --name <file> --snap <n>
qemu-disk snapshot apply   --name <file> --snap <n>
qemu-disk snapshot delete  --name <file> --snap <n>
```

Size format: integer followed by `K`, `M`, `G`, or `T` — e.g. `20G`, `512M`, `2T`.

Supported convert formats: `qcow2` `raw` `vmdk` `vdi` `vpc` `vhdx` `qed` `parallels`

---

## Lock File System

Running VMs create lock files in `~/.virt-forge-locks/`:

```
~/.virt-forge-locks/
├── qemu_4444.pid        # QEMU PID (keyed by SSH port)
├── ssh_4444.lock
├── vnc_5909.lock        # keyed by VNC port
└── spice_5910.lock
```

- Locks are removed automatically when the VM stops
- `qemu-ctl status` reports stale locks from crashed sessions
- Port conflicts are caught before launch

---

## KVM Hardware Acceleration

```sh
ls /dev/kvm                    # check availability
groups $USER                   # check group membership
sudo usermod -aG kvm $USER     # add yourself (re-login required)
```

Without KVM, QEMU falls back to TCG (software emulation) — functional but slower.

---

## Environment Variables

```sh
# Override project root (default: auto-detected from binary location)
VIRT_FORGE_ROOT=/opt/virt-forge ./virt-forge

# Override bin directory
VIRT_FORGE_BIN=/custom/bin ./virt-forge
```

---

## Dependencies

| Component | Requirements |
|---|---|
| VM Launcher / CTL / Disk | `qemu-system-x86_64` `qemu-system-aarch64` `qemu-utils` |
| Build — Go | `go` 1.21+ |
| Build — GUI | `python3` · `python3-venv` (PyInstaller + PyQt6 auto-installed in temp venv) |
| GUI Runtime (one-dir) | `_internal/` (bundled — no separate install needed) |
| GUI Runtime (one-file) | none — fully self-contained |

---

## License

MIT — see [LICENSE](LICENSE) for details.
