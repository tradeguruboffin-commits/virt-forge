# virt-forge — QEMU VM Manager

busybox + QEMU দিয়ে তৈরি portable VM management system।
কোনো বাইরের dependency নেই — সব `./bin/` এর টুল থেকে চলে।

---

## প্রজেক্ট স্ট্রাকচার

```
/opt/virt-forge/
├── bin/
│   ├── qemu             ← মেইন wrapper (এটাই ব্যবহার করুন)
│   ├── busybox          ← সব unix টুলের base
│   └── ...symlinks
├── engine/
│   ├── bin/
│   │   ├── qemu-system-x86_64
│   │   ├── qemu-system-aarch64
│   │   ├── qemu-system-riscv64
│   │   └── qemu-img
│   ├── lib/             ← সব shared libraries
│   ├── modules/         ← QEMU modules
│   └── qemu/            ← firmware, OVMF, keymaps
├── disks/               ← qcow2 disk images
├── isos/                ← ISO ফাইল রাখুন এখানে
├── vms/
│   └── <n>/
│       ├── config       ← ম্যানুয়াল config (এডিট করুন)
│       └── config.auto  ← অটো জেনারেট (সরাসরি এডিট করবেন না)
├── runtime/
│   ├── logs/            ← VM লগ
│   └── pids/            ← running VM pid ফাইল
└── libexec/             ← internal scripts (সরাসরি কল করবেন না)
    ├── lib.sh
    ├── cmd-create.sh
    ├── cmd-start.sh
    ├── cmd-stop.sh
    ├── cmd-list.sh
    ├── cmd-info.sh
    ├── cmd-attach-iso.sh
    ├── cmd-console.sh
    └── cmd-delete.sh
```

---

## দ্রুত শুরু

```bash
# PATH এ যোগ করুন
export PATH="/opt/virt-forge/bin:$PATH"

# ISO রাখুন
cp debian-12.iso /opt/virt-forge/isos/

# VM তৈরি করুন
qemu create myvm --arch x86_64 --disk 20G --ram 2G --cpu 2

# ISO attach করুন
qemu attach-iso myvm /opt/virt-forge/isos/debian-12.iso

# VM চালু করুন
qemu start myvm

# অবস্থা দেখুন
qemu list

# বিস্তারিত তথ্য
qemu info myvm

# SSH Console
qemu console myvm

# বন্ধ করুন
qemu stop myvm

# মুছে দিন
qemu delete myvm
```

---

## Config সিস্টেম

Config দুই স্তরে কাজ করে:

### 1. `config.auto` (অটো)
`qemu create` এ অটো তৈরি হয়। সরাসরি এডিট করবেন না।

### 2. `config` (ম্যানুয়াল)
আপনার পরিবর্তন এখানে করুন। এই ফাইলের মান সব সময় `config.auto` এর উপরে।

```bash
# শুধু যা বদলাতে চান uncomment করুন
RAM=4G
CPU=4
ACCEL=kvm          # KVM থাকলে
SPICE_PORT=5931    # port conflict হলে
EXTRA_ARGS=-device usb-tablet -usb
```

### Config Priority
```
manual config  >  config.auto  >  built-in defaults
```

---

## Boot মোড

| পরিস্থিতি | কী হয় |
|-----------|--------|
| DISK আছে, ISO নেই | disk থেকে boot |
| DISK আছে, ISO আছে | ISO দিয়ে install, disk এ save |
| DISK নেই, ISO নেই | isos/ থেকে সিলেক্ট করতে বলে + নতুন disk তৈরির অফার |
| `--iso` দিয়ে start | config এর ISO override করে |

---

## সব Command

```bash
qemu create   <n> --arch x86_64 --disk 20G --ram 2G --cpu 2
qemu start    <n> [--iso path] [--ram 2G] [--cpu 4] [--dry-run]
qemu start    --all
qemu stop     <n> [--force] [--timeout 30]
qemu stop     --all
qemu list
qemu info     <n>
qemu attach-iso <n> [iso-path]
qemu console  <n>
qemu delete   <n> [--force] [--keep-disk]
```

---

## Architectures

| Arch | QEMU Binary | Machine |
|------|-------------|---------|
| x86_64 | qemu-system-x86_64 | pc |
| aarch64 | qemu-system-aarch64 | virt + cortex-a72 |
| riscv64 | qemu-system-riscv64 | virt + opensbi |
