# vm_tray.py — FINAL SAFE VERSION

from PyQt5.QtWidgets import QSystemTrayIcon, QMenu, QMessageBox, QApplication
from PyQt5.QtGui import QIcon
from vm_store import all_vms
from vm_runner import stop_vm


class VMTray(QSystemTrayIcon):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.parent_window = parent
        self.setToolTip("QEMU Manager")

        menu = QMenu()
        menu.addAction("Show Manager").triggered.connect(self.restore_window)
        menu.addSeparator()
        menu.addAction("Quit").triggered.connect(self.exit_application)
        self.setContextMenu(menu)

        self.update_icon()
        self.activated.connect(
            lambda r: self.restore_window()
            if r == QSystemTrayIcon.Trigger else None
        )

    def restore_window(self):
        if self.parent_window:
            self.parent_window.showNormal()
            self.parent_window.activateWindow()

    def update_icon(self):
        running = any(v.get("status") == "running" for v in all_vms().values())
        self.setIcon(
            QIcon.fromTheme("system-run" if running else "system-shutdown")
        )
        if not self.isVisible():
            self.show()

    def exit_application(self):
        running = [v for v in all_vms().values() if v.get("status") == "running"]

        if running:
            names = ", ".join(v["name"] for v in running)
            reply = QMessageBox.question(
                None,
                "Confirm Exit",
                f"VMs still running: {names}\nForce quit?",
                QMessageBox.Yes | QMessageBox.No
            )
            if reply == QMessageBox.No:
                return

            for v in running:
                stop_vm(v["id"], force=True)

        if self.parent_window:
            self.parent_window.shutdown()

        self.hide()
        QApplication.instance().quit()
