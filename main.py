"""main.py — QEMU Manager with multi-VM support. (FULL SAFE VERSION)"""

import sys
import os
import signal
import subprocess

from PyQt5.QtWidgets import *
from PyQt5.QtCore import QTimer, Qt
from PyQt5.QtGui import QFont

from common import get_env, QEMU_WRAPPER, BASE_DIR
from vm_store import all_vms, create_vm, update_vm, delete_vm, set_status
from vm_runner import start_vm, stop_vm, get_live_status, get_log
from vm_card import VMCard
from vm_dialog import VMDialog
from disk_manager import DiskManagerWidget
from vm_tray import VMTray


# ─────────────────────────────────────────────────────────────
# Kill old instances
# ─────────────────────────────────────────────────────────────

def cleanup_old_instances():
    current_pid = os.getpid()
    try:
        pids = subprocess.check_output(
            ["pgrep", "-f", "main.py"]
        ).decode().split()
        for pid in pids:
            if int(pid) != current_pid:
                os.kill(int(pid), signal.SIGTERM)
    except:
        pass


cleanup_old_instances()


# ─────────────────────────────────────────────────────────────
# Log Dialog
# ─────────────────────────────────────────────────────────────

class LogDialog(QDialog):
    def __init__(self, parent, vm_name, vm_id):
        super().__init__(parent)
        self.vm_id = vm_id
        self.setWindowTitle(f"Log — {vm_name}")
        self.resize(640, 400)

        vl = QVBoxLayout(self)
        self.text = QTextEdit(readOnly=True)
        self.text.setFont(QFont("Monospace", 9))
        self.text.setStyleSheet("background:#1e1e1e; color:#00ff00;")
        vl.addWidget(self.text)

        btn_row = QHBoxLayout()
        ref = QPushButton("⟳ Refresh")
        ref.clicked.connect(self._load)
        cls = QPushButton("Close")
        cls.clicked.connect(self.close)
        btn_row.addWidget(ref)
        btn_row.addStretch()
        btn_row.addWidget(cls)
        vl.addLayout(btn_row)

        self._load()

    def _load(self):
        self.text.setPlainText(get_log(self.vm_id))


# ─────────────────────────────────────────────────────────────
# Main Window
# ─────────────────────────────────────────────────────────────

class MainWindow(QMainWindow):
    def __init__(self, tray):
        super().__init__()
        self.tray = tray
        self._cards = {}

        self.setWindowTitle("QEMU Manager — Multi-VM")
        self.setMinimumSize(660, 700)

        tabs = QTabWidget()
        tabs.addTab(self._build_vm_tab(), "🖥 Virtual Machines")
        tabs.addTab(DiskManagerWidget(), "💾 Disks & Snapshots")
        self.setCentralWidget(tabs)

        qemu_ok = os.path.exists(QEMU_WRAPPER)
        self.statusBar().showMessage(
            f"{'✅' if qemu_ok else '❌'} QEMU: {QEMU_WRAPPER}"
        )

        self._timer = QTimer()
        self._timer.timeout.connect(self._poll)
        self._timer.start(2500)

    # ─────────────────────────────────────────────────────────

    def shutdown(self):
        try:
            self._timer.stop()
        except:
            pass

        for vm_id, vm in all_vms().items():
            if vm.get("status") == "running":
                try:
                    stop_vm(vm_id, force=True)
                except:
                    pass

    # ─────────────────────────────────────────────────────────
    # VM TAB
    # ─────────────────────────────────────────────────────────

    def _build_vm_tab(self):
        w = QWidget()
        vl = QVBoxLayout(w)
        vl.setContentsMargins(8, 8, 8, 8)

        tb = QHBoxLayout()

        btn_new = QPushButton("➕ New VM")
        btn_new.setFixedHeight(34)
        btn_new.setStyleSheet(
            "background:#2196F3; color:white; font-weight:bold; border-radius:5px;"
        )
        btn_new.clicked.connect(self._new_vm)
        tb.addWidget(btn_new)
        tb.addStretch()

        self.lbl_summary = QLabel()
        self.lbl_summary.setStyleSheet("color:gray; font-size:11px;")
        tb.addWidget(self.lbl_summary)

        vl.addLayout(tb)

        scroll = QScrollArea()
        scroll.setWidgetResizable(True)
        scroll.setHorizontalScrollBarPolicy(Qt.ScrollBarAlwaysOff)

        self._vm_list_widget = QWidget()
        self._vm_list_layout = QVBoxLayout(self._vm_list_widget)
        self._vm_list_layout.setAlignment(Qt.AlignTop)
        self._vm_list_layout.setSpacing(4)

        scroll.setWidget(self._vm_list_widget)
        vl.addWidget(scroll)

        self._reload_cards()
        return w

    # ─────────────────────────────────────────────────────────
    # Cards
    # ─────────────────────────────────────────────────────────

    def _reload_cards(self):
        for card in self._cards.values():
            self._vm_list_layout.removeWidget(card)
            card.deleteLater()

        self._cards.clear()
        vms = all_vms()

        if not vms:
            lbl = QLabel("No VMs yet. Click ➕ New VM to get started.")
            lbl.setAlignment(Qt.AlignCenter)
            lbl.setStyleSheet("color:gray; font-size:13px; margin:40px;")
            self._vm_list_layout.addWidget(lbl)
            self._cards["__empty__"] = lbl
        else:
            for vm in vms.values():
                self._add_card(vm)

        self._update_summary()

    def _add_card(self, vm):
        card = VMCard(vm)
        card.sig_start.connect(self._on_start)
        card.sig_stop.connect(lambda vid: self._on_stop(vid, False))
        card.sig_kill.connect(lambda vid: self._on_stop(vid, True))
        card.sig_edit.connect(self._on_edit)
        card.sig_delete.connect(self._on_delete)
        card.sig_log.connect(self._on_log)

        self._vm_list_layout.addWidget(card)
        self._cards[vm["id"]] = card

    def _refresh_card(self, vm_id):
        vm = all_vms().get(vm_id)
        card = self._cards.get(vm_id)
        if vm and card:
            card.refresh(vm)

    def _update_summary(self):
        vms = all_vms()
        total = len(vms)
        running = sum(1 for v in vms.values()
                      if v.get("status") == "running")
        self.lbl_summary.setText(f"{running} running / {total} total")

    # ─────────────────────────────────────────────────────────
    # ACTIONS
    # ─────────────────────────────────────────────────────────

    def _new_vm(self):
        from vm_store import DEFAULTS

        tmp = {**DEFAULTS, "id": "__new__", "name": "New VM"}
        dlg = VMDialog(self, tmp, is_new=True)

        if dlg.exec_() == QDialog.Accepted:
            result = dlg.get_result()
            vm_id, vm = create_vm(result["name"])
            fields = {k: v for k, v in result.items() if k != "id"}

            try:
                update_vm(vm_id, fields)
            except ValueError as e:
                QMessageBox.warning(self, "Port Conflict", str(e))
                delete_vm(vm_id)
                return

            empty = self._cards.pop("__empty__", None)
            if empty:
                self._vm_list_layout.removeWidget(empty)
                empty.deleteLater()

            self._add_card(all_vms()[vm_id])
            self._update_summary()

    def _on_edit(self, vm_id):
        vm = all_vms().get(vm_id)
        if not vm:
            return

        if vm.get("status") == "running":
            QMessageBox.warning(
                self, "Running",
                "Stop the VM before editing."
            )
            return

        dlg = VMDialog(self, vm)
        if dlg.exec_() == QDialog.Accepted:
            try:
                update_vm(vm_id, dlg.get_result())
            except ValueError as e:
                QMessageBox.warning(self, "Port Conflict", str(e))
                return
            self._refresh_card(vm_id)

    def _on_start(self, vm_id):
        vm = all_vms().get(vm_id)
        if not vm:
            return

        if not vm.get("use_custom") and not os.path.exists(vm.get("disk_path", "")):
            QMessageBox.warning(
                self, "Error",
                f"Disk not found:\n{vm.get('disk_path')}"
            )
            return

        vm["_wrapper"] = QEMU_WRAPPER

        def _done(ok, msg):
            if ok:
                self.statusBar().showMessage(
                    f"✅ {vm['name']}: {msg}"
                )
            else:
                self.statusBar().showMessage(
                    f"❌ {vm['name']}: failed"
                )
                QMessageBox.critical(
                    self,
                    f"QEMU Error — {vm['name']}",
                    msg
                )

            self._refresh_card(vm_id)
            self._update_summary()

        set_status(vm_id, "starting")
        self._refresh_card(vm_id)
        start_vm(vm, on_done=_done)

    def _on_stop(self, vm_id, force=False):
        vm = all_vms().get(vm_id)
        if not vm:
            return

        msg = stop_vm(vm_id, force)

        self.statusBar().showMessage(
            f"{'⚡' if force else '⏹'} {vm['name']}: {msg}"
        )

        self._refresh_card(vm_id)
        self._update_summary()

    def _on_delete(self, vm_id):
        vm = all_vms().get(vm_id)
        if not vm:
            return

        if vm.get("status") == "running":
            QMessageBox.warning(
                self, "Running",
                "Stop the VM before deleting."
            )
            return

        if QMessageBox.question(
            self,
            "Confirm Delete",
            f"Delete VM '{vm['name']}'?\n(Disk file is NOT deleted)",
            QMessageBox.Yes | QMessageBox.No,
        ) == QMessageBox.Yes:

            delete_vm(vm_id)

            card = self._cards.pop(vm_id, None)
            if card:
                self._vm_list_layout.removeWidget(card)
                card.deleteLater()

            if not all_vms():
                self._reload_cards()

            self._update_summary()

    def _on_log(self, vm_id):
        vm = all_vms().get(vm_id)
        if vm:
            LogDialog(self, vm["name"], vm_id).show()

    # ─────────────────────────────────────────────────────────
    # POLL
    # ─────────────────────────────────────────────────────────

    def _poll(self):
        changed = False

        for vm_id, vm in all_vms().items():
            live = get_live_status(vm_id)

            if live != vm.get("status") and vm.get("status") != "starting":
                set_status(vm_id, live)
                self._refresh_card(vm_id)
                changed = True

        if changed:
            self._update_summary()
            self.tray.update_icon()

    # ─────────────────────────────────────────────────────────
    # CLOSE
    # ─────────────────────────────────────────────────────────

    def closeEvent(self, event):
        running = [
            v for v in all_vms().values()
            if v.get("status") == "running"
        ]

        if running:
            self.hide()
            event.ignore()
        else:
            self.shutdown()
            self.tray.hide()
            QApplication.instance().quit()
            event.accept()


# ─────────────────────────────────────────────────────────────
# MAIN
# ─────────────────────────────────────────────────────────────

if __name__ == "__main__":
    app = QApplication(sys.argv)
    app.setQuitOnLastWindowClosed(False)

    tray = VMTray()
    w = MainWindow(tray)
    tray.parent_window = w

    app.aboutToQuit.connect(w.shutdown)

    w.show()
    sys.exit(app.exec_())
