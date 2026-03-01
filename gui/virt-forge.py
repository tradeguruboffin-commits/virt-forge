#!/usr/bin/env python3
"""
virt-forge — QEMU Control Panel
Requires: PyQt6  (pip install PyQt6)
"""
import sys
import os
import subprocess
import shutil
from pathlib import Path

from PyQt6.QtWidgets import (
    QApplication, QMainWindow, QWidget, QTabWidget,
    QVBoxLayout, QHBoxLayout, QFormLayout, QGridLayout,
    QLabel, QLineEdit, QSpinBox, QComboBox, QCheckBox,
    QPushButton, QTextEdit, QListWidget, QListWidgetItem,
    QMessageBox, QDialog, QDialogButtonBox, QSplitter,
    QGroupBox, QFrame, QSizePolicy,
)
from PyQt6.QtCore import Qt, QTimer, QThread, pyqtSignal, QProcess
from PyQt6.QtGui import QFont, QColor, QPalette, QIcon

# ── Paths ─────────────────────────────────────────────────────
ROOT    = Path(__file__).resolve().parent.parent
BIN_DIR = ROOT / "bin"

# ── Terminal emulator preference list ────────────────────────
TERMINALS = ["xterm", "konsole", "gnome-terminal", "xfce4-terminal", "lxterminal"]


def find_terminal():
    for t in TERMINALS:
        if shutil.which(t):
            return t
    return None


# =============================================================
#  WORKER THREAD — non-blocking subprocess
# =============================================================

class Worker(QThread):
    """Run a subprocess in a background thread; emit stdout when done."""
    finished = pyqtSignal(str, str)   # stdout, stderr

    def __init__(self, cmd, stdin_data=None, parent=None):
        super().__init__(parent)
        self.cmd        = cmd
        self.stdin_data = stdin_data

    def run(self):
        try:
            r = subprocess.run(
                self.cmd,
                input=self.stdin_data,
                capture_output=True,
                text=True,
                timeout=30,
            )
            self.finished.emit(r.stdout, r.stderr)
        except subprocess.TimeoutExpired:
            self.finished.emit("", "⏱ Timeout: command took too long")
        except Exception as e:
            self.finished.emit("", f"❌ Error: {e}")


# =============================================================
#  DISK OPERATION DIALOG  (single form, no popup storm)
# =============================================================

class DiskDialog(QDialog):
    def __init__(self, mode, parent=None):
        super().__init__(parent)
        self.mode = mode
        self.setWindowTitle({"create": "Create Image",
                             "info":   "Image Info",
                             "resize": "Resize Image",
                             "convert":"Convert Image"}[mode])
        self.setMinimumWidth(840)
        self.setStyleSheet(parent.styleSheet() if parent else "")
        self._build()

    def _build(self):
        layout = QFormLayout(self)
        layout.setSpacing(20)
        layout.setContentsMargins(40, 40, 40, 40)

        self.fields = {}

        def add(label, key, placeholder=""):
            w = QLineEdit()
            w.setPlaceholderText(placeholder)
            w.setMinimumHeight(36)
            layout.addRow(label, w)
            self.fields[key] = w

        if self.mode == "create":
            add("Image name:",  "name", "e.g. myvm.qcow2")
            add("Size:",        "size", "e.g. 20G, 512M")

        elif self.mode == "info":
            add("Image name:", "name", "e.g. alpine.qcow2")

        elif self.mode == "resize":
            add("Image name:", "name", "e.g. alpine.qcow2")
            add("New size:",   "size", "e.g. 30G")

        elif self.mode == "convert":
            add("Source image:",      "src",  "e.g. alpine.qcow2")
            add("Destination image:", "dst",  "e.g. alpine.raw")
            add("Format:",            "fmt",  "qcow2 / raw / vmdk / vdi …")

        buttons = QDialogButtonBox(
            QDialogButtonBox.StandardButton.Ok |
            QDialogButtonBox.StandardButton.Cancel
        )
        buttons.accepted.connect(self.accept)
        buttons.rejected.connect(self.reject)
        layout.addRow(buttons)

    def get(self, key):
        return self.fields[key].text().strip()


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
#  VM TAB
# =============================================================

class VMTab(QWidget):
    def __init__(self, parent=None):
        super().__init__(parent)
        self._build()

    def _build(self):
        root = QVBoxLayout(self)
        root.setSpacing(20)
        root.setContentsMargins(32, 32, 32, 32)

        # ── Launch button ─────────────────────────────────────
        self.launch_btn = QPushButton("🚀   Launch VM in Terminal")
        self.launch_btn.setFixedHeight(80)
        self.launch_btn.setStyleSheet("""
            QPushButton {
                background: #238636;
                font-size: 20px;
                font-weight: bold;
                border-radius: 8px;
            }
            QPushButton:hover  { background: #2ea043; }
            QPushButton:pressed{ background: #196127; }
        """)
        self.launch_btn.clicked.connect(self.launch_vm)
        root.addWidget(self.launch_btn)

        self.status_label = QLabel("")
        self.status_label.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.status_label.setStyleSheet("font-size: 15px;")
        root.addWidget(self.status_label)

        # ── User Guide ────────────────────────────────────────
        guide = QTextEdit()
        guide.setReadOnly(True)
        guide.setStyleSheet("""
            QTextEdit {
                background: #161b22;
                border: 1px solid #30363d;
                border-radius: 8px;
                padding: 16px;
                font-size: 15px;
                color: #e6edf3;
            }
        """)
        guide.setHtml("""
<style>
  body  { font-family: 'DejaVu Sans', sans-serif; font-size: 15px;
          color: #e6edf3; line-height: 1.7; }
  h2    { color: #58a6ff; margin-top: 0; font-size: 17px; }
  h3    { color: #3fb950; font-size: 15px; margin-bottom: 4px; }
  code  { background: #0d1117; color: #f78166; padding: 2px 6px;
          border-radius: 4px; font-family: Monospace; }
  table { border-collapse: collapse; width: 100%; margin: 8px 0; }
  td, th { padding: 6px 12px; border: 1px solid #30363d; }
  th    { background: #21262d; color: #8b949e; font-weight: bold; }
  tr:nth-child(even) { background: #0d1117; }
  .tip  { background: #1f3f6a; border-left: 3px solid #58a6ff;
          padding: 8px 12px; border-radius: 0 6px 6px 0;
          margin: 8px 0; }
  .warn { background: #3d2600; border-left: 3px solid #d29922;
          padding: 8px 12px; border-radius: 0 6px 6px 0;
          margin: 8px 0; }
</style>

<h2>🖥 VM Launcher — User Guide</h2>

<p>Click <b>Launch VM in Terminal</b> — an interactive terminal opens where
<code>qemu</code> guides you through all configuration steps.</p>

<h3>📋 Setup Steps (in terminal)</h3>
<table>
  <tr><th>Step</th><th>Prompt</th><th>Example</th></tr>
  <tr><td>1</td><td>Select profile</td><td><code>1</code> Normal / <code>2</code> Low RAM / <code>3</code> Saved</td></tr>
  <tr><td>2</td><td>Architecture</td><td><code>1</code> x86_64 &nbsp; <code>2</code> aarch64</td></tr>
  <tr><td>3</td><td>Select disk</td><td>Pick from list or enter full path</td></tr>
  <tr><td>4</td><td>RAM / CPU</td><td><code>4096</code> MB, <code>2</code> cores</td></tr>
  <tr><td>5</td><td>Boot from ISO?</td><td><code>y</code> for first-time OS install</td></tr>
  <tr><td>6</td><td>VNC / SPICE</td><td>Enable remote display (y/n)</td></tr>
  <tr><td>7</td><td>SSH port</td><td><code>4444</code> → <code>ssh user@localhost -p 4444</code></td></tr>
  <tr><td>8</td><td>SPICE password</td><td>Enter or press Enter for random</td></tr>
  <tr><td>9</td><td>Extra ports</td><td>Forward additional ports (optional)</td></tr>
  <tr><td>10</td><td>Daemon mode</td><td><code>y</code> = background, <code>n</code> = foreground</td></tr>
  <tr><td>11</td><td>Save profile?</td><td><code>y</code> to reuse settings next time</td></tr>
</table>

<h3>🔌 Connecting to a Running VM</h3>
<table>
  <tr><th>Method</th><th>Command / App</th></tr>
  <tr><td>SSH</td><td><code>ssh user@localhost -p 4444</code></td></tr>
  <tr><td>VNC</td><td>Connect to <code>127.0.0.1:590X</code> &nbsp;(display + 5900)</td></tr>
  <tr><td>SPICE</td><td><code>remote-viewer spice://localhost:5902</code></td></tr>
</table>

<h3>💾 Disk Files</h3>
<p>Place <code>.qcow2</code> disk images in the <code>bin/</code> folder.
Use the <b>Disk Manager</b> tab to create new images.</p>

<div class="tip">💡 <b>Tip:</b> Run in daemon mode (<code>y</code>) so the VM keeps
running after the terminal closes. Use the <b>Control</b> tab to monitor and stop it.</div>

<div class="warn">⚠ <b>Note:</b> KVM hardware acceleration requires
<code>/dev/kvm</code> access. Run <code>sudo usermod -aG kvm $USER</code>
and re-login if QEMU falls back to TCG (software) mode.</div>
""")
        root.addWidget(guide)


    def launch_vm(self):
        """
        qemu is a fully interactive TUI. We must launch it inside a real
        terminal emulator — not capture its stdin/stdout.
        """
        qemu = BIN_DIR / "qemu-run"
        if not qemu.exists():
            QMessageBox.critical(self, "Error",
                f"Binary not found:\n{qemu}\n\nRun: ./build/make qemu")
            return

        term = find_terminal()
        if term is None:
            QMessageBox.critical(self, "No Terminal Found",
                "Could not find a terminal emulator.\n"
                "Install one of: " + ", ".join(TERMINALS))
            return

        # Build terminal command — each emulator has different flags
        if term in ("gnome-terminal", "xfce4-terminal", "lxterminal"):
            cmd = [term, "--", str(qemu)]
        elif term == "konsole":
            cmd = [term, "--font-size", "16", "-e", str(qemu)]
        else:  # xterm — -fs sets font size, -fa uses FreeType font
            cmd = [term, "-fa", "Monospace", "-fs", "16", "-e", str(qemu)]

        try:
            subprocess.Popen(cmd)
            self.status_label.setText(
                f"✅ Launched in {term}  —  qemu is running interactively")
            self.status_label.setStyleSheet("color: #3fb950;")
        except Exception as e:
            QMessageBox.critical(self, "Launch Failed", str(e))


# =============================================================
#  CONTROL TAB
# =============================================================

class ControlTab(QWidget):
    def __init__(self, parent=None):
        super().__init__(parent)
        self._workers = []   # keep references so GC doesn't collect them
        self._build()
        self._start_refresh()

    def _build(self):
        root = QVBoxLayout(self)
        root.setSpacing(20)
        root.setContentsMargins(32, 32, 32, 32)

        splitter = QSplitter(Qt.Orientation.Vertical)

        # ── VM list ───────────────────────────────────────────
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

        # ── Action buttons ────────────────────────────────────
        btn_row = QHBoxLayout()
        for label, slot in [
            ("📊 Status",       self.show_status),
            ("🛑 Stop Selected",self.stop_selected),
            ("💀 Stop All",     self.stop_all),
        ]:
            b = QPushButton(label)
            b.clicked.connect(slot)
            btn_row.addWidget(b)
        top_layout.addLayout(btn_row)

        splitter.addWidget(top)

        # ── Console ───────────────────────────────────────────
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
        """Non-blocking subprocess via Worker thread."""
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
                item = QListWidgetItem(stripped)
                self.vm_list.addItem(item)
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
            # qemu-ctl stop reads a line from stdin — must include newline
            self._run(cmd, stdin_data=f"{row + 1}\n",
                      on_done=self._after_stop)

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
            ("➕  Create Image",   "create",  "#238636", "#2ea043"),
            ("ℹ   Image Info",    "info",    "#0078d4", "#0090ff"),
            ("📏  Resize Image",  "resize",  "#9e6a03", "#bb8009"),
            ("🔄  Convert Image", "convert", "#6e40c9", "#8250df"),
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

        disk = BIN_DIR / "qemu-disk"
        if not disk.exists():
            QMessageBox.critical(self, "Error",
                f"Binary not found:\n{disk}\n\nRun: ./build/make qemu-disk")
            return

        # Build stdin for the interactive binary.
        # Always use full BIN_DIR paths so qemu-disk creates/reads
        # files in bin/ regardless of the working directory.
        def bin_path(filename):
            """If user gave a bare name, prepend BIN_DIR."""
            p = Path(filename)
            if p.is_absolute() or p.parent != Path("."):
                return str(p)          # already has a directory component
            return str(BIN_DIR / p)

        if mode == "create":
            raw_name = dlg.get("name") or "default.qcow2"
            name = bin_path(raw_name)
            size = dlg.get("size") or "10G"
            stdin = f"{name}\n{size}\ny\n"
            self.console.clear_and_print(f"Creating {name} ({size})…")

        elif mode == "info":
            raw_name = dlg.get("name")
            if not raw_name:
                QMessageBox.warning(self, "Missing", "Image name is required.")
                return
            name = bin_path(raw_name)
            stdin = f"{name}\n"
            self.console.clear_and_print(f"Fetching info for {name}…")

        elif mode == "resize":
            raw_name = dlg.get("name")
            size = dlg.get("size")
            if not raw_name or not size:
                QMessageBox.warning(self, "Missing", "All fields are required.")
                return
            name = bin_path(raw_name)
            stdin = f"{name}\n{size}\ny\n"
            self.console.clear_and_print(f"Resizing {name} → {size}…")

        elif mode == "convert":
            raw_src = dlg.get("src")
            raw_dst = dlg.get("dst")
            fmt     = dlg.get("fmt")
            if not raw_src or not raw_dst or not fmt:
                QMessageBox.warning(self, "Missing", "All fields are required.")
                return
            src = bin_path(raw_src)
            dst = bin_path(raw_dst)
            stdin = f"{src}\n{dst}\n{fmt}\ny\n"
            self.console.clear_and_print(f"Converting {src} → {dst} ({fmt})…")

        w = Worker([str(disk), mode], stdin_data=stdin)
        self._workers.append(w)
        w.finished.connect(self._on_done)
        w.finished.connect(lambda: self._workers.remove(w) if w in self._workers else None)
        w.start()

    def _on_done(self, stdout, stderr):
        output = (stdout + stderr).strip()
        self.console.clear_and_print(output or "(no output)")


# =============================================================
#  MAIN WINDOW
# =============================================================

class VirtForge(QMainWindow):

    def __init__(self):
        super().__init__()
        self.setWindowTitle("Virt-Forge  —  QEMU Control Panel")
        self.resize(1100, 780)
        self.setMinimumSize(900, 640)
        self.setStyleSheet(self._theme())

        tabs = QTabWidget()
        tabs.setDocumentMode(True)
        tabs.addTab(VMTab(self),      "🖥  VM Launcher")
        tabs.addTab(ControlTab(self), "⚙  Control")
        tabs.addTab(DiskTab(self),    "💾  Disk Manager")

        self.setCentralWidget(tabs)

        # Status bar shows bin/ path
        self.statusBar().showMessage(f"bin: {BIN_DIR}")

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
        QSpinBox::up-button, QSpinBox::down-button {
            width: 24px;
        }
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
        QPushButton:hover  { background: #30363d; border-color: #58a6ff; }
        QPushButton:pressed{ background: #161b22; }
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
        """


# =============================================================
#  ENTRY POINT
# =============================================================

if __name__ == "__main__":
    app = QApplication(sys.argv)
    app.setApplicationName("Virt-Forge")

    # Enable HiDPI
    if hasattr(Qt.ApplicationAttribute, "AA_UseHighDpiPixmaps"):
        app.setAttribute(Qt.ApplicationAttribute.AA_UseHighDpiPixmaps, True)

    window = VirtForge()
    window.show()
    sys.exit(app.exec())
