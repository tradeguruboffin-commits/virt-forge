"""vm_card.py — one row/card per VM in the main list."""
from PyQt5.QtWidgets import *
from PyQt5.QtCore import Qt, pyqtSignal
from PyQt5.QtGui import QFont


STATUS_STYLE = {
    "running":  ("🟢 Running",  "#4CAF50"),
    "stopped":  ("🔴 Stopped",  "#f44336"),
    "starting": ("🟡 Starting…","#FF9800"),
}


class VMCard(QFrame):
    sig_start  = pyqtSignal(str)
    sig_stop   = pyqtSignal(str)
    sig_kill   = pyqtSignal(str)
    sig_edit   = pyqtSignal(str)
    sig_delete = pyqtSignal(str)
    sig_log    = pyqtSignal(str)

    def __init__(self, vm: dict, parent=None):
        super().__init__(parent)
        self.vm_id = vm["id"]
        self.setFrameShape(QFrame.StyledPanel)
        self.setStyleSheet("QFrame { border:1px solid #555; border-radius:6px; margin:3px; padding:4px; }")

        root = QHBoxLayout(self)
        root.setContentsMargins(8, 6, 8, 6)

        # ── Left: info ───────────────────────────────────────────────────
        info = QVBoxLayout()
        self.lbl_name   = QLabel()
        self.lbl_name.setFont(QFont("", 11, QFont.Bold))
        self.lbl_status = QLabel()
        self.lbl_ports  = QLabel()
        self.lbl_ports.setStyleSheet("color: gray; font-size: 10px;")
        info.addWidget(self.lbl_name)
        info.addWidget(self.lbl_status)
        info.addWidget(self.lbl_ports)
        root.addLayout(info, stretch=1)

        # ── Right: buttons ───────────────────────────────────────────────
        btns = QVBoxLayout()
        btns.setSpacing(3)

        row1 = QHBoxLayout(); row1.setSpacing(4)
        self.btn_start = self._btn("▶ Start",  "#4CAF50", lambda: self.sig_start.emit(self.vm_id))
        self.btn_stop  = self._btn("⏹ Stop",   "#FF9800", lambda: self.sig_stop.emit(self.vm_id))
        self.btn_kill  = self._btn("☠ Kill",   "#f44336", lambda: self.sig_kill.emit(self.vm_id))
        row1.addWidget(self.btn_start)
        row1.addWidget(self.btn_stop)
        row1.addWidget(self.btn_kill)
        btns.addLayout(row1)

        row2 = QHBoxLayout(); row2.setSpacing(4)
        row2.addWidget(self._btn("✏ Edit",   "#2196F3", lambda: self.sig_edit.emit(self.vm_id)))
        row2.addWidget(self._btn("📋 Log",   "#607D8B", lambda: self.sig_log.emit(self.vm_id)))
        row2.addWidget(self._btn("🗑 Delete","#9E9E9E", lambda: self.sig_delete.emit(self.vm_id)))
        btns.addLayout(row2)

        root.addLayout(btns)
        self.refresh(vm)

    def refresh(self, vm: dict):
        self.vm_id = vm["id"]
        status     = vm.get("status", "stopped")
        text, color = STATUS_STYLE.get(status, ("⚪ Unknown", "gray"))

        self.lbl_name.setText(vm.get("name", "—"))
        self.lbl_status.setText(f"<span style='color:{color}'>{text}</span>")

        if vm.get("use_custom"):
            self.lbl_ports.setText("⚙ Custom command mode")
        else:
            vnc   = vm.get("vnc_display", "?")
            spice = vm.get("spice_port",  "?")
            arch  = vm.get("arch", "x86_64")
            ram   = vm.get("ram", "?")
            self.lbl_ports.setText(
                f"{arch} | RAM {ram}MB | "
                f"VNC :{vnc} | SPICE {spice}"
            )

        running = (status == "running")
        self.btn_start.setEnabled(not running)
        self.btn_stop.setEnabled(running)
        self.btn_kill.setEnabled(running)

    def _btn(self, label, color, slot):
        b = QPushButton(label)
        b.setFixedHeight(26)
        b.setStyleSheet(
            f"QPushButton {{ background:{color}; color:white; border-radius:4px; font-size:10px; }}"
            f"QPushButton:disabled {{ background:#555; color:#999; }}"
        )
        b.clicked.connect(slot)
        return b
