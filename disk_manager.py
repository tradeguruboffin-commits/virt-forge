"""disk_manager.py — Disk & Snapshot management tab."""
import os, subprocess
from PyQt5.QtWidgets import *
from PyQt5.QtCore import Qt
from common import get_env, qemu_img_cmd, STORAGE_DIR, BASE_DIR


class DiskManagerWidget(QWidget):
    def __init__(self, parent=None):
        super().__init__(parent)
        layout = QVBoxLayout(self)
        layout.setContentsMargins(8, 8, 8, 8)

        layout.addWidget(QLabel("<b>Virtual Disks</b>  (double-click for info):"))
        self.disk_list = QListWidget()
        self.disk_list.itemDoubleClicked.connect(self._disk_info)
        self.disk_list.itemClicked.connect(self._load_snaps)
        layout.addWidget(self.disk_list)

        d_btns = QHBoxLayout()
        for label in ["Create", "Delete", "Resize", "Convert", "Refresh"]:
            b = QPushButton(label)
            b.clicked.connect(self._disk_op)
            d_btns.addWidget(b)
        layout.addLayout(d_btns)

        layout.addWidget(QLabel("<b>Snapshots:</b>"))
        self.snap_list = QListWidget()
        layout.addWidget(self.snap_list)

        s_btns = QHBoxLayout()
        for label in ["Create Snap", "Remove Snap", "Revert"]:
            b = QPushButton(label)
            b.clicked.connect(self._snap_op)
            s_btns.addWidget(b)
        layout.addLayout(s_btns)

        self._refresh()

    # ── Disk ─────────────────────────────────────────────────────────────────

    def _refresh(self):
        self.disk_list.clear()
        self.snap_list.clear()
        exts = (".qcow2", ".raw", ".img", ".vmdk")
        for d in sorted(set([STORAGE_DIR, BASE_DIR])):
            if os.path.isdir(d):
                for f in sorted(os.listdir(d)):
                    if f.endswith(exts):
                        self.disk_list.addItem(os.path.join(d, f))

    def _disk_info(self, item):
        cmd = qemu_img_cmd("info", item.text())
        try:
            out = subprocess.check_output(cmd, env=get_env(), stderr=subprocess.STDOUT).decode()
            QMessageBox.information(self, f"Info: {os.path.basename(item.text())}", out)
        except subprocess.CalledProcessError as e:
            QMessageBox.critical(self, "Error", e.output.decode())

    def _disk_op(self):
        action = self.sender().text()
        env    = get_env()

        if action == "Refresh":
            self._refresh(); return

        sel = self.disk_list.currentItem()
        if action != "Create" and not sel:
            QMessageBox.warning(self, "Warning", "Select a disk first!"); return

        if action == "Create":
            name, ok1 = QInputDialog.getText(self, "New Disk", "Filename (no extension):")
            fmt, ok2  = QInputDialog.getItem(self, "Format", "Format:",
                                              ["qcow2","raw","vmdk"], 0, False)
            size, ok3 = QInputDialog.getText(self, "Size", "Size (e.g. 20G):")
            if ok1 and ok2 and ok3:
                out = os.path.join(STORAGE_DIR, name + "." + fmt)
                cmd = qemu_img_cmd("create", "-f", fmt, out, size)
                res = subprocess.run(cmd, env=env, capture_output=True, text=True)
                if res.returncode == 0:
                    QMessageBox.information(self, "Done", f"Created:\n{out}")
                else:
                    QMessageBox.critical(self, "Error", res.stderr or res.stdout)
                self._refresh()

        elif action == "Delete":
            path = sel.text()
            if QMessageBox.question(self, "Confirm", f"Delete {os.path.basename(path)}?",
                                    QMessageBox.Yes | QMessageBox.No) == QMessageBox.Yes:
                try:
                    os.remove(path); self._refresh(); self.snap_list.clear()
                except Exception as e:
                    QMessageBox.critical(self, "Error", str(e))

        elif action == "Resize":
            path = sel.text()
            size, ok = QInputDialog.getText(self, "Resize", "New size or delta (e.g. +5G / 30G):")
            if ok and size:
                cmd = qemu_img_cmd("resize", path, size)
                res = subprocess.run(cmd, env=env, capture_output=True, text=True)
                if res.returncode != 0:
                    QMessageBox.critical(self, "Error", res.stderr or res.stdout)
                else:
                    QMessageBox.information(self, "Done", "Resize successful")

        elif action == "Convert":
            path = sel.text()
            fmt, ok1  = QInputDialog.getItem(self, "Convert", "Output format:",
                                              ["qcow2","raw","vmdk"], 0, False)
            name, ok2 = QInputDialog.getText(self, "Output", "Output filename (with extension):")
            if ok1 and ok2:
                out = os.path.join(STORAGE_DIR, name)
                cmd = qemu_img_cmd("convert", "-O", fmt, path, out)
                subprocess.run(cmd, env=env)
                self._refresh()

    # ── Snapshot ──────────────────────────────────────────────────────────────

    def _load_snaps(self, item):
        self.snap_list.clear()
        try:
            cmd = qemu_img_cmd("snapshot", "-l", item.text())
            out = subprocess.check_output(cmd, env=get_env(),
                                          stderr=subprocess.STDOUT).decode()
            lines = out.splitlines()
            if len(lines) > 2:
                self.snap_list.addItems(lines[2:])
        except:
            pass

    def _snap_op(self):
        sel_disk = self.disk_list.currentItem()
        if not sel_disk: return
        action = self.sender().text()
        env    = get_env()
        path   = sel_disk.text()

        if action == "Create Snap":
            sname, ok = QInputDialog.getText(self, "Snapshot", "Snapshot name:")
            if ok and sname:
                cmd = qemu_img_cmd("snapshot", "-c", sname, path)
                subprocess.run(cmd, env=env)
                self._load_snaps(sel_disk)

        elif action == "Remove Snap":
            sel_snap = self.snap_list.currentItem()
            if not sel_snap: return
            snap_id = sel_snap.text().split()[0]
            if QMessageBox.question(self, "Confirm", "Delete snapshot?",
                                    QMessageBox.Yes | QMessageBox.No) == QMessageBox.Yes:
                cmd = qemu_img_cmd("snapshot", "-d", snap_id, path)
                subprocess.run(cmd, env=env)
                self._load_snaps(sel_disk)

        elif action == "Revert":
            sel_snap = self.snap_list.currentItem()
            if not sel_snap: return
            snap_id = sel_snap.text().split()[0]
            if QMessageBox.question(self, "Confirm", "Revert to snapshot?",
                                    QMessageBox.Yes | QMessageBox.No) == QMessageBox.Yes:
                cmd = qemu_img_cmd("snapshot", "-a", snap_id, path)
                subprocess.run(cmd, env=env)
