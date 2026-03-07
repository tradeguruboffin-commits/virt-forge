#!/usr/bin/env python3
"""
virt-forge — QEMU Control Panel
Requires: PyQt6  (pip install PyQt6)
"""
import sys
import os
import glob
import subprocess
import secrets
import string
from pathlib import Path

from PyQt6.QtWidgets import (
    QApplication, QMainWindow, QWidget, QTabWidget,
    QVBoxLayout, QHBoxLayout, QFormLayout, QGridLayout,
    QLabel, QLineEdit, QSpinBox, QComboBox, QCheckBox,
    QPushButton, QTextEdit, QListWidget, QListWidgetItem,
    QMessageBox, QDialog, QDialogButtonBox, QSplitter,
    QGroupBox, QFileDialog, QScrollArea,
)
from PyQt6.QtCore import Qt, QTimer, QThread, pyqtSignal
from PyQt6.QtGui import QFont

# ── Paths ─────────────────────────────────────────────────────
_env_root = os.environ.get("VIRT_FORGE_ROOT")
ROOT    = Path(_env_root).resolve() if _env_root else Path(__file__).resolve().parent.parent
BIN_DIR = Path(os.environ.get("VIRT_FORGE_BIN", str(ROOT / "bin"))).resolve()

PROFILES_DIR = Path.home() / ".vm_profiles"


# =============================================================
#  WORKER THREAD — non-blocking subprocess
# =============================================================

class Worker(QThread):
    finished = pyqtSignal(str, str)   # stdout, stderr

    def __init__(self, cmd, stdin_data=None, timeout=30, parent=None):
        super().__init__(parent)
        self.cmd        = cmd
        self.stdin_data = stdin_data
        self.timeout    = timeout

    def run(self):
        try:
            r = subprocess.run(
                self.cmd,
                input=self.stdin_data,
                capture_output=True,
                text=True,
                timeout=self.timeout,
            )
            self.finished.emit(r.stdout, r.stderr)
        except subprocess.TimeoutExpired:
            self.finished.emit("", f"⏱ Timeout after {self.timeout}s — try again")
        except Exception as e:
            self.finished.emit("", f"❌ Error: {e}")


# =============================================================
#  DISK OPERATION DIALOG
# =============================================================

class DiskDialog(QDialog):
    """
    Form dialog for disk operations.
    File fields have Browse buttons; all fields are required (no defaults).
    """
    def __init__(self, mode, parent=None):
        super().__init__(parent)
        self.mode = mode
        self.setWindowTitle({"create": "Create Image",
                             "info":   "Image Info",
                             "resize": "Resize Image",
                             "convert":"Convert Image"}[mode])
        self.setMinimumWidth(860)
        self.setStyleSheet(parent.styleSheet() if parent else "")
        self._build()

    def _build(self):
        layout = QFormLayout(self)
        layout.setSpacing(20)
        layout.setContentsMargins(40, 40, 40, 40)
        self.fields = {}

        def add_text(label, key, placeholder=""):
            w = QLineEdit()
            w.setPlaceholderText(placeholder)
            w.setMinimumHeight(36)
            layout.addRow(label, w)
            self.fields[key] = w

        def add_file(label, key, placeholder="", save=False):
            """File field with Browse button. save=True → Save dialog."""
            w = QLineEdit()
            w.setPlaceholderText(placeholder)
            w.setMinimumHeight(36)
            btn = QPushButton("Browse…")
            btn.setFixedWidth(120)
            if save:
                btn.clicked.connect(lambda: self._browse_save(w))
            else:
                btn.clicked.connect(lambda: self._browse_open(w))
            row = QWidget()
            rl  = QHBoxLayout(row)
            rl.setContentsMargins(0, 0, 0, 0)
            rl.addWidget(w, stretch=1)
            rl.addWidget(btn)
            layout.addRow(label, row)
            self.fields[key] = w

        if self.mode == "create":
            add_file("Image path:", "name", "e.g. /path/to/myvm.qcow2", save=True)
            add_text("Size:",       "size", "e.g. 20G, 512M, 2T")

        elif self.mode == "info":
            add_file("Image path:", "name", "e.g. /path/to/alpine.qcow2")

        elif self.mode == "resize":
            add_file("Image path:", "name", "e.g. /path/to/alpine.qcow2")
            add_text("New size:",   "size", "e.g. 30G, 512M, 2T")

        elif self.mode == "convert":
            add_file("Source image:",      "src", "e.g. /path/to/alpine.qcow2")
            add_file("Destination image:", "dst", "e.g. /path/to/alpine.raw", save=True)
            fmt_combo = QComboBox()
            for f in ["qcow2", "raw", "vmdk", "vdi", "vpc", "vhdx", "qed", "parallels"]:
                fmt_combo.addItem(f)
            layout.addRow("Format:", fmt_combo)
            self.fields["fmt"] = fmt_combo

        buttons = QDialogButtonBox(
            QDialogButtonBox.StandardButton.Ok |
            QDialogButtonBox.StandardButton.Cancel
        )
        buttons.accepted.connect(self.accept)
        buttons.rejected.connect(self.reject)
        layout.addRow(buttons)

    def _browse_open(self, field):
        path, _ = QFileDialog.getOpenFileName(
            self, "Select Image", str(BIN_DIR),
            "QCOW2 Images (*.qcow2);;All Images (*.qcow2 *.raw *.vmdk *.vdi *.img);;All Files (*)")
        if path:
            field.setText(path)

    def _browse_save(self, field):
        path, _ = QFileDialog.getSaveFileName(
            self, "Save Image As", str(BIN_DIR),
            "QCOW2 Images (*.qcow2);;All Files (*)")
        if path:
            field.setText(path)

    def get(self, key):
        w = self.fields.get(key)
        if w is None:
            return ""
        if isinstance(w, QComboBox):
            return w.currentText()
        return w.text().strip()


# =============================================================
#  CONSOLE WIDGET
# =============================================================

class Console(QTextEdit):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.setReadOnly(True)
        self.setFont(QFont("Monospace", 14))
        self.setStyleSheet("""
            QTextEdit {
                background: #0d1117;
                color: #e6edf3;
                border: 1px solid #30363d;
                border-radius: 8px;
                padding: 12px;
            }
        """)

    def print(self, text, color=None):
        if color:
            self.append(f'<span style="color:{color}">{text}</span>')
        else:
            self.append(text)
        self.verticalScrollBar().setValue(self.verticalScrollBar().maximum())

    def clear_and_print(self, text):
        self.clear()
        self.append(text)


# =============================================================
#  HELPERS
# =============================================================

def _gen_password(length=16):
    alphabet = string.ascii_letters + string.digits + "@#%"
    return "".join(secrets.choice(alphabet) for _ in range(length))


def _discover_disks():
    """Return list of .qcow2 files found in BIN_DIR."""
    return sorted(glob.glob(str(BIN_DIR / "*.qcow2")))


def _load_saved_profiles():
    """Return list of saved profile names from ~/.vm_profiles/."""
    if not PROFILES_DIR.exists():
        return []
    return sorted(p.name for p in PROFILES_DIR.iterdir() if p.is_file())


# =============================================================
#  VM LAUNCHER TAB
# =============================================================

class VMTab(QWidget):
    """
    Form-based VM launcher — builds qemu-run args directly,
    no xterm / interactive TUI required.

    Layout:
      ┌─ Profile & Arch ────────────────────────────────────┐
      ├─ Disk ──────────────────────────────────────────────┤
      ├─ Resources (RAM / CPU) ─────────────────────────────┤
      ├─ Network (SSH / Extra fwds) ────────────────────────┤
      ├─ Display (VNC / SPICE) ─────────────────────────────┤
      ├─ Options (Audio / Mode) ────────────────────────────┤
      └─ Launch button + console ───────────────────────────┘
    """

    def __init__(self, parent=None):
        super().__init__(parent)
        self._workers = []
        self._build()

    # ── Build UI ──────────────────────────────────────────────

    def _build(self):
        outer = QVBoxLayout(self)
        outer.setContentsMargins(0, 0, 0, 0)
        outer.setSpacing(0)

        # Scrollable form area
        scroll = QScrollArea()
        scroll.setWidgetResizable(True)
        scroll.setFrameShape(QScrollArea.Shape.NoFrame)

        form_widget = QWidget()
        root = QVBoxLayout(form_widget)
        root.setSpacing(16)
        root.setContentsMargins(32, 24, 32, 16)

        root.addWidget(self._section_profile())
        root.addWidget(self._section_disk())
        root.addWidget(self._section_resources())
        root.addWidget(self._section_network())
        root.addWidget(self._section_display())
        root.addWidget(self._section_options())
        root.addStretch(1)

        scroll.setWidget(form_widget)
        outer.addWidget(scroll, stretch=1)

        # ── Launch button + console ───────────────────────────
        btn_area = QWidget()
        btn_layout = QVBoxLayout(btn_area)
        btn_layout.setContentsMargins(32, 8, 32, 8)
        btn_layout.setSpacing(8)

        self.launch_btn = QPushButton("🚀   Launch VM")
        self.launch_btn.setFixedHeight(64)
        self.launch_btn.setStyleSheet("""
            QPushButton {
                background: #238636;
                font-size: 20px;
                font-weight: bold;
                border-radius: 8px;
            }
            QPushButton:hover   { background: #2ea043; }
            QPushButton:pressed { background: #196127; }
            QPushButton:disabled{ background: #21262d; color: #8b949e; }
        """)
        self.launch_btn.clicked.connect(self.launch_vm)
        btn_layout.addWidget(self.launch_btn)

        self.console = Console()
        self.console.setFixedHeight(160)
        btn_layout.addWidget(self.console)

        outer.addWidget(btn_area)

    def _group(self, title):
        g = QGroupBox(title)
        g.setLayout(QFormLayout())
        g.layout().setSpacing(12)
        g.layout().setContentsMargins(20, 16, 20, 16)
        return g

    # ── Profile & Architecture ─────────────────────────────────

    def _section_profile(self):
        g = self._group("Profile & Architecture")
        fl = g.layout()

        self.profile_combo = QComboBox()
        self.profile_combo.addItem("normal  — 4 GB RAM, 2 CPU", "normal")
        self.profile_combo.addItem("lowram  — 2 GB RAM, 1 CPU", "lowram")
        for name in _load_saved_profiles():
            self.profile_combo.addItem(f"💾  {name}", name)
        self.profile_combo.currentIndexChanged.connect(self._on_profile_changed)
        fl.addRow("Profile:", self.profile_combo)

        self.arch_combo = QComboBox()
        self.arch_combo.addItem("x86_64  (PC / most Linux ISOs)", "x86_64")
        self.arch_combo.addItem("aarch64  (ARM64 / Raspberry Pi)", "aarch64")
        fl.addRow("Architecture:", self.arch_combo)

        return g

    # ── Disk ──────────────────────────────────────────────────

    def _section_disk(self):
        g = self._group("Disk Image")
        fl = g.layout()

        # Auto-discovered disks
        self.disk_combo = QComboBox()
        self.disk_combo.setMaxVisibleItems(8)
        self._refresh_disk_combo()
        self.disk_combo.currentIndexChanged.connect(self._on_disk_combo_changed)

        refresh_btn = QPushButton("⟳")
        refresh_btn.setFixedWidth(48)
        refresh_btn.setToolTip("Rescan bin/ for .qcow2 files")
        refresh_btn.clicked.connect(self._refresh_disk_combo)

        row = QWidget()
        rl  = QHBoxLayout(row)
        rl.setContentsMargins(0, 0, 0, 0)
        rl.addWidget(self.disk_combo, stretch=1)
        rl.addWidget(refresh_btn)
        fl.addRow("Disk:", row)

        # Manual path override
        self.disk_path = QLineEdit()
        self.disk_path.setPlaceholderText("Or enter / paste full path to .qcow2 …")

        browse_btn = QPushButton("Browse…")
        browse_btn.setFixedWidth(120)
        browse_btn.clicked.connect(self._browse_disk)

        row2 = QWidget()
        rl2  = QHBoxLayout(row2)
        rl2.setContentsMargins(0, 0, 0, 0)
        rl2.addWidget(self.disk_path, stretch=1)
        rl2.addWidget(browse_btn)
        fl.addRow("Manual path:", row2)

        # ISO for first boot
        self.iso_path = QLineEdit()
        self.iso_path.setPlaceholderText("Optional — leave blank to boot from disk")

        iso_browse = QPushButton("Browse…")
        iso_browse.setFixedWidth(120)
        iso_browse.clicked.connect(self._browse_iso)

        row3 = QWidget()
        rl3  = QHBoxLayout(row3)
        rl3.setContentsMargins(0, 0, 0, 0)
        rl3.addWidget(self.iso_path, stretch=1)
        rl3.addWidget(iso_browse)
        fl.addRow("Boot ISO:", row3)

        return g

    # ── Resources ─────────────────────────────────────────────

    def _section_resources(self):
        g = self._group("Resources")
        fl = g.layout()

        self.ram_spin = QSpinBox()
        self.ram_spin.setRange(256, 131072)
        self.ram_spin.setSingleStep(512)
        self.ram_spin.setValue(4096)
        self.ram_spin.setSuffix("  MB")
        fl.addRow("RAM:", self.ram_spin)

        self.cpu_spin = QSpinBox()
        self.cpu_spin.setRange(1, 64)
        self.cpu_spin.setValue(2)
        self.cpu_spin.setSuffix("  cores")
        fl.addRow("CPU:", self.cpu_spin)

        return g

    # ── Network ───────────────────────────────────────────────

    def _section_network(self):
        g = self._group("Network")
        fl = g.layout()

        self.ssh_spin = QSpinBox()
        self.ssh_spin.setRange(1024, 65535)
        self.ssh_spin.setValue(4444)
        fl.addRow("SSH port:", self.ssh_spin)

        self.extra_fwds = QLineEdit()
        self.extra_fwds.setPlaceholderText("e.g. 8080:8080,5432:5432")
        fl.addRow("Extra port forwards:", self.extra_fwds)

        return g

    # ── Display ───────────────────────────────────────────────

    def _section_display(self):
        g = self._group("Display")
        fl = g.layout()

        # VNC
        self.vnc_check = QCheckBox("Enable VNC")
        self.vnc_check.setChecked(True)
        self.vnc_check.toggled.connect(self._on_vnc_toggled)
        fl.addRow("VNC:", self.vnc_check)

        self.vnc_port_spin = QSpinBox()
        self.vnc_port_spin.setRange(5900, 5999)
        self.vnc_port_spin.setValue(5909)
        fl.addRow("VNC port:", self.vnc_port_spin)

        # SPICE
        self.spice_check = QCheckBox("Enable SPICE")
        self.spice_check.setChecked(False)
        self.spice_check.toggled.connect(self._on_spice_toggled)
        fl.addRow("SPICE:", self.spice_check)

        self.spice_port_spin = QSpinBox()
        self.spice_port_spin.setRange(5900, 65535)
        self.spice_port_spin.setValue(5910)
        self.spice_port_spin.setEnabled(False)
        fl.addRow("SPICE port:", self.spice_port_spin)

        self.spice_pass_edit = QLineEdit()
        self.spice_pass_edit.setPlaceholderText("SPICE password (required when SPICE is on)")
        self.spice_pass_edit.setEnabled(False)

        self._spice_gen_btn = QPushButton("Generate")
        self._spice_gen_btn.setFixedWidth(130)
        self._spice_gen_btn.setEnabled(False)
        self._spice_gen_btn.clicked.connect(self._gen_spice_pass)

        row = QWidget()
        rl  = QHBoxLayout(row)
        rl.setContentsMargins(0, 0, 0, 0)
        rl.addWidget(self.spice_pass_edit, stretch=1)
        rl.addWidget(self._spice_gen_btn)
        fl.addRow("SPICE password:", row)

        return g

    # ── Options ───────────────────────────────────────────────

    def _section_options(self):
        g = self._group("Options")
        fl = g.layout()

        self.audio_check = QCheckBox("Enable audio  (requires PulseAudio)")
        self.audio_check.setChecked(False)
        fl.addRow("Audio:", self.audio_check)

        self.daemon_check = QCheckBox("Run in background (daemon)")
        self.daemon_check.setChecked(True)
        fl.addRow("Mode:", self.daemon_check)

        return g

    # ── Slots ─────────────────────────────────────────────────

    def _on_profile_changed(self, _idx):
        profile = self.profile_combo.currentData()
        if profile == "normal":
            self.ram_spin.setValue(4096)
            self.cpu_spin.setValue(2)
            self.audio_check.setChecked(False)
        elif profile == "lowram":
            self.ram_spin.setValue(2048)
            self.cpu_spin.setValue(1)
            self.audio_check.setChecked(False)

    def _on_disk_combo_changed(self, idx):
        if idx > 0:
            self.disk_path.clear()

    def _on_vnc_toggled(self, checked):
        self.vnc_port_spin.setEnabled(checked)

    def _on_spice_toggled(self, checked):
        self.spice_port_spin.setEnabled(checked)
        self.spice_pass_edit.setEnabled(checked)
        self._spice_gen_btn.setEnabled(checked)

    def _gen_spice_pass(self):
        self.spice_pass_edit.setText(_gen_password())

    # ── Helpers ───────────────────────────────────────────────

    def _refresh_disk_combo(self):
        current = self.disk_combo.currentData()
        self.disk_combo.blockSignals(True)
        self.disk_combo.clear()
        self.disk_combo.addItem("(select from bin/)", "")
        for path in _discover_disks():
            self.disk_combo.addItem(Path(path).name, path)
        if current:
            idx = self.disk_combo.findData(current)
            if idx >= 0:
                self.disk_combo.setCurrentIndex(idx)
        self.disk_combo.blockSignals(False)

    def _browse_disk(self):
        path, _ = QFileDialog.getOpenFileName(
            self, "Select QCOW2 Disk Image", str(BIN_DIR),
            "QCOW2 Images (*.qcow2);;All Files (*)")
        if path:
            self.disk_path.setText(path)
            self.disk_combo.setCurrentIndex(0)

    def _browse_iso(self):
        path, _ = QFileDialog.getOpenFileName(
            self, "Select Boot ISO", str(Path.home()),
            "ISO Images (*.iso);;All Files (*)")
        if path:
            self.iso_path.setText(path)

    def _effective_disk(self):
        """Manual path field takes priority over combo selection."""
        manual = self.disk_path.text().strip()
        if manual:
            return manual
        return self.disk_combo.currentData() or ""

    # ── Build qemu-run args list ───────────────────────────────

    def _build_args(self):
        """
        Validate form and return (args_list, error_string).
        Returns (None, msg) on validation failure.
        """
        qemu_run = BIN_DIR / "qemu-run"
        if not qemu_run.exists():
            return None, f"qemu-run not found:\n{qemu_run}\n\nRun: ./build/make qemu"

        disk = self._effective_disk()
        if not disk:
            return None, "Please select or enter a disk image."
        if not Path(disk).exists():
            return None, f"Disk file not found:\n{disk}"

        if self.spice_check.isChecked() and not self.spice_pass_edit.text().strip():
            return None, "SPICE is enabled but no password is set.\n\nEnter a password or click Generate."

        iso = self.iso_path.text().strip()
        if iso and not Path(iso).exists():
            return None, f"ISO file not found:\n{iso}"

        args = [str(qemu_run)]
        args += ["--profile", self.profile_combo.currentData()]
        args += ["--arch",    self.arch_combo.currentData()]
        args += ["--disk",    disk]
        args += ["--ram",     str(self.ram_spin.value())]
        args += ["--cpu",     str(self.cpu_spin.value())]
        args += ["--ssh",     str(self.ssh_spin.value())]

        if iso:
            args += ["--iso", iso]

        extra = self.extra_fwds.text().strip()
        if extra:
            args += ["--extra-fwds", extra]

        if self.vnc_check.isChecked():
            args += ["--vnc", str(self.vnc_port_spin.value())]
        else:
            args.append("--no-vnc")

        if self.spice_check.isChecked():
            args += ["--spice",      str(self.spice_port_spin.value())]
            args += ["--spice-pass", self.spice_pass_edit.text().strip()]
        else:
            args.append("--no-spice")

        args.append("--audio" if self.audio_check.isChecked() else "--no-audio")

        if not self.daemon_check.isChecked():
            args.append("--fg")

        return args, ""

    # ── Launch ────────────────────────────────────────────────

    def launch_vm(self):
        args, err = self._build_args()
        if err:
            QMessageBox.warning(self, "Cannot Launch", err)
            return

        self.console.clear_and_print("$ " + " ".join(args))
        self.launch_btn.setEnabled(False)

        # daemon mode: qemu-run exits in ~1s after starting QEMU
        # foreground mode: runs until VM stops — no timeout
        timeout = 5 if self.daemon_check.isChecked() else None
        w = Worker(args, timeout=timeout)
        self._workers.append(w)
        w.finished.connect(self._on_launch_done)
        w.finished.connect(lambda: self._workers.remove(w) if w in self._workers else None)
        w.start()

    def _on_launch_done(self, stdout, stderr):
        self.launch_btn.setEnabled(True)
        output = (stdout + stderr).strip()
        self.console.clear_and_print(output or "(no output)")
        if "✅" in output:
            self.console.print("\n✔ VM is running.", "#3fb950")
        elif stderr.strip():
            self.console.print("\n✘ Launch failed.", "#f85149")


# =============================================================
#  CONTROL TAB
# =============================================================

class ControlTab(QWidget):
    def __init__(self, parent=None):
        super().__init__(parent)
        self._workers = []
        self._build()
        self._start_refresh()

    def _build(self):
        root = QVBoxLayout(self)
        root.setSpacing(20)
        root.setContentsMargins(32, 32, 32, 32)

        splitter = QSplitter(Qt.Orientation.Vertical)

        top = QWidget()
        top_layout = QVBoxLayout(top)
        top_layout.setContentsMargins(0, 0, 0, 0)

        hdr = QHBoxLayout()
        hdr.addWidget(QLabel("Running VMs"))
        hdr.addStretch()
        self.refresh_btn = QPushButton("⟳ Refresh")
        self.refresh_btn.setFixedWidth(180)
        self.refresh_btn.clicked.connect(self.do_refresh)
        hdr.addWidget(self.refresh_btn)
        top_layout.addLayout(hdr)

        self.vm_list = QListWidget()
        self.vm_list.setAlternatingRowColors(True)
        top_layout.addWidget(self.vm_list)

        btn_row = QHBoxLayout()
        for label, slot in [
            ("📊 Status",        self.show_status),
            ("🛑 Stop Selected", self.stop_selected),
            ("💀 Stop All",      self.stop_all),
        ]:
            b = QPushButton(label)
            b.clicked.connect(slot)
            btn_row.addWidget(b)
        top_layout.addLayout(btn_row)

        splitter.addWidget(top)

        self.console = Console()
        splitter.addWidget(self.console)
        splitter.setSizes([560, 640])

        root.addWidget(splitter)

    def _start_refresh(self):
        self.timer = QTimer(self)
        self.timer.timeout.connect(self.do_refresh)
        self.timer.start(5000)
        self.do_refresh()

    def _run(self, cmd, stdin_data=None, on_done=None):
        w = Worker(cmd, stdin_data)
        self._workers.append(w)
        if on_done:
            w.finished.connect(on_done)
        w.finished.connect(lambda: self._workers.remove(w) if w in self._workers else None)
        w.start()

    def _ctl(self, *args):
        ctl = BIN_DIR / "qemu-ctl"
        if not ctl.exists():
            self.console.print(f"❌ qemu-ctl not found: {ctl}", "#f85149")
            return None
        return [str(ctl)] + list(args)

    def do_refresh(self):
        cmd = self._ctl("list")
        if cmd:
            self._run(cmd, on_done=self._update_list)

    def _update_list(self, stdout, stderr):
        self.vm_list.clear()
        for line in stdout.splitlines():
            stripped = line.strip()
            if stripped.startswith("["):
                self.vm_list.addItem(QListWidgetItem(stripped))
        if self.vm_list.count() == 0:
            self.vm_list.addItem("(no running VMs)")

    def show_status(self):
        cmd = self._ctl("status")
        if cmd:
            self.console.clear_and_print("Fetching status…")
            self._run(cmd, on_done=lambda out, err:
                self.console.clear_and_print(out or err or "(no output)"))

    def stop_selected(self):
        row = self.vm_list.currentRow()
        if row < 0 or self.vm_list.item(row).text().startswith("("):
            QMessageBox.warning(self, "No Selection", "Please select a VM first.")
            return
        reply = QMessageBox.question(self, "Confirm Stop",
            f"Stop VM #{row + 1}?",
            QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.No)
        if reply != QMessageBox.StandardButton.Yes:
            return
        cmd = self._ctl("stop")
        if cmd:
            self._run(cmd, stdin_data=f"{row + 1}\n", on_done=self._after_stop)

    def stop_all(self):
        reply = QMessageBox.question(self, "Confirm Stop All",
            "Stop ALL running VMs?",
            QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.No)
        if reply != QMessageBox.StandardButton.Yes:
            return
        cmd = self._ctl("stop")
        if cmd:
            self._run(cmd, stdin_data="all\n", on_done=self._after_stop)

    def _after_stop(self, stdout, stderr):
        self.console.clear_and_print(stdout or stderr or "(no output)")
        QTimer.singleShot(1000, self.do_refresh)


# =============================================================
#  DISK TAB
# =============================================================

class DiskTab(QWidget):
    def __init__(self, parent=None):
        super().__init__(parent)
        self._workers = []
        self._build()

    def _build(self):
        root = QVBoxLayout(self)
        root.setSpacing(20)
        root.setContentsMargins(32, 32, 32, 32)

        grid = QGridLayout()
        grid.setSpacing(16)

        ops = [
            ("➕  Create Image",  "create",  "#238636", "#2ea043"),
            ("ℹ   Image Info",   "info",    "#0078d4", "#0090ff"),
            ("📏  Resize Image", "resize",  "#9e6a03", "#bb8009"),
            ("🔄  Convert Image","convert", "#6e40c9", "#8250df"),
        ]
        for i, (label, mode, bg, hover) in enumerate(ops):
            b = QPushButton(label)
            b.setFixedHeight(96)
            b.setStyleSheet(f"""
                QPushButton {{
                    background: {bg};
                    font-size: 18px;
                    border-radius: 8px;
                }}
                QPushButton:hover {{ background: {hover}; }}
            """)
            b.clicked.connect(lambda _, m=mode: self.open_dialog(m))
            grid.addWidget(b, i // 2, i % 2)

        root.addLayout(grid)
        self.console = Console()
        root.addWidget(self.console)

    def open_dialog(self, mode):
        dlg = DiskDialog(mode, parent=self)
        if dlg.exec() != QDialog.DialogCode.Accepted:
            return

        disk_bin = BIN_DIR / "qemu-disk"
        if not disk_bin.exists():
            QMessageBox.critical(self, "Error",
                f"Binary not found:\n{disk_bin}\n\nRun: ./build/make qemu-disk")
            return

        def bin_path(filename):
            p = Path(filename)
            if p.is_absolute() or p.parent != Path("."):
                return str(p)
            return str(BIN_DIR / p)

        # Build args for CLI interface: qemu-disk <cmd> --flag value
        if mode == "create":
            name = dlg.get("name")
            size = dlg.get("size")
            if not name or not size:
                QMessageBox.warning(self, "Missing fields",
                    "Image path and size are both required.")
                return
            args = [str(disk_bin), "create",
                    "--name", bin_path(name), "--size", size]
            self.console.clear_and_print(f"Creating {bin_path(name)} ({size})\u2026")

        elif mode == "info":
            name = dlg.get("name")
            if not name:
                QMessageBox.warning(self, "Missing fields", "Image path is required.")
                return
            args = [str(disk_bin), "info", "--name", bin_path(name)]
            self.console.clear_and_print(f"Fetching info for {bin_path(name)}\u2026")

        elif mode == "resize":
            name = dlg.get("name")
            size = dlg.get("size")
            if not name or not size:
                QMessageBox.warning(self, "Missing fields",
                    "Image path and new size are both required.")
                return
            args = [str(disk_bin), "resize",
                    "--name", bin_path(name), "--size", size]
            self.console.clear_and_print(f"Resizing {bin_path(name)} \u2192 {size}\u2026")

        elif mode == "convert":
            src = dlg.get("src")
            dst = dlg.get("dst")
            fmt = dlg.get("fmt")
            if not src or not dst or not fmt:
                QMessageBox.warning(self, "Missing fields",
                    "Source, destination, and format are all required.")
                return
            args = [str(disk_bin), "convert",
                    "--src", bin_path(src), "--dst", bin_path(dst), "--fmt", fmt]
            self.console.clear_and_print(
                f"Converting {bin_path(src)} \u2192 {bin_path(dst)} ({fmt})\u2026")

        w = Worker(args, timeout=None)
        self._workers.append(w)
        w.finished.connect(self._on_done)
        w.finished.connect(lambda: self._workers.remove(w) if w in self._workers else None)
        w.start()

    def _on_done(self, stdout, stderr):
        self.console.clear_and_print((stdout + stderr).strip() or "(no output)")


# =============================================================
#  MAIN WINDOW
# =============================================================

class VirtForge(QMainWindow):

    def __init__(self):
        super().__init__()
        self.setWindowTitle("Virt-Forge  —  QEMU Control Panel")
        self.resize(1100, 820)
        self.setMinimumSize(900, 680)
        self.setStyleSheet(self._theme())

        tabs = QTabWidget()
        tabs.setDocumentMode(True)
        tabs.addTab(VMTab(self),      "🖥  VM Launcher")
        tabs.addTab(ControlTab(self), "⚙  Control")
        tabs.addTab(DiskTab(self),    "💾  Disk Manager")
        self.setCentralWidget(tabs)

        self.statusBar().showMessage(f"bin: {BIN_DIR}")
        self._tabs = [tabs.widget(i) for i in range(tabs.count())]

    def closeEvent(self, event):
        for tab in self._tabs:
            for w in list(getattr(tab, "_workers", [])):
                if w.isRunning():
                    w.terminate()
                    w.wait(2000)
        event.accept()

    def _theme(self):
        return """
        QMainWindow, QWidget {
            background-color: #0d1117;
            color: #e6edf3;
            font-family: 'DejaVu Sans', 'Liberation Sans', sans-serif;
            font-size: 18px;
        }
        QTabWidget::pane {
            border: 1px solid #30363d;
            border-radius: 8px;
        }
        QTabBar::tab {
            background: #161b22;
            color: #8b949e;
            padding: 16px 36px;
            border: 1px solid #30363d;
            border-bottom: none;
        }
        QTabBar::tab:selected {
            background: #0d1117;
            color: #e6edf3;
            border-bottom: 2px solid #58a6ff;
        }
        QGroupBox {
            border: 1px solid #30363d;
            border-radius: 12px;
            margin-top: 10px;
            padding-top: 12px;
            font-weight: bold;
            color: #8b949e;
        }
        QGroupBox::title {
            subcontrol-origin: margin;
            left: 10px;
        }
        QLineEdit, QSpinBox, QComboBox, QTextEdit {
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 8px;
            padding: 8px 12px;
            color: #e6edf3;
            min-height: 36px;
            font-size: 18px;
        }
        QSpinBox::up-button, QSpinBox::down-button { width: 24px; }
        QLineEdit:focus, QSpinBox:focus, QComboBox:focus {
            border-color: #58a6ff;
        }
        QPushButton {
            background: #21262d;
            border: 1px solid #30363d;
            border-radius: 8px;
            padding: 10px 24px;
            color: #e6edf3;
        }
        QPushButton:hover   { background: #30363d; border-color: #58a6ff; }
        QPushButton:pressed { background: #161b22; }
        QListWidget {
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 8px;
            alternate-background-color: #0d1117;
        }
        QListWidget::item:selected {
            background: #1f3f6a;
            color: #58a6ff;
        }
        QSplitter::handle { background: #30363d; }
        QScrollBar:vertical {
            background: #161b22;
            width: 16px;
        }
        QScrollBar::handle:vertical {
            background: #30363d;
            border-radius: 8px;
        }
        QCheckBox::indicator {
            width: 28px; height: 28px;
            border: 1px solid #30363d;
            border-radius: 6px;
            background: #161b22;
        }
        QCheckBox::indicator:checked {
            background: #238636;
            border-color: #2ea043;
        }
        QStatusBar { color: #8b949e; font-size: 15px; }
        QScrollArea { border: none; background: transparent; }
        """


# =============================================================
#  ENTRY POINT
# =============================================================

if __name__ == "__main__":
    app = QApplication(sys.argv)
    app.setApplicationName("Virt-Forge")

    if hasattr(Qt.ApplicationAttribute, "AA_UseHighDpiPixmaps"):
        app.setAttribute(Qt.ApplicationAttribute.AA_UseHighDpiPixmaps, True)

    window = VirtForge()
    window.show()
    sys.exit(app.exec())
