"""vm_store.py — JSON-backed VM registry with port conflict detection."""
import json, os, uuid
from common import BASE_DIR

REGISTRY_FILE = os.path.join(BASE_DIR, "vms.json")

DEFAULTS = {
    "arch":         "x86_64",
    "ram":          "1024",
    "smp":          "2",
    "accel":        "tcg,tb-size=2048",
    "disk_path":    "",
    "net_forwards": "tcp::6666-:22",
    "vnc_display":  1,
    "spice_port":   5930,
    "use_custom":   False,
    "custom_args":  "",
    "status":       "stopped",
}

# ── Helpers ───────────────────────────────────────────────────────────────────

def _load():
    if os.path.exists(REGISTRY_FILE):
        try:
            return json.load(open(REGISTRY_FILE))
        except:
            pass
    return {}


def _save(data):
    json.dump(data, open(REGISTRY_FILE, "w"), indent=2)


# ── Public API ────────────────────────────────────────────────────────────────

def all_vms():
    return _load()


def get_vm(vm_id):
    return _load().get(vm_id)


def create_vm(name):
    data   = _load()
    vm_id  = uuid.uuid4().hex[:8]
    vm     = dict(DEFAULTS)
    vm["id"]   = vm_id
    vm["name"] = name
    # Auto-assign ports so there's no conflict
    used_vnc   = {v["vnc_display"] for v in data.values()}
    used_spice = {v["spice_port"]  for v in data.values()}
    vm["vnc_display"] = _next_free(used_vnc,   1)
    vm["spice_port"]  = _next_free(used_spice, 5930)
    data[vm_id] = vm
    _save(data)
    return vm_id, vm


def update_vm(vm_id, fields: dict):
    data = _load()
    if vm_id not in data:
        raise KeyError(f"VM {vm_id} not found")
    # Port conflict check
    for other_id, other in data.items():
        if other_id == vm_id:
            continue
        if "vnc_display" in fields and fields["vnc_display"] == other["vnc_display"]:
            raise ValueError(f"VNC display :{fields['vnc_display']} already used by '{other['name']}'")
        if "spice_port" in fields and fields["spice_port"] == other["spice_port"]:
            raise ValueError(f"SPICE port {fields['spice_port']} already used by '{other['name']}'")
    data[vm_id].update(fields)
    _save(data)


def delete_vm(vm_id):
    data = _load()
    data.pop(vm_id, None)
    _save(data)


def set_status(vm_id, status):
    data = _load()
    if vm_id in data:
        data[vm_id]["status"] = status
        _save(data)


def _next_free(used: set, base: int) -> int:
    n = base
    while n in used:
        n += 1
    return n
