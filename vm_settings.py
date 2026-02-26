import os
from PyQt5.QtWidgets import *
from PyQt5.QtCore import QTimer, Qt
from common import get_env, is_vm_running, BASE_DIR, qemu_system_cmd, QEMU_WRAPPER
import subprocess

class VMSettingsWindow(QWidget):
    def __init__(self):
        super().__init__()
        self.setWindowTitle("VM Runner")
        self.setGeometry(200, 200, 580, 560)
        self.iso_path = ""

        layout = QVBoxLayout()
        layout.addWidget(QLabel("⚙️ <b>VM Configuration</b>"))

        form = QFormLayout()
        self.inputs = {
            'arch':      QLineEdit("x86_64"),
            'ram':       QLineEdit("1024"),
            'smp':       QLineEdit("2"),
            'disk_name': QLineEdit("debian12.qcow2"),
            'format':    QLineEdit("qcow2"),
            'net':       QLineEdit("tcp::6666-:22"),
            'vnc':       QLineEdit("127.0.0.1:1"),
            'spice':     QLineEdit("5930"),
            'accel':     QLineEdit("tcg,tb-size=2048"),
        }

        labels = {
            'arch':      'Architecture',
            'ram':       'Memory (MB)',
            'smp':       'CPUs',
            'disk_name': 'Disk Name',
            'format':    'Disk Format',
            'net':       'Port Forwards',
            'vnc':       'VNC',
            'spice':     'SPICE Port',
            'accel':     'Accelerator',
        }

        for key, widget in self.inputs.items():
            form.addRow(labels[key] + ":", widget)
            widget.textChanged.connect(self.update_preview)

        # ISO
        self.iso_label = QLabel("No ISO Selected")
        self.iso_label.setWordWrap(True)
        self.btn_browse_iso = QPushButton("💿 Browse ISO")
        self.btn_browse_iso.clicked.connect(self.browse_iso)
        form.addRow("Installation ISO:", self.btn_browse_iso)
        form.addRow("Selected ISO:", self.iso_label)

        layout.addLayout(form)

        # Preview
        self.preview_text = QTextEdit()
        self.preview_text.setReadOnly(True)
        self.preview_text.setFixedHeight(120)
        self.preview_text.setStyleSheet(
            "background-color: #1e1e1e; color: #00ff00; font-family: monospace; font-size: 11px;"
        )
        layout.addWidget(QLabel("<b>Generated Command:</b>"))
        layout.addWidget(self.preview_text)

        # Start/Stop button
        self.btn_run = QPushButton()
        self.btn_run.setFixedHeight(50)
        self.btn_run.clicked.connect(self.toggle_vm)
        layout.addWidget(self.btn_run)

        # QEMU path info
        info = QLabel(f"🔧 QEMU wrapper: <code>{QEMU_WRAPPER}</code>")
        info.setWordWrap(True)
        info.setStyleSheet("color: gray; font-size: 10px;")
        layout.addWidget(info)

        self.ui_timer = QTimer()
        self.ui_timer.timeout.connect(self.update_button_style)
        self.ui_timer.start(1000)

        self.update_preview()
        self.update_button_style()
        self.setLayout(layout)

    def browse_iso(self):
        file_path, _ = QFileDialog.getOpenFileName(self, "Select ISO File", "", "ISO Files (*.iso)")
        if file_path:
            self.iso_path = file_path
            self.iso_label.setText(os.path.basename(file_path))
        else:
            self.iso_path = ""
            self.iso_label.setText("No ISO Selected")
        self.update_preview()

    def update_button_style(self):
        if is_vm_running():
            self.btn_run.setText("⏹  STOP VM")
            self.btn_run.setStyleSheet(
                "background-color: #f44336; color: white; font-weight: bold; font-size: 14px; border-radius: 5px;"
            )
        else:
            self.btn_run.setText("▶  START VM")
            self.btn_run.setStyleSheet(
                "background-color: #4CAF50; color: white; font-weight: bold; font-size: 14px; border-radius: 5px;"
            )

    def generate_cmd(self) -> list:
        i = {k: v.text().strip() for k, v in self.inputs.items()}
        disk_path = os.path.join(BASE_DIR, i['disk_name'])

        cmd = qemu_system_cmd(
            i['arch'],
            "-m",      i['ram'],
            "-smp",    i['smp'],
            "-accel",  i['accel'],
            "-drive",  f"file={disk_path},if=virtio,format={i['format']}",
            "-device", "virtio-net,netdev=n1",
            "-netdev", f"user,id=n1,hostfwd={i['net']}",
            "-vnc",    i['vnc'],
            "-spice",  f"port={i['spice']},addr=127.0.0.1,disable-ticketing=on",
            "-device", "virtio-serial-pci",
            "-device", "virtserialport,chardev=spicechannel0,name=com.redhat.spice.0",
            "-chardev","spicevmc,id=spicechannel0,name=vdagent",
            "-vga",    "qxl",
            "-display","none",
        )

        if self.iso_path:
            cmd.extend(["-cdrom", self.iso_path, "-boot", "d"])

        return cmd

    def update_preview(self):
        cmd = self.generate_cmd()
        # Pretty print: wrapper + subcommand on first line, rest with \
        pretty = " \\\n  ".join(cmd)
        self.preview_text.setText(pretty)

    def toggle_vm(self):
        env = get_env()
        if is_vm_running():
            subprocess.run(["pkill", "-f", "qemu-system"], env=env)
            QMessageBox.information(self, "Stopped", "VM stop signal sent.")
        else:
            try:
                cmd = self.generate_cmd()
                disk_file = os.path.join(BASE_DIR, self.inputs['disk_name'].text())
                if not os.path.exists(disk_file):
                    QMessageBox.warning(self, "Error", f"Disk not found:\n{disk_file}")
                    return
                subprocess.Popen(cmd, env=env, cwd=BASE_DIR)
                QMessageBox.information(self, "Started", "VM started successfully.")
            except Exception as e:
                QMessageBox.critical(self, "Error", str(e))
