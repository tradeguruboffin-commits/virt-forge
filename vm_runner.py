"""vm_runner.py — FINAL SAFE start / stop / kill individual VMs."""

import os
import signal
import subprocess
import threading
import time

from common import (
    get_env,
    qemu_system_cmd,
    pid_file,
    log_file,
    read_pid,
    is_pid_alive,
)
from vm_store import set_status, update_vm


# ─────────────────────────────────────────────────────────────
# Build QEMU Arguments
# ─────────────────────────────────────────────────────────────

def build_args(vm: dict) -> list:
    """Build QEMU args from VM config dict."""
    pf = pid_file(vm["id"])

    # Custom Mode
    if vm.get("use_custom") and vm.get("custom_args", "").strip():
        import shlex

        tokens = shlex.split(vm["custom_args"].replace("\\\n", " "))
        args = [vm.get("_wrapper", ""), f"system-{vm['arch']}"] + tokens

        if "-daemonize" not in args:
            args.append("-daemonize")

        if "-pidfile" not in args:
            args += ["-pidfile", pf]

        return args

    # Simple Mode
    args = qemu_system_cmd(
        vm["arch"],
        "-name", vm["name"],
        "-m", str(vm["ram"]),
        "-smp", str(vm["smp"]),
        "-accel", vm["accel"],
        "-drive", f"file={vm['disk_path']},if=virtio,format=qcow2",
        "-display", "none",
        "-vnc", f"127.0.0.1:{vm['vnc_display']}",
        "-spice", f"port={vm['spice_port']},addr=127.0.0.1,disable-ticketing=on",
        "-device", "virtio-net,netdev=n1",
        "-netdev", f"user,id=n1,hostfwd={vm['net_forwards']}",
        "-device", "virtio-serial-pci",
        "-device", "virtserialport,chardev=spicechannel0,name=com.redhat.spice.0",
        "-chardev", "spicevmc,id=spicechannel0,name=vdagent",
        "-vga", "qxl",
        "-daemonize",
        "-pidfile", pf,
    )

    return args


# ─────────────────────────────────────────────────────────────
# Start VM
# ─────────────────────────────────────────────────────────────

def start_vm(vm: dict, on_done=None):
    """Launch VM in background thread. on_done(success, message)."""

    def _run():
        try:
            args = build_args(vm)
            env = get_env()
            lf = log_file(vm["id"])
            pf = pid_file(vm["id"])

            set_status(vm["id"], "starting")

            # Clean old pidfile
            if os.path.exists(pf):
                try:
                    os.unlink(pf)
                except:
                    pass

            with open(lf, "w") as f:
                subprocess.Popen(
                    args,
                    env=env,
                    stdout=f,
                    stderr=f,
                )

            # Wait for PID file
            timeout = 15
            start_time = time.time()
            pid = None

            while time.time() - start_time < timeout:
                pid = read_pid(vm["id"])
                if pid and is_pid_alive(pid):
                    break
                time.sleep(0.3)

            if pid and is_pid_alive(pid):
                update_vm(vm["id"], {"status": "running"})
                if on_done:
                    on_done(True, f"Started  PID {pid}")
            else:
                set_status(vm["id"], "stopped")
                err = ""
                if os.path.exists(lf):
                    try:
                        err = open(lf).read()
                    except:
                        pass
                if on_done:
                    on_done(False, f"Failed to start\n{err}")

        except Exception as e:
            set_status(vm["id"], "stopped")
            if on_done:
                on_done(False, str(e))

    threading.Thread(target=_run, daemon=True).start()


# ─────────────────────────────────────────────────────────────
# Stop VM
# ─────────────────────────────────────────────────────────────

def stop_vm(vm_id, force=False):
    pid = read_pid(vm_id)

    if not pid or not is_pid_alive(pid):
        set_status(vm_id, "stopped")
        return "Already stopped"

    try:
        sig = signal.SIGKILL if force else signal.SIGTERM
        os.kill(pid, sig)

        # Wait for process to exit
        timeout = 10
        start_time = time.time()

        while time.time() - start_time < timeout:
            if not is_pid_alive(pid):
                break
            time.sleep(0.3)

        # Force kill fallback
        if is_pid_alive(pid):
            os.kill(pid, signal.SIGKILL)

        set_status(vm_id, "stopped")

        pf = pid_file(vm_id)
        if os.path.exists(pf):
            try:
                os.unlink(pf)
            except:
                pass

        return f"{'Killed' if force else 'Stopped'} PID {pid}"

    except ProcessLookupError:
        set_status(vm_id, "stopped")
        return "Process not found"
    except Exception as e:
        return str(e)


# ─────────────────────────────────────────────────────────────
# Live Status
# ─────────────────────────────────────────────────────────────

def get_live_status(vm_id) -> str:
    pid = read_pid(vm_id)
    return "running" if pid and is_pid_alive(pid) else "stopped"


# ─────────────────────────────────────────────────────────────
# Get Log
# ─────────────────────────────────────────────────────────────

def get_log(vm_id) -> str:
    lf = log_file(vm_id)
    if os.path.exists(lf):
        try:
            return open(lf).read()
        except:
            pass
    return "(no log yet)"
