package main

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

// =============================================================
//  VERSION
// =============================================================

const version = "2.0.0"

// =============================================================
//  STRUCTS
// =============================================================

type VMConfig struct {
	RAM        int
	CPU        int
	Disk       string
	ISO        string // intentionally not saved to profile
	SSHPort    int
	VNCDisplay int
	SPICEPort  int
	Audio      bool
	UseVNC     bool
	UseSPICE   bool
	Daemon     bool
	ExtraFwds  []PortForward
	Arch       string
	SpicePass  string // intentionally not saved to profile
}

type PortForward struct {
	HostPort  int
	GuestPort int
}

// =============================================================
//  GLOBAL PATHS
// =============================================================

var (
	vfRoot    string
	configDir string
	lockDir   string
)

// =============================================================
//  ROOT RESOLVER
// =============================================================

func resolveRoot() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// =============================================================
//  LOCK ENGINE
// =============================================================

func lockPath(name string) string {
	return filepath.Join(lockDir, name)
}

func checkLock(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		os.Remove(path)
		return nil
	}

	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		fmt.Printf("  ⚠ Stale lock removed: %s (invalid PID)\n", path)
		os.Remove(path)
		return nil
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		fmt.Printf("  ⚠ Stale lock removed: %s (invalid PID)\n", path)
		os.Remove(path)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err == nil {
		if signalErr := proc.Signal(syscall.Signal(0)); signalErr == nil {
			return fmt.Errorf("port already in use — PID %d still running (lock: %s)", pid, path)
		}
	}

	fmt.Printf("  ⚠ Stale lock removed: %s (PID %d no longer exists)\n", path, pid)
	os.Remove(path)
	return nil
}

func createLock(path string) error {
	// Write "0\n" — matches bash's initial empty-PID convention.
	// checkLock treats pid==0 as stale, so an unupdated lock is self-healing.
	return os.WriteFile(path, []byte("0\n"), 0644)
}

func updateLocksWithPID(cfg *VMConfig, pid int) {
	write := func(name string) {
		os.WriteFile(name, []byte(strconv.Itoa(pid)+"\n"), 0644)
	}
	write(lockPath(fmt.Sprintf("ssh_%d.lock", cfg.SSHPort)))
	if cfg.UseVNC {
		write(lockPath(fmt.Sprintf("vnc_%d.lock", cfg.VNCDisplay)))
	}
	if cfg.UseSPICE {
		write(lockPath(fmt.Sprintf("spice_%d.lock", cfg.SPICEPort)))
	}
	for _, f := range cfg.ExtraFwds {
		write(lockPath(fmt.Sprintf("extra_%d.lock", f.HostPort)))
	}
}

// sweepStaleLocks removes any .lock file in lockDir whose PID is dead.
// This handles the case where a previous session used different ports
// (e.g. VNC display 3 → display 1) leaving orphan lock files behind,
// because those ports are never checked by acquireLocks of the new session.
func sweepStaleLocks() {
	entries, err := os.ReadDir(lockDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".lock") {
			continue
		}
		p := lockPath(name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		pidStr := strings.TrimSpace(string(data))
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			fmt.Printf("  ⚠ Sweeping orphan lock (invalid PID): %s\n", name)
			os.Remove(p)
			continue
		}
		proc, _ := os.FindProcess(pid)
		if proc == nil || proc.Signal(syscall.Signal(0)) != nil {
			fmt.Printf("  ⚠ Sweeping orphan lock (PID %d dead): %s\n", pid, name)
			os.Remove(p)
		}
	}
}

func acquireLocks(cfg *VMConfig) error {
	// Remove stale locks from previous sessions before checking conflicts.
	sweepStaleLocks()

	checks := []string{
		lockPath(fmt.Sprintf("ssh_%d.lock", cfg.SSHPort)),
	}
	if cfg.UseVNC {
		checks = append(checks, lockPath(fmt.Sprintf("vnc_%d.lock", cfg.VNCDisplay)))
	}
	if cfg.UseSPICE {
		checks = append(checks, lockPath(fmt.Sprintf("spice_%d.lock", cfg.SPICEPort)))
	}
	for _, f := range cfg.ExtraFwds {
		checks = append(checks, lockPath(fmt.Sprintf("extra_%d.lock", f.HostPort)))
	}

	for _, p := range checks {
		if err := checkLock(p); err != nil {
			return err
		}
	}
	for _, p := range checks {
		if err := createLock(p); err != nil {
			return fmt.Errorf("failed to create lock %s: %w", p, err)
		}
	}
	return nil
}

func cleanupLocks(cfg *VMConfig) {
	if cfg == nil {
		return // daemon success: locks must persist for the running QEMU process
	}
	fmt.Println("\nCleaning up locks...")
	os.Remove(lockPath(fmt.Sprintf("ssh_%d.lock", cfg.SSHPort)))
	if cfg.UseVNC {
		os.Remove(lockPath(fmt.Sprintf("vnc_%d.lock", cfg.VNCDisplay)))
	}
	if cfg.UseSPICE {
		os.Remove(lockPath(fmt.Sprintf("spice_%d.lock", cfg.SPICEPort)))
	}
	for _, f := range cfg.ExtraFwds {
		os.Remove(lockPath(fmt.Sprintf("extra_%d.lock", f.HostPort)))
	}
}

// =============================================================
//  INPUT HELPERS
// =============================================================

var stdinReader = bufio.NewReader(os.Stdin)

func readLine() string {
	line, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(line)
}

func readInt(prompt string, def int) int {
	for {
		fmt.Printf("%s [%d]: ", prompt, def)
		input := readLine()
		if input == "" {
			return def
		}
		val, err := strconv.Atoi(input)
		if err != nil || val <= 0 {
			fmt.Printf("  ! Please enter a valid number > 0 (e.g. %d)\n", def)
			continue
		}
		return val
	}
}

func readYesNo(prompt string, def bool) bool {
	defLabel := "Y/n"
	if !def {
		defLabel = "y/N"
	}
	fmt.Printf("%s (%s): ", prompt, defLabel)
	input := strings.ToLower(strings.TrimSpace(readLine()))
	if input == "" {
		return def
	}
	return strings.HasPrefix(input, "y")
}

// readPassword reads a password without echoing to the terminal.
// Falls back to visible readLine() if stdin is not a TTY (e.g. piped input).
func readPassword(prompt string) string {
	fmt.Print(prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println() // newline after hidden input
		if err == nil {
			return strings.TrimSpace(string(pw))
		}
	}
	// Not a TTY or read failed — fall back to visible input
	return readLine()
}

// =============================================================
//  VALIDATION
// =============================================================

func validateName(name string) error {
	if name == "" {
		return errors.New("name cannot be empty")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return errors.New("name cannot contain '/' or '..'")
	}
	return nil
}

func validatePort(p int) bool {
	return p >= 1 && p <= 65535
}

func validateDiskPath(path string) error {
	if strings.Contains(path, ",") {
		return fmt.Errorf(
			"disk path contains a comma — not allowed in QEMU drive args\n"+
				"   Please rename the file to remove commas: %s", path)
	}
	return nil
}

// =============================================================
//  PULSEAUDIO (Linux only, best-effort)
// =============================================================

func ensurePulseAudio() {
	if _, err := exec.LookPath("pulseaudio"); err != nil {
		return // not installed — skip silently
	}

	check := exec.Command("pulseaudio", "--check")
	if check.Run() == nil {
		return // already running
	}

	start := exec.Command("pulseaudio",
		"--start",
		"--exit-idle-time=-1",
		"--daemonize=yes",
	)
	start.Stdout = io.Discard
	start.Stderr = io.Discard
	start.Run() // ignore errors — audio is optional
}

// =============================================================
//  SPICE PASSWORD  (3-tier: crypto/rand → urandom → timestamp)
// =============================================================

func generatePassword() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789@#%"
	const length = 16

	buf := make([]byte, length)
	if _, err := rand.Read(buf); err == nil {
		for i := range buf {
			buf[i] = chars[int(buf[i])%len(chars)]
		}
		return string(buf)
	}

	// Fallback: /dev/urandom via od
	if out, err := exec.Command(
		"sh", "-c",
		`od -A n -t x1 /dev/urandom | tr -d ' \n' | head -c 16`,
	).Output(); err == nil && len(out) >= length {
		return string(out[:length])
	}

	// Last resort: timestamp-based (low entropy, warn user)
	ts := strconv.FormatInt(time.Now().UnixNano(), 36)
	for len(ts) < length {
		ts += ts
	}
	fmt.Println("  ⚠ crypto/rand unavailable — using low-entropy fallback password")
	return ts[:length]
}

// =============================================================
//  PROFILE ENGINE
// =============================================================

func listProfiles() ([]string, error) {
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return nil, err
	}
	var profiles []string
	for _, e := range entries {
		if !e.IsDir() {
			profiles = append(profiles, e.Name())
		}
	}
	return profiles, nil
}

func loadProfile(name string, cfg *VMConfig) error {
	if err := validateName(name); err != nil {
		return err
	}
	path := filepath.Join(configDir, name)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("profile not found: %s", name)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]

		switch key {
		case "RAM":
			if v, err := strconv.Atoi(val); err == nil && v > 0 {
				cfg.RAM = v
			}
		case "CPU":
			if v, err := strconv.Atoi(val); err == nil && v > 0 {
				cfg.CPU = v
			}
		case "DISK":
			if err := validateDiskPath(val); err == nil {
				cfg.Disk = val
			}
		case "SSHPORT":
			if v, err := strconv.Atoi(val); err == nil && validatePort(v) {
				cfg.SSHPort = v
			}
		case "VNCDISPLAY":
			if v, err := strconv.Atoi(val); err == nil && v >= 0 {
				cfg.VNCDisplay = v
			}
		case "SPICEPORT":
			if v, err := strconv.Atoi(val); err == nil && validatePort(v) {
				cfg.SPICEPort = v
			}
		case "AUDIO":
			cfg.Audio = val == "1"
		case "USE_VNC":
			cfg.UseVNC = val == "1"
		case "USE_SPICE":
			cfg.UseSPICE = val == "1"
		case "DAEMON":
			cfg.Daemon = val == "1"
		case "ARCH":
			if val == "x86_64" || val == "aarch64" {
				cfg.Arch = val
			}
		case "EXTRA_FWDS":
			if val != "" {
				parseExtraFwds(val, cfg)
			}
		}
	}
	return scanner.Err()
}

func saveProfile(name string, cfg *VMConfig) error {
	if err := validateName(name); err != nil {
		return err
	}
	path := filepath.Join(configDir, name)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	kv := func(k, v string) { fmt.Fprintf(f, "%s=%s\n", k, v) }
	b2i := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}

	// ISO and SpicePass are intentionally NOT saved (same as shell version)
	kv("RAM", strconv.Itoa(cfg.RAM))
	kv("CPU", strconv.Itoa(cfg.CPU))
	kv("DISK", cfg.Disk)
	kv("SSHPORT", strconv.Itoa(cfg.SSHPort))
	kv("VNCDISPLAY", strconv.Itoa(cfg.VNCDisplay))
	kv("SPICEPORT", strconv.Itoa(cfg.SPICEPort))
	kv("AUDIO", b2i(cfg.Audio))
	kv("USE_VNC", b2i(cfg.UseVNC))
	kv("USE_SPICE", b2i(cfg.UseSPICE))
	kv("DAEMON", b2i(cfg.Daemon))
	kv("ARCH", cfg.Arch)
	kv("EXTRA_FWDS", serializeFwds(cfg.ExtraFwds))
	return nil
}

// =============================================================
//  PORT FORWARD HELPERS
// =============================================================

func parseExtraFwds(val string, cfg *VMConfig) {
	for _, pair := range strings.Split(val, ",") {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			continue
		}
		h, e1 := strconv.Atoi(parts[0])
		g, e2 := strconv.Atoi(parts[1])
		if e1 == nil && e2 == nil && validatePort(h) && validatePort(g) {
			cfg.ExtraFwds = append(cfg.ExtraFwds, PortForward{h, g})
		} else {
			fmt.Printf("  ! Invalid port pair skipped: '%s'\n", pair)
		}
	}
}

func serializeFwds(fwds []PortForward) string {
	parts := make([]string, len(fwds))
	for i, f := range fwds {
		parts[i] = fmt.Sprintf("%d:%d", f.HostPort, f.GuestPort)
	}
	return strings.Join(parts, ",")
}

func printFwds(fwds []PortForward) {
	for i, f := range fwds {
		fmt.Printf("    %d) localhost:%-6d  →  VM:%d\n", i+1, f.HostPort, f.GuestPort)
	}
}

// =============================================================
//  EXTRA PORT FORWARD INTERACTIVE ENGINE
// =============================================================

func configureExtraFwds(cfg *VMConfig) {
	fmt.Println("\n----- Extra Port Forwarding -----")
	fmt.Println("  (SSH is already forwarded — this is for additional ports)")

	if len(cfg.ExtraFwds) > 0 {
		fmt.Println("  Loaded from profile:")
		printFwds(cfg.ExtraFwds)
	}

	for {
		fmt.Println("\n  a) Add port")
		fmt.Println("  d) Delete port")
		fmt.Println("  c) Load preset")
		fmt.Println("  s) Done / skip")
		fmt.Print("\n  Choice [s]: ")

		opt := strings.ToLower(readLine())
		if opt == "" {
			opt = "s"
		}

		switch opt {
		case "a":
			fmt.Print("    Host port  (on your machine): ")
			hStr := readLine()
			fmt.Print("    Guest port (inside the VM)  : ")
			gStr := readLine()

			h, e1 := strconv.Atoi(hStr)
			g, e2 := strconv.Atoi(gStr)
			if e1 != nil || e2 != nil || !validatePort(h) || !validatePort(g) {
				fmt.Println("    ! Port must be between 1-65535")
				continue
			}
			// Duplicate host-port check — QEMU will error, better to warn early
			for _, existing := range cfg.ExtraFwds {
				if existing.HostPort == h {
					fmt.Printf("    ⚠ Host port %d is already forwarded (to VM:%d) — QEMU will reject duplicates\n",
						h, existing.GuestPort)
					break
				}
			}
			if h == cfg.SSHPort {
				fmt.Printf("    ⚠ Host port %d is already used for SSH forwarding\n", h)
			}
			cfg.ExtraFwds = append(cfg.ExtraFwds, PortForward{h, g})
			fmt.Printf("    Added: localhost:%d  →  VM:%d\n", h, g)

		case "d":
			if len(cfg.ExtraFwds) == 0 {
				fmt.Println("  No extra ports configured.")
				continue
			}
			printFwds(cfg.ExtraFwds)
			fmt.Print("  Which number to remove? ")
			n, err := strconv.Atoi(readLine())
			if err != nil || n < 1 || n > len(cfg.ExtraFwds) {
				fmt.Println("  ! Please enter a valid number")
				continue
			}
			cfg.ExtraFwds = append(cfg.ExtraFwds[:n-1], cfg.ExtraFwds[n:]...)
			fmt.Println("  Removed.")

		case "c":
			fmt.Println("\n  Presets:")
			fmt.Println("    1) Web dev     — 8000, 8080, 9000")
			fmt.Println("    2) HTTPS / TLS — 443,  8443, 9443")
			fmt.Println("    3) Database    — 5432, 3306, 6379")
			fmt.Println("    4) Full stack  — 8000, 8080, 9000, 5432, 6379")
			fmt.Println("    5) SPICE extra — 5908, 5909")
			fmt.Println("    6) Custom list — enter manually")
			fmt.Print("  Select [1-6]: ")
			sel := readLine()

			switch sel {
			case "1":
				addPreset(cfg, [][2]int{{8000, 8000}, {8080, 8080}, {9000, 9000}})
			case "2":
				addPreset(cfg, [][2]int{{443, 443}, {8443, 8443}, {9443, 9443}})
			case "3":
				addPreset(cfg, [][2]int{{5432, 5432}, {3306, 3306}, {6379, 6379}})
			case "4":
				addPreset(cfg, [][2]int{{8000, 8000}, {8080, 8080}, {9000, 9000}, {5432, 5432}, {6379, 6379}})
			case "5":
				addPreset(cfg, [][2]int{{5908, 5908}, {5909, 5909}})
			case "6":
				fmt.Print("  Enter list (hostport:guestport,...): ")
				parseExtraFwds(readLine(), cfg)
			default:
				fmt.Println("  ! Please choose between 1-6")
				continue
			}
			fmt.Println("  Preset added.")

		case "s":
			return

		default:
			fmt.Println("  ! Please type a / d / c / s")
			continue
		}

		fmt.Println()
		if len(cfg.ExtraFwds) > 0 {
			fmt.Println("  Current extra forwards:")
			printFwds(cfg.ExtraFwds)
		} else {
			fmt.Println("  (no extra ports configured)")
		}
	}
}

func addPreset(cfg *VMConfig, pairs [][2]int) {
	for _, p := range pairs {
		cfg.ExtraFwds = append(cfg.ExtraFwds, PortForward{p[0], p[1]})
	}
}

// =============================================================
//  ARCHITECTURE SELECTION
// =============================================================

func selectArchitecture(cfg *VMConfig) {
	fmt.Println("\n----- Architecture -----")
	fmt.Println("  1) x86_64  (PC / Windows / most Linux ISOs)")
	fmt.Println("  2) aarch64 (ARM64 / Raspberry Pi / ARM Linux)")
	fmt.Printf("Select architecture [%s]: ", cfg.Arch)

	switch readLine() {
	case "1":
		cfg.Arch = "x86_64"
	case "2":
		cfg.Arch = "aarch64"
	}
	fmt.Println("  → Architecture:", cfg.Arch)
}

// =============================================================
//  DISK DISCOVERY
// =============================================================

func discoverDisks() ([]string, error) {
	return filepath.Glob(filepath.Join(vfRoot, "*.qcow2"))
}

func selectDisk(cfg *VMConfig) error {
	fmt.Println("\n----- Available QCOW2 Disks -----")
	disks, _ := discoverDisks()

	if len(disks) == 0 {
		fmt.Println("  No qcow2 disks found.")
		fmt.Print("Enter full disk path: ")
		cfg.Disk = readLine()
	} else {
		for i, d := range disks {
			fmt.Printf("  %d) %s\n", i+1, d)
		}
		fmt.Printf("  %d) Manual path\n", len(disks)+1)
		choice := readInt("Select disk", 1) - 1

		if choice >= 0 && choice < len(disks) {
			cfg.Disk = disks[choice]
		} else {
			fmt.Print("Enter full disk path: ")
			cfg.Disk = readLine()
		}
	}

	return validateDiskPath(cfg.Disk)
}

// =============================================================
//  ISO BOOT SELECTION  (not saved to profile — same as shell)
// =============================================================

func selectISO(cfg *VMConfig) error {
	if !readYesNo("Boot from ISO?", false) {
		return nil
	}
	fmt.Print("ISO Path: ")
	path := readLine()
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("ISO file not found: %s", path)
	}
	cfg.ISO = path
	return nil
}

// =============================================================
//  ARM FIRMWARE DETECTION
// =============================================================

func detectARMFirmware() string {
	candidates := []string{
		"/usr/share/qemu/edk2-aarch64-code.fd",
		"/usr/share/edk2/aarch64/QEMU_EFI-pflash.raw",
		"/usr/share/edk2-ovmf/aarch64/QEMU_EFI.fd",
		"/usr/lib/u-boot/qemu_arm64/u-boot.bin",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			fmt.Println("  ✔ ARM UEFI firmware:", p)
			return p
		}
	}
	fmt.Println("  ⚠ ARM UEFI firmware not found — ISOs may not boot correctly")
	return ""
}

// =============================================================
//  KVM DETECTION
// =============================================================

func detectKVM() bool {
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err == nil {
		f.Close()
		fmt.Println("  ✔ KVM available — hardware acceleration enabled")
		return true
	}
	fmt.Println("  ℹ KVM not available — using TCG (software emulation)")
	return false
}

// =============================================================
//  LAUNCH SUMMARY
// =============================================================

func printSummary(cfg *VMConfig) {
	fmt.Println("\n===== Launch Summary =====")
	fmt.Println("  Disk   :", cfg.Disk)
	fmt.Printf("  RAM    : %d MB  |  CPU: %d cores\n", cfg.RAM, cfg.CPU)
	fmt.Printf("  SSH    : localhost:%d  →  VM:22\n", cfg.SSHPort)
	fmt.Println("  Arch   :", cfg.Arch)
	if cfg.ISO != "" {
		fmt.Printf("  ISO    : %s  (once=d, single boot)\n", cfg.ISO)
	}
	if cfg.UseVNC {
		fmt.Printf("  VNC    : 127.0.0.1:%d\n", cfg.VNCDisplay)
	}
	if cfg.UseSPICE {
		fmt.Printf("  SPICE  : localhost:%d (password protected)\n", cfg.SPICEPort)
	}
	if len(cfg.ExtraFwds) > 0 {
		fmt.Println("  Extra  :")
		printFwds(cfg.ExtraFwds)
	}
	fmt.Println("==========================")
	fmt.Println("\nStarting VM...\n")
}

// =============================================================
//  BUILD QEMU ARGS (injection-safe — no shell interpolation)
// =============================================================

func buildQemuArgs(cfg *VMConfig, useKVM bool, bios string) []string {
	var args []string

	if cfg.Arch == "aarch64" {
		args = append(args, "-machine", "virt", "-cpu", "cortex-a72", "-device", "virtio-gpu-pci")
		if bios != "" {
			args = append(args, "-bios", bios)
		}
	} else {
		args = append(args, "-machine", "type=q35", "-cpu", "max", "-vga", "qxl")
	}

	if useKVM {
		args = append(args, "-accel", "kvm")
	} else {
		args = append(args, "-accel", "tcg,tb-size=2048")
	}

	args = append(args,
		"-rtc", "base=localtime",
		"-m", strconv.Itoa(cfg.RAM),
		"-smp", strconv.Itoa(cfg.CPU),
		"-drive", fmt.Sprintf("file=%s,if=virtio,format=qcow2", cfg.Disk),
	)

	if cfg.ISO != "" {
		args = append(args, "-cdrom", cfg.ISO, "-boot", "order=d,once=d,menu=on")
	} else {
		args = append(args, "-boot", "order=c")
	}

	// Network — build as a single netdev string to avoid extra args
	netArg := fmt.Sprintf("user,id=n1,hostfwd=tcp::%d-:22", cfg.SSHPort)
	for _, f := range cfg.ExtraFwds {
		netArg += fmt.Sprintf(",hostfwd=tcp::%d-:%d", f.HostPort, f.GuestPort)
	}
	args = append(args, "-device", "virtio-net,netdev=n1", "-netdev", netArg)

	if cfg.UseVNC {
		args = append(args, "-vnc", fmt.Sprintf("127.0.0.1:%d", cfg.VNCDisplay))
	}

	if cfg.UseSPICE {
		args = append(args,
			"-object", fmt.Sprintf("secret,id=spice0,data=%s", cfg.SpicePass),
			"-spice", fmt.Sprintf("port=%d,addr=127.0.0.1,password-secret=spice0", cfg.SPICEPort),
			"-device", "virtio-serial-pci",
			"-chardev", "spicevmc,id=spicechannel0,name=vdagent",
			"-device", "virtserialport,chardev=spicechannel0,name=com.redhat.spice.0",
		)
	}

	// ARM does not support ich9-intel-hda
	if cfg.Audio && cfg.Arch != "aarch64" {
		args = append(args,
			"-audiodev", "pa,id=snd0",
			"-device", "ich9-intel-hda",
			"-device", "hda-output,audiodev=snd0",
		)
	}

	return args
}

// =============================================================
//  FATAL — cleanup-aware exit (mirrors bash `trap cleanup EXIT`)
//
//  os.Exit() bypasses defer, so every fatal path after locks are
//  acquired must go through die() instead.  This gives us the same
//  guarantee as bash's EXIT trap: locks are always removed.
// =============================================================

var activeCfg *VMConfig // set once, used by die() and panicHandler()

func die(format string, args ...any) {
	fmt.Printf("❌ "+format+"\n", args...)
	if activeCfg != nil {
		cleanupLocks(activeCfg)
	}
	os.Exit(1)
}

// =============================================================
//  LAUNCH
// =============================================================

func launchVM(cfg *VMConfig, qemuBin string, args []string) error {
	pidFile := filepath.Join(lockDir, fmt.Sprintf("qemu_%d.pid", cfg.SSHPort))

	if cfg.Daemon {
		daemonArgs := append(args, "-daemonize", "-pidfile", pidFile)
		cmd := exec.Command(qemuBin, daemonArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			// QEMU failed — cleanup is handled by the caller via die()
			return fmt.Errorf("QEMU launch failed: %w", err)
		}
		time.Sleep(time.Second)

		data, err := os.ReadFile(pidFile)
		if err != nil {
			fmt.Println("  ⚠ Warning: pidfile not found:", pidFile)
			// VM may still be running; do NOT clean up locks — leave them
			// so a future run detects the conflict correctly.
			return nil
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 0 {
			fmt.Println("  ⚠ Warning: could not parse PID from pidfile")
			return nil
		}
		updateLocksWithPID(cfg, pid)
		fmt.Println("VM started — PID:", pid)
		// Success: virt-forge exits here; QEMU keeps running as a daemon.
		// Locks now contain the real QEMU PID — cleanup NOT called on exit.
		return nil
	}

	// --------------------------------------------------
	// Foreground mode
	// --------------------------------------------------
	cmd := exec.Command(qemuBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("QEMU launch failed: %w", err)
	}

	updateLocksWithPID(cfg, cmd.Process.Pid)
	fmt.Println("VM started — PID:", cmd.Process.Pid)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-sigCh:
		fmt.Println("\nSignal received — terminating VM...")
		cmd.Process.Signal(syscall.SIGTERM)
		<-done
		// Normal signal exit — cleanupLocks runs via defer in main()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("VM exited with error: %w", err)
		}
	}
	return nil
}

// =============================================================
//  MAIN
// =============================================================

func main() {
	fmt.Printf("========== VM Manager v%s ==========\n\n", version)

	var err error
	vfRoot, err = resolveRoot()
	if err != nil {
		fmt.Println("❌ Failed to resolve project root.")
		os.Exit(1)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("❌ Failed to determine home directory.")
		os.Exit(1)
	}
	configDir = filepath.Join(home, ".vm_profiles")
	lockDir = filepath.Join(home, ".virt-forge-locks")

	if err := os.MkdirAll(configDir, 0755); err != nil {
		fmt.Println("❌ Cannot create config dir:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		fmt.Println("❌ Cannot create lock dir:", err)
		os.Exit(1)
	}

	// --------------------------------------------------
	// Defaults
	// --------------------------------------------------
	cfg := &VMConfig{
		RAM:        4096,
		CPU:        2,
		SSHPort:    4444,
		VNCDisplay: 9,
		SPICEPort:  5908,
		Audio:      true,
		UseVNC:     true,
		UseSPICE:   true,
		Daemon:     true,
		Arch:       "x86_64",
	}

	// --------------------------------------------------
	// Profile selection
	// --------------------------------------------------
	fmt.Println("1) Normal Mode  (4 GB RAM, 2 CPU)")
	fmt.Println("2) Low RAM Mode (2 GB RAM, 1 CPU)")
	fmt.Println("3) Load Saved Profile")
	fmt.Print("Select profile [1]: ")

	switch mode := readLine(); mode {
	case "2":
		cfg.RAM = 2048
		cfg.CPU = 1
		cfg.Audio = false
	case "3":
		profiles, _ := listProfiles()
		if len(profiles) == 0 {
			fmt.Println("  (no profiles found)")
		} else {
			fmt.Println("\nAvailable profiles:")
			for _, p := range profiles {
				fmt.Println("  -", p)
			}
			fmt.Print("Enter profile name: ")
			name := readLine()
			if err := validateName(name); err != nil {
				fmt.Println("❌", err)
				os.Exit(1)
			}
			if err := loadProfile(name, cfg); err != nil {
				fmt.Println("❌", err)
				os.Exit(1)
			}
		}
	}

	// --------------------------------------------------
	// Architecture
	// --------------------------------------------------
	selectArchitecture(cfg)

	// --------------------------------------------------
	// Disk
	// --------------------------------------------------
	if err := selectDisk(cfg); err != nil {
		fmt.Println("❌", err)
		os.Exit(1)
	}

	// --------------------------------------------------
	// Manual configuration
	// --------------------------------------------------
	fmt.Println("\n----- Manual Configuration -----")
	cfg.RAM = readInt("RAM in MB", cfg.RAM)
	cfg.CPU = readInt("CPU Cores", cfg.CPU)

	// ISO — interactive only, never saved to profile
	if err := selectISO(cfg); err != nil {
		fmt.Println("❌", err)
		os.Exit(1)
	}

	cfg.Audio = readYesNo("Enable Audio?", cfg.Audio)
	cfg.UseVNC = readYesNo("Enable VNC?", cfg.UseVNC)
	cfg.UseSPICE = readYesNo("Enable SPICE?", cfg.UseSPICE)
	cfg.Daemon = readYesNo("Run in background (daemonize)?", cfg.Daemon)

	cfg.SSHPort = readInt("SSH Forward Port", cfg.SSHPort)

	cfg.VNCDisplay = readInt("VNC Display", cfg.VNCDisplay)
	if cfg.VNCDisplay > 99 {
		fmt.Printf("  ⚠ Warning: VNC display %d is unusually high (normal range: 0–99)\n", cfg.VNCDisplay)
	}

	cfg.SPICEPort = readInt("SPICE Port", cfg.SPICEPort)

	// SPICE password — not saved to profile
	if cfg.UseSPICE {
		fmt.Println("\n----- SPICE Authentication -----")
		pass := readPassword("  SPICE password (Enter for random): ")
		if pass == "" {
			pass = generatePassword()
			fmt.Println("  → Generated SPICE password:", pass)
			fmt.Println("  ⚠ Note this down — SPICE client will require it.")
		}
		cfg.SpicePass = pass
	}

	// --------------------------------------------------
	// Extra port forwarding
	// --------------------------------------------------
	configureExtraFwds(cfg)

	// --------------------------------------------------
	// Acquire locks before launching
	// --------------------------------------------------
	// Register the defer BEFORE acquireLocks so that Ctrl+C (SIGINT default
	// handler) between acquireLocks and the `activeCfg = cfg` assignment
	// cannot leave orphan lock files.  defer runs even when Go's default
	// SIGINT handler terminates the process via a goroutine panic.
	// activeCfg starts nil → cleanupLocks is a no-op until we set it below.
	defer func() { cleanupLocks(activeCfg) }()

	if err := acquireLocks(cfg); err != nil {
		fmt.Println("❌", err)
		os.Exit(1) // locks not yet held — plain exit is fine here
	}
	// From this point on: use die() for all fatal exits so locks are cleaned up.
	activeCfg = cfg // defer above now sees a non-nil cfg → will cleanup on exit

	// --------------------------------------------------
	// Detect QEMU binary
	// --------------------------------------------------
	qemuBin, err := exec.LookPath("qemu-system-" + cfg.Arch)
	if err != nil {
		die("qemu-system-%s not found in PATH.\n   Install with: apt install qemu-system (or equivalent)", cfg.Arch)
	}
	fmt.Println("  ✔ QEMU:", qemuBin)

	// --------------------------------------------------
	// ARM firmware
	// --------------------------------------------------
	bios := ""
	if cfg.Arch == "aarch64" {
		bios = detectARMFirmware()
	}

	// --------------------------------------------------
	// ARM audio guard
	// --------------------------------------------------
	if cfg.Arch == "aarch64" && cfg.Audio {
		fmt.Println("  ⚠ Audio not supported on ARM — disabling")
		cfg.Audio = false
	}

	// --------------------------------------------------
	// XDG_RUNTIME_DIR — required by QEMU / PulseAudio
	// Mirrors: export XDG_RUNTIME_DIR="${TMPDIR:-/tmp}" in the bash version
	// --------------------------------------------------
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tmpdir := os.Getenv("TMPDIR")
		if tmpdir == "" {
			tmpdir = "/tmp"
		}
		os.Setenv("XDG_RUNTIME_DIR", tmpdir)
		fmt.Println("  ℹ XDG_RUNTIME_DIR not set — defaulting to:", tmpdir)
	}
	// Unset PULSE_SERVER so QEMU uses the local daemon, not a remote one
	os.Unsetenv("PULSE_SERVER")

	// --------------------------------------------------
	// PulseAudio (best-effort, Linux only)
	// --------------------------------------------------
	if cfg.Audio {
		ensurePulseAudio()
	}

	// --------------------------------------------------
	// KVM
	// --------------------------------------------------
	useKVM := detectKVM()

	// --------------------------------------------------
	// Disk existence check (final guard before launch)
	// --------------------------------------------------
	if _, err := os.Stat(cfg.Disk); err != nil {
		die("Disk file not found: %s", cfg.Disk)
	}

	// --------------------------------------------------
	// Save profile (ISO & SpicePass intentionally excluded)
	// --------------------------------------------------
	fmt.Println()
	if readYesNo("Save this configuration?", false) {
		var saveName string
		for {
			fmt.Print("Profile name: ")
			saveName = readLine()
			if err := validateName(saveName); err == nil {
				break
			} else {
				fmt.Println("  !", err)
			}
		}
		if err := saveProfile(saveName, cfg); err != nil {
			fmt.Println("  ⚠ Could not save profile:", err)
		} else {
			fmt.Printf("Profile saved: %s/%s\n", configDir, saveName)
		}
	}

	// --------------------------------------------------
	// Summary & launch
	// --------------------------------------------------
	printSummary(cfg)

	args := buildQemuArgs(cfg, useKVM, bios)
	if len(args) == 0 {
		die("Internal error: QEMU args empty")
	}

	if err := launchVM(cfg, qemuBin, args); err != nil {
		die("%s", err)
	}

	// Daemon mode success: QEMU is running independently.
	// Suppress the defer cleanupLocks — locks now hold the real QEMU PID
	// and must persist until QEMU exits.
	if cfg.Daemon {
		activeCfg = nil // tells defer cleanupLocks() to skip
	}
}
