"""vm_dialog.py — Create / Edit VM dialog (Simple + Custom Command tabs)."""
import os, shlex
from PyQt5.QtWidgets import *
from PyQt5.QtCore import Qt
from common import STORAGE_DIR, QEMU_WRAPPER, get_env, qemu_img_cmd
from vm_store import update_vm
from vm_runner import build_args
import subprocess


class VMDialog(QDialog):
    def __init__(self, parent, vm: dict, is_new=False):
        super().__init__(parent)
        self.vm      = dict(vm)
        self.is_new  = is_new
        self.setWindowTitle("New VM" if is_new else f"Edit — {vm['name']}")
        self.setMinimumWidth(560)

        layout = QVBoxLayout(self)
        self.tabs = QTabWidget()
        layout.addWidget(self.tabs)

        self.tabs.addTab(self._build_simple(), "  Simple  ")
        self.tabs.addTab(self._build_custom(), "  Custom Command  ")

        if vm.get("use_custom"):
            self.tabs.setCurrentIndex(1)

        self.tabs.currentChanged.connect(self._on_tab_change)

        btns = QDialogButtonBox(QDialogButtonBox.Ok | QDialogButtonBox.Cancel)
        btns.accepted.connect(self.accept)
        btns.rejected.connect(self.reject)
        layout.addWidget(btns)

    # ── Simple tab ────────────────────────────────────────────────────────────

    def _build_simple(self):
        w    = QWidget()
        form = QFormLayout(w)
        form.setContentsMargins(12, 12, 12, 12)
        form.setSpacing(8)

        vm = self.vm

        self.f_name  = QLineEdit(vm.get("name", ""))
        self.f_arch  = QComboBox()
        self.f_arch.addItems(["x86_64", "aarch64", "riscv64"])
        self.f_arch.setCurrentText(vm.get("arch", "x86_64"))
        self.f_ram   = QLineEdit(str(vm.get("ram",  "1024")))
        self.f_smp   = QLineEdit(str(vm.get("smp",  "2")))
        self.f_accel = QLineEdit(vm.get("accel", "tcg,tb-size=2048"))

        # Disk row
        self.f_disk  = QLineEdit(vm.get("disk_path", ""))
        disk_row = QWidget()
        dl = QHBoxLayout(disk_row); dl.setContentsMargins(0,0,0,0)
        dl.addWidget(self.f_disk)
        btn_browse = QPushButton("Browse…")
        btn_browse.clicked.connect(self._browse_disk)
        btn_create = QPushButton("Create New…")
        btn_create.clicked.connect(self._create_disk)
        dl.addWidget(btn_browse)
        dl.addWidget(btn_create)

        self.f_net   = QLineEdit(vm.get("net_forwards", "tcp::6666-:22"))
        self.f_vnc   = QSpinBox(); self.f_vnc.setRange(1, 99)
        self.f_vnc.setValue(int(vm.get("vnc_display", 1)))
        self.f_spice = QSpinBox(); self.f_spice.setRange(1024, 65535)
        self.f_spice.setValue(int(vm.get("spice_port", 5930)))

        form.addRow("Name:",         self.f_name)
        form.addRow("Architecture:", self.f_arch)
        form.addRow("Memory (MB):",  self.f_ram)
        form.addRow("CPUs:",         self.f_smp)
        form.addRow("Accelerator:",  self.f_accel)
        form.addRow("Disk Image:",   disk_row)
        form.addRow("Port Forwards:", self.f_net)
        form.addRow(self._sep(), QLabel(""))
        form.addRow("VNC Display #:", self.f_vnc)
        form.addRow("SPICE Port:",    self.f_spice)

        # Connection hint
        self.hint_lbl = QLabel()
        self.hint_lbl.setStyleSheet("color: gray; font-size: 10px;")
        self.hint_lbl.setWordWrap(True)
        form.addRow("", self.hint_lbl)
        self._update_hints()
        self.f_vnc.valueChanged.connect(self._update_hints)
        self.f_spice.valueChanged.connect(self._update_hints)

        return w

    def _update_hints(self):
        vnc   = self.f_vnc.value()
        spice = self.f_spice.value()
        self.hint_lbl.setText(
            f"VNC  → vncviewer 127.0.0.1:{vnc}   (port {5900+vnc})\n"
            f"SPICE→ spicy --host=127.0.0.1 --port={spice}"
        )

    # ── Custom Command tab ────────────────────────────────────────────────────

    def _build_custom(self):
        w   = QWidget()
        vl  = QVBoxLayout(w)
        vl.setContentsMargins(12, 12, 12, 12)

        info = QLabel(
            "<b>Custom Command Mode</b><br>"
            "<small>Write the full QEMU argument list below (everything <i>after</i> the wrapper binary).<br>"
            "Simple tab settings are <b>ignored</b> when this tab is active.<br>"
            "<tt>-daemonize</tt> and <tt>-pidfile</tt> are auto-added if missing.</small>"
        )
        info.setWordWrap(True)
        vl.addWidget(info)
        vl.addWidget(self._sep())

        # Name still needed
        name_row = QHBoxLayout()
        name_row.addWidget(QLabel("VM Name:"))
        self.f_adv_name = QLineEdit(self.vm.get("name", ""))
        name_row.addWidget(self.f_adv_name)
        vl.addLayout(name_row)

        arch_row = QHBoxLayout()
        arch_row.addWidget(QLabel("Arch:"))
        self.f_adv_arch = QComboBox()
        self.f_adv_arch.addItems(["x86_64", "aarch64", "riscv64"])
        self.f_adv_arch.setCurrentText(self.vm.get("arch", "x86_64"))
        arch_row.addWidget(self.f_adv_arch)
        vl.addLayout(arch_row)

        vl.addWidget(QLabel("<b>QEMU Arguments</b> <small>(everything after the binary)</small>"))

        self.custom_edit = QTextEdit()
        self.custom_edit.setFont(__import__('PyQt5.QtGui', fromlist=['QFont']).QFont("Monospace", 9))
        self.custom_edit.setStyleSheet("background:#1e1e1e; color:#00ff00;")
        self.custom_edit.setMinimumHeight(200)
        self.custom_edit.setPlainText(self.vm.get("custom_args", ""))
        vl.addWidget(self.custom_edit)

        btn_row = QHBoxLayout()
        gen_btn = QPushButton("⟳  Generate from Simple tab")
        gen_btn.clicked.connect(self._gen_from_simple)
        clr_btn = QPushButton("✕  Clear")
        clr_btn.clicked.connect(lambda: self.custom_edit.setPlainText(""))
        btn_row.addWidget(gen_btn)
        btn_row.addStretch()
        btn_row.addWidget(clr_btn)
        vl.addLayout(btn_row)

        note = QLabel(
            "<small style='color:gray'>Tip: include <tt>-daemonize -pidfile /path/file.pid</tt> "
            "for proper tracking. virt-gui auto-adds them if missing.</small>"
        )
        note.setWordWrap(True)
        vl.addWidget(note)
        return w

    def _gen_from_simple(self):
        try:
            tmp = self._read_simple()
            tmp["_wrapper"] = QEMU_WRAPPER
            args = build_args(tmp)
            # skip wrapper + system-arch
            self.custom_edit.setPlainText(" \\\n".join(args[2:]))
        except Exception as e:
            self.custom_edit.setPlainText(f"# Error: {e}")

    def _on_tab_change(self, idx):
        if idx == 1 and not self.custom_edit.toPlainText().strip():
            self._gen_from_simple()

    # ── Disk helpers ──────────────────────────────────────────────────────────

    def _browse_disk(self):
        path, _ = QFileDialog.getOpenFileName(
            self, "Select Disk Image", STORAGE_DIR,
            "Disk Images (*.qcow2 *.img *.iso *.raw *.vmdk)"
        )
        if path:
            self.f_disk.setText(path)

    def _create_disk(self):
        dlg = QDialog(self)
        dlg.setWindowTitle("Create New Disk")
        dlg.setMinimumWidth(340)
        form = QFormLayout(dlg)
        f_name = QLineEdit(self.f_name.text() or "disk")
        f_size = QLineEdit("20G")
        f_dir  = QLineEdit(STORAGE_DIR)
        btn_dir = QPushButton("…")
        dir_row = QWidget(); dl = QHBoxLayout(dir_row); dl.setContentsMargins(0,0,0,0)
        dl.addWidget(f_dir); dl.addWidget(btn_dir)
        btn_dir.clicked.connect(lambda: (
            lambda p: f_dir.setText(p) if p else None
        )(QFileDialog.getExistingDirectory(dlg, "Choose folder", STORAGE_DIR)))
        form.addRow("Filename:", f_name)
        form.addRow("Size:",     f_size)
        form.addRow("Save to:",  dir_row)
        btns = QDialogButtonBox(QDialogButtonBox.Ok | QDialogButtonBox.Cancel)
        btns.accepted.connect(dlg.accept); btns.rejected.connect(dlg.reject)
        form.addRow(btns)
        if dlg.exec_() == QDialog.Accepted:
            out  = os.path.join(f_dir.text(), f_name.text() + ".qcow2")
            cmd  = qemu_img_cmd("create", "-f", "qcow2", out, f_size.text())
            res  = subprocess.run(cmd, env=get_env(), capture_output=True, text=True)
            if res.returncode == 0:
                self.f_disk.setText(out)
                QMessageBox.information(self, "Done", f"Created:\n{out}")
            else:
                QMessageBox.critical(self, "Error", res.stderr or res.stdout)

    # ── Result ────────────────────────────────────────────────────────────────

    def _read_simple(self) -> dict:
        return {
            **self.vm,
            "name":         self.f_name.text().strip() or "unnamed",
            "arch":         self.f_arch.currentText(),
            "ram":          self.f_ram.text().strip(),
            "smp":          self.f_smp.text().strip(),
            "accel":        self.f_accel.text().strip(),
            "disk_path":    self.f_disk.text().strip(),
            "net_forwards": self.f_net.text().strip(),
            "vnc_display":  self.f_vnc.value(),
            "spice_port":   self.f_spice.value(),
            "use_custom":   False,
            "custom_args":  "",
        }

    def get_result(self) -> dict:
        if self.tabs.currentIndex() == 1:
            return {
                **self.vm,
                "name":       self.f_adv_name.text().strip() or "unnamed",
                "arch":       self.f_adv_arch.currentText(),
                "use_custom": True,
                "custom_args":self.custom_edit.toPlainText(),
            }
        return self._read_simple()

    def _sep(self):
        s = QFrame(); s.setFrameShape(QFrame.HLine)
        s.setStyleSheet("color: #aaa"); return s
