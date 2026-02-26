"""common.py — portable paths & environment (PyInstaller safe)."""
import os
import sys
import subprocess

# ─────────────────────────────────────────────────────────────
# Base directory detection (works in normal + PyInstaller mode)
# ─────────────────────────────────────────────────────────────
if getattr(sys, "frozen", False):
    # Running as PyInstaller bundle
    BASE_DIR = os.path.dirname(sys.executable)
    MEIPASS  = getattr(sys, "_MEIPASS", BASE_DIR)
else:
    BASE_DIR = os.path.dirname(os.path.abspath(__file__))
    MEIPASS  = BASE_DIR

# ─────────────────────────────────────────────────────────────
# Portable QEMU paths
# ─────────────────────────────────────────────────────────────
QEMU_DIR     = os.path.join(BASE_DIR, "qemu_portable")
QEMU_BIN_DIR = os.path.join(QEMU_DIR, "bin")
QEMU_LIB_DIR = os.path.join(QEMU_DIR, "lib")

QEMU_WRAPPER = os.path.join(QEMU_BIN_DIR, "qemu")
QEMU_IMG     = os.path.join(QEMU_BIN_DIR, "qemu-img")

PID_DIR      = os.path.join(BASE_DIR, "pids")
LOG_DIR      = os.path.join(BASE_DIR, "logs")
STORAGE_DIR  = os.path.join(BASE_DIR, "storage")

# Ensure runtime directories exist
for d in (PID_DIR, LOG_DIR, STORAGE_DIR):
    os.makedirs(d, exist_ok=True)

# ─────────────────────────────────────────────────────────────
# Environment (portable lib injection)
# ─────────────────────────────────────────────────────────────
def get_env():
    env = os.environ.copy()

    # Remove problematic preload
    env.pop("LD_PRELOAD", None)

    # Inject portable lib path first
    old = env.get("LD_LIBRARY_PATH", "")
    env["LD_LIBRARY_PATH"] = (
        f"{QEMU_LIB_DIR}:{old}" if old else QEMU_LIB_DIR
    )

    return env

# ─────────────────────────────────────────────────────────────
# QEMU commands
# ─────────────────────────────────────────────────────────────
def qemu_img_cmd(*args):
    return [QEMU_IMG] + list(args)


def qemu_system_cmd(arch, *args):
    return [QEMU_WRAPPER, f"system-{arch}"] + list(args)

# ─────────────────────────────────────────────────────────────
# PID / Log helpers
# ─────────────────────────────────────────────────────────────
def pid_file(vm_id):
    return os.path.join(PID_DIR, f"{vm_id}.pid")


def log_file(vm_id):
    return os.path.join(LOG_DIR, f"{vm_id}.log")


def read_pid(vm_id):
    pf = pid_file(vm_id)
    if os.path.exists(pf):
        try:
            with open(pf) as f:
                return int(f.read().strip())
        except:
            pass
    return None


def is_pid_alive(pid):
    if pid is None:
        return False
    try:
        os.kill(pid, 0)
        return True
    except:
        return False


def is_vm_running(vm_id):
    return is_pid_alive(read_pid(vm_id))
