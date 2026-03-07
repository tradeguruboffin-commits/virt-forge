package main

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// =============================================================
//  VERSION
// =============================================================

const version = "3.0.1"

// =============================================================
//  USAGE
// =============================================================

const usage = `qemu-run v` + version + ` — QEMU VM launcher

Usage:
  qemu-run --disk <path> [options]

Required:
  --disk <path>          QCOW2 disk image path

Profile (default: normal):
  --profile <name>       Built-in: normal, lowram
                         Saved:    any name in ~/.vm_profiles/

VM config (overrides profile defaults):
  --arch    <arch>       x86_64 | aarch64  (default: x86_64)
  --ram     <MB>         RAM in megabytes
  --cpu     <n>          CPU core count
  --iso     <path>       Boot ISO (first boot / OS install)

Network:
  --ssh     <port>       Host SSH forward port  (default: 4444)
  --extra-fwds <list>    Extra port forwards, comma-separated
                         Format: hostport:guestport,...
                         Example: --extra-fwds 8080:8080,5432:5432

Display:
  --vnc     <port>       Enable VNC on given port, e.g. 5901 (default: 5909)
  --no-vnc               Disable VNC
  --spice   <port>       Enable SPICE on given port (requires --spice-pass)
  --spice-pass <pass>    SPICE password (required to activate SPICE)
  --no-spice             Disable SPICE

Audio:
  --audio                Enable audio (requires PulseAudio; off by default)
  --no-audio             Disable audio explicitly (always off for aarch64)

Mode:
  --fg                   Run in foreground (default: background daemon)

Other:
  --help                 Show this help

Examples:
  # Quick launch with defaults
  qemu-run --disk ~/vms/debian.qcow2

  # Low RAM profile, custom SSH port, SPICE enabled
  qemu-run --profile lowram --disk ~/vms/alpine.qcow2 \
           --ssh 5555 --spice 5910 --spice-pass hunter2

  # Install from ISO, foreground mode
  qemu-run --disk ~/vms/new.qcow2 --iso ~/Downloads/debian.iso \
           --ram 2048 --fg

  # Load saved profile, override RAM, add extra ports
  qemu-run --profile myvm --disk ~/vms/dev.qcow2 \
           --ram 8192 --extra-fwds 8080:8080,5432:5432
`

// =============================================================
//  STRUCTS
// =============================================================

type PortForward struct {
	HostPort  int
	GuestPort int
}

type VMConfig struct {
	Arch       string
	Disk       string
	ISO        string // not saved to profile
	SpicePass  string // not saved to profile
	RAM        int
	CPU        int
	SSHPort    int
	VNCPort    int // user-facing port, e.g. 5909; display = VNCPort - 5900
	SPICEPort  int
	ExtraFwds  []PortForward
	Audio      bool
	UseVNC     bool
	UseSPICE   bool
	Daemon     bool
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
//  BUILT-IN PROFILE TEMPLATES
// =============================================================

func profileNormal() *VMConfig {
	return &VMConfig{
		Arch:       "x86_64",
		RAM:        4096,
		CPU:        2,
		SSHPort:    4444,
		VNCPort:    5909,
		SPICEPort:  5908,
		Audio:      false,
		UseVNC:     true,
		UseSPICE:   false,
		Daemon:     true,
	}
}

func profileLowRAM() *VMConfig {
	return &VMConfig{
		Arch:       "x86_64",
		RAM:        2048,
		CPU:        1,
		SSHPort:    4444,
		VNCPort:    5909,
		SPICEPort:  5908,
		Audio:      false,
		UseVNC:     true,
		UseSPICE:   false,
		Daemon:     true,
	}
}

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
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		fmt.Printf("  ⚠ Stale lock removed: %s (invalid PID)\n", path)
		os.Remove(path)
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err == nil {
		if proc.Signal(syscall.Signal(0)) == nil {
			return fmt.Errorf("port already in use — PID %d still running (lock: %s)", pid, path)
		}
	}
	fmt.Printf("  ⚠ Stale lock removed: %s (PID %d no longer exists)\n", path, pid)
	os.Remove(path)
	return nil
}

func createLock(path string) error {
	return os.WriteFile(path, []byte("0\n"), 0644)
}

func updateLocksWithPID(cfg *VMConfig, pid int) {
	write := func(name string) {
		os.WriteFile(name, []byte(strconv.Itoa(pid)+"\n"), 0644)
	}
	write(lockPath(fmt.Sprintf("ssh_%d.lock", cfg.SSHPort)))
	if cfg.UseVNC {
		write(lockPath(fmt.Sprintf("vnc_%d.lock", cfg.VNCPort)))
	}
	if cfg.UseSPICE {
		write(lockPath(fmt.Sprintf("spice_%d.lock", cfg.SPICEPort)))
	}
	for _, f := range cfg.ExtraFwds {
		write(lockPath(fmt.Sprintf("extra_%d.lock", f.HostPort)))
	}
}

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
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
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

func lockPaths(cfg *VMConfig) []string {
	paths := []string{lockPath(fmt.Sprintf("ssh_%d.lock", cfg.SSHPort))}
	if cfg.UseVNC {
		paths = append(paths, lockPath(fmt.Sprintf("vnc_%d.lock", cfg.VNCPort)))
	}
	if cfg.UseSPICE {
		paths = append(paths, lockPath(fmt.Sprintf("spice_%d.lock", cfg.SPICEPort)))
	}
	for _, f := range cfg.ExtraFwds {
		paths = append(paths, lockPath(fmt.Sprintf("extra_%d.lock", f.HostPort)))
	}
	return paths
}

func acquireLocks(cfg *VMConfig) error {
	sweepStaleLocks()
	paths := lockPaths(cfg)
	for _, p := range paths {
		if err := checkLock(p); err != nil {
			return err
		}
	}
	for _, p := range paths {
		if err := createLock(p); err != nil {
			return fmt.Errorf("failed to create lock %s: %w", p, err)
		}
	}
	return nil
}

func cleanupLocks(cfg *VMConfig) {
	if cfg == nil {
		return
	}
	for _, p := range lockPaths(cfg) {
		os.Remove(p)
	}
}

// =============================================================
//  FATAL
// =============================================================

var activeCfg *VMConfig

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "❌ "+format+"\n", args...)
	cleanupLocks(activeCfg)
	os.Exit(1)
}

// =============================================================
//  VALIDATION
// =============================================================

func validatePort(p int) bool {
	return p >= 1 && p <= 65535
}

func validateName(name string) bool {
	return name != "" &&
		!strings.Contains(name, "/") &&
		!strings.Contains(name, "..")
}

func validateDiskPath(path string) error {
	if strings.Contains(path, ",") {
		return fmt.Errorf(
			"disk path contains a comma — not allowed in QEMU drive args\n"+
				"   Please rename: %s", path)
	}
	return nil
}

// =============================================================
//  SAVED PROFILE LOADER
// =============================================================

func loadSavedProfile(name string, cfg *VMConfig) error {
	f, err := os.Open(filepath.Join(configDir, name))
	if err != nil {
		return fmt.Errorf("saved profile not found: %s", name)
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
		case "ARCH":
			if val == "x86_64" || val == "aarch64" {
				cfg.Arch = val
			}
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
			// stored as display number (legacy) — convert to port
			if v, err := strconv.Atoi(val); err == nil && v >= 0 {
				cfg.VNCPort = 5900 + v
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
		case "EXTRA_FWDS":
			if val != "" {
				parseExtraFwds(val, cfg)
			}
		}
	}
	return scanner.Err()
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
			fmt.Fprintf(os.Stderr, "  ⚠ Invalid port pair skipped: '%s'\n", pair)
		}
	}
}

func printFwds(fwds []PortForward) {
	for i, f := range fwds {
		fmt.Printf("    %d) localhost:%-6d  →  VM:%d\n", i+1, f.HostPort, f.GuestPort)
	}
}

// =============================================================
//  SPICE PASSWORD GENERATOR
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
	if out, err := exec.Command(
		"sh", "-c", `od -A n -t x1 /dev/urandom | tr -d ' \n' | head -c 16`,
	).Output(); err == nil && len(out) >= length {
		return string(out[:length])
	}
	ts := strconv.FormatInt(time.Now().UnixNano(), 36)
	for len(ts) < length {
		ts += ts
	}
	return ts[:length]
}

// =============================================================
//  PULSEAUDIO  (Linux only, best-effort)
// =============================================================

func ensurePulseAudio() {
	if _, err := exec.LookPath("pulseaudio"); err != nil {
		return
	}
	if exec.Command("pulseaudio", "--check").Run() == nil {
		return
	}
	cmd := exec.Command("pulseaudio", "--start", "--exit-idle-time=-1", "--daemonize=yes")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Run()
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
	fmt.Printf("  Arch   : %s\n", cfg.Arch)
	fmt.Printf("  Disk   : %s\n", cfg.Disk)
	fmt.Printf("  RAM    : %d MB  |  CPU: %d cores\n", cfg.RAM, cfg.CPU)
	fmt.Printf("  SSH    : localhost:%d  →  VM:22\n", cfg.SSHPort)
	if cfg.ISO != "" {
		fmt.Printf("  ISO    : %s\n", cfg.ISO)
	}
	if cfg.UseVNC {
		fmt.Printf("  VNC    : 127.0.0.1:%d\n", cfg.VNCPort)
	}
	if cfg.UseSPICE {
		fmt.Printf("  SPICE  : localhost:%d (password set)\n", cfg.SPICEPort)
	}
	if len(cfg.ExtraFwds) > 0 {
		fmt.Println("  Extra  :")
		printFwds(cfg.ExtraFwds)
	}
	mode := "foreground"
	if cfg.Daemon {
		mode = "daemon (background)"
	}
	fmt.Printf("  Mode   : %s\n", mode)
	fmt.Println("==========================")
	fmt.Println()
}

// =============================================================
//  BUILD QEMU ARGS
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

	netArg := fmt.Sprintf("user,id=n1,hostfwd=tcp::%d-:22", cfg.SSHPort)
	for _, f := range cfg.ExtraFwds {
		netArg += fmt.Sprintf(",hostfwd=tcp::%d-:%d", f.HostPort, f.GuestPort)
	}
	args = append(args, "-device", "virtio-net,netdev=n1", "-netdev", netArg)

	if cfg.UseVNC {
		display := cfg.VNCPort - 5900
		args = append(args, "-vnc", fmt.Sprintf("127.0.0.1:%d", display))
	} else {
		args = append(args, "-display", "none")
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

	if cfg.Audio && cfg.Arch != "aarch64" {
		args = append(args,
			"-audiodev", "pa,id=snd0",
			"-device", "ich9-intel-hda",
			"-device", "hda-output,audiodev=snd0",
		)
	} else {
		// Explicitly suppress QEMU's default audio backend so it doesn't
		// attempt to connect to PulseAudio and print warnings.
		args = append(args, "-audiodev", "none,id=snd0")
	}

	return args
}

// =============================================================
//  LAUNCH
// =============================================================

func launchVM(cfg *VMConfig, qemuBin string, args []string) error {
	if cfg.Daemon {
		return launchDaemon(cfg, qemuBin, args)
	}
	return launchForeground(cfg, qemuBin, args)
}

// launchDaemon starts QEMU in a new kernel session (Setsid=true) so it
// survives after the launching terminal (e.g. xterm) closes.
// QEMU's own -daemonize flag is intentionally avoided — it is absent
// in several distro builds (notably ARM cross-compiled packages).
// Stderr is captured to a temp file; if QEMU exits within 800 ms the
// error is surfaced to the caller instead of being silently swallowed.
func launchDaemon(cfg *VMConfig, qemuBin string, args []string) error {
	pidFile := filepath.Join(lockDir, fmt.Sprintf("qemu_%d.pid", cfg.SSHPort))
	errLog  := filepath.Join(lockDir, fmt.Sprintf("qemu_%d.err", cfg.SSHPort))

	errF, _ := os.Create(errLog)

	cmd := exec.Command(qemuBin, args...)
	cmd.Stdin  = nil
	cmd.Stdout = nil
	cmd.Stderr = errF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		errF.Close()
		os.Remove(errLog)
		return fmt.Errorf("QEMU launch failed: %w", err)
	}

	pid := cmd.Process.Pid
	cmd.Process.Release()
	errF.Close()

	// Brief wait — lets QEMU initialise or fail fast.
	time.Sleep(800 * time.Millisecond)

	proc, _ := os.FindProcess(pid)
	if proc == nil || proc.Signal(syscall.Signal(0)) != nil {
		errData, _ := os.ReadFile(errLog)
		os.Remove(errLog)
		msg := strings.TrimSpace(string(errData))
		if msg == "" {
			msg = "(no error output captured)"
		}
		return fmt.Errorf("QEMU exited immediately:\n%s", msg)
	}
	os.Remove(errLog)

	os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0644)
	updateLocksWithPID(cfg, pid)

	fmt.Printf("✅ VM started — PID: %d\n", pid)
	fmt.Printf("   SSH   : ssh user@localhost -p %d\n", cfg.SSHPort)
	if cfg.UseSPICE {
		fmt.Printf("   SPICE : remote-viewer spice://localhost:%d\n", cfg.SPICEPort)
	}
	if cfg.UseVNC {
		fmt.Printf("   VNC   : 127.0.0.1:%d\n", cfg.VNCPort)
	}
	return nil
}

// launchForeground runs QEMU attached to the current terminal.
// Ctrl-C / SIGTERM is forwarded to QEMU; locks are cleaned up by main's defer.
func launchForeground(cfg *VMConfig, qemuBin string, args []string) error {
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
	case err := <-done:
		if err != nil {
			return fmt.Errorf("VM exited with error: %w", err)
		}
	}
	return nil
}

// =============================================================
//  MINIMAL FLAG PARSER
//
//  Supports:  --key value   --key=value   --bool-flag
//  Boolean flags must be registered with registerBool() before parse().
// =============================================================

type flagSet struct {
	args  []string
	seen  map[string]string
	bools map[string]bool
}

func newFlagSet(args []string) *flagSet {
	return &flagSet{
		args:  args,
		seen:  make(map[string]string),
		bools: make(map[string]bool),
	}
}

func (f *flagSet) registerBool(keys ...string) {
	for _, k := range keys {
		f.bools[k] = true
	}
}

func (f *flagSet) parse() error {
	i := 0
	for i < len(f.args) {
		arg := f.args[i]
		if !strings.HasPrefix(arg, "--") {
			i++
			continue
		}
		key := strings.TrimPrefix(arg, "--")

		if idx := strings.IndexByte(key, '='); idx >= 0 {
			f.seen[key[:idx]] = key[idx+1:]
			i++
			continue
		}
		if f.bools[key] {
			f.seen[key] = ""
			i++
			continue
		}
		if i+1 >= len(f.args) {
			return fmt.Errorf("flag --%s requires a value", key)
		}
		f.seen[key] = f.args[i+1]
		i += 2
	}
	return nil
}

func (f *flagSet) has(key string) bool       { _, ok := f.seen[key]; return ok }
func (f *flagSet) str(key, def string) string { if v, ok := f.seen[key]; ok { return v }; return def }

func (f *flagSet) integer(key string, def int) (int, error) {
	v, ok := f.seen[key]
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("--%s: '%s' is not a valid integer", key, v)
	}
	return n, nil
}

// =============================================================
//  MAIN
// =============================================================

func main() {
	for _, a := range os.Args[1:] {
		if a == "--help" || a == "-h" {
			fmt.Print(usage)
			os.Exit(0)
		}
	}

	// ── Resolve paths ─────────────────────────────────────────
	var err error
	vfRoot, err = resolveRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌ Failed to resolve project root.")
		os.Exit(1)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌ Failed to determine home directory.")
		os.Exit(1)
	}
	configDir = filepath.Join(home, ".vm_profiles")
	lockDir   = filepath.Join(home, ".virt-forge-locks")

	for _, dir := range []string{configDir, lockDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Cannot create directory %s: %v\n", dir, err)
			os.Exit(1)
		}
	}

	// ── Parse flags ───────────────────────────────────────────
	flags := newFlagSet(os.Args[1:])
	flags.registerBool("no-vnc", "no-spice", "no-audio", "audio", "fg")
	if err := flags.parse(); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		fmt.Fprintln(os.Stderr, "Run with --help for usage.")
		os.Exit(1)
	}

	// ── Load base profile ─────────────────────────────────────
	profileName := flags.str("profile", "normal")
	var cfg *VMConfig

	switch profileName {
	case "normal":
		cfg = profileNormal()
	case "lowram":
		cfg = profileLowRAM()
	default:
		if !validateName(profileName) {
			fmt.Fprintf(os.Stderr, "❌ Invalid profile name: %s\n", profileName)
			os.Exit(1)
		}
		cfg = profileNormal() // sane defaults as base
		if err := loadSavedProfile(profileName, cfg); err != nil {
			fmt.Fprintln(os.Stderr, "❌", err)
			os.Exit(1)
		}
		fmt.Printf("  ✔ Loaded profile: %s\n", profileName)
	}

	// ── Apply flag overrides ───────────────────────────────────

	// --arch
	if arch := flags.str("arch", ""); arch != "" {
		if arch != "x86_64" && arch != "aarch64" {
			fmt.Fprintln(os.Stderr, "❌ --arch must be x86_64 or aarch64")
			os.Exit(1)
		}
		cfg.Arch = arch
	}

	// --disk (required)
	disk := flags.str("disk", "")
	if disk == "" {
		fmt.Fprintln(os.Stderr, "❌ --disk is required")
		fmt.Fprintln(os.Stderr, "Run with --help for usage.")
		os.Exit(1)
	}
	if err := validateDiskPath(disk); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	cfg.Disk = disk

	// --iso
	if iso := flags.str("iso", ""); iso != "" {
		if _, err := os.Stat(iso); err != nil {
			fmt.Fprintf(os.Stderr, "❌ ISO file not found: %s\n", iso)
			os.Exit(1)
		}
		cfg.ISO = iso
	}

	// --ram
	if ram, err := flags.integer("ram", cfg.RAM); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	} else {
		cfg.RAM = ram
	}
	if cfg.RAM <= 0 {
		fmt.Fprintln(os.Stderr, "❌ --ram must be > 0")
		os.Exit(1)
	}

	// --cpu
	if cpu, err := flags.integer("cpu", cfg.CPU); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	} else {
		cfg.CPU = cpu
	}
	if cfg.CPU <= 0 {
		fmt.Fprintln(os.Stderr, "❌ --cpu must be > 0")
		os.Exit(1)
	}

	// --ssh
	if ssh, err := flags.integer("ssh", cfg.SSHPort); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	} else if !validatePort(ssh) {
		fmt.Fprintln(os.Stderr, "❌ --ssh: port out of range (1-65535)")
		os.Exit(1)
	} else {
		cfg.SSHPort = ssh
	}

	// --vnc / --no-vnc
	if flags.has("no-vnc") {
		cfg.UseVNC = false
	} else if vnc, err := flags.integer("vnc", -1); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	} else if vnc >= 0 {
		if vnc < 5900 || vnc > 65535 {
			fmt.Fprintln(os.Stderr, "❌ --vnc: port must be >= 5900 (e.g. 5901, 5909)")
			os.Exit(1)
		}
		cfg.VNCPort = vnc
		cfg.UseVNC = true
	}

	// --spice / --no-spice / --spice-pass
	spicePass := flags.str("spice-pass", "")
	switch {
	case flags.has("no-spice"):
		cfg.UseSPICE = false
		cfg.SpicePass = ""

	case flags.has("spice"):
		spicePort, err := flags.integer("spice", cfg.SPICEPort)
		if err != nil {
			fmt.Fprintln(os.Stderr, "❌", err)
			os.Exit(1)
		}
		if !validatePort(spicePort) {
			fmt.Fprintln(os.Stderr, "❌ --spice: port out of range (1-65535)")
			os.Exit(1)
		}
		if spicePass == "" {
			fmt.Println("  ⚠ --spice given but --spice-pass is missing — SPICE disabled")
			cfg.UseSPICE = false
		} else {
			cfg.SPICEPort = spicePort
			cfg.SpicePass = spicePass
			cfg.UseSPICE = true
		}

	case spicePass != "":
		// --spice-pass without --spice → use default port from profile
		cfg.SpicePass = spicePass
		cfg.UseSPICE = true
	}

	// --audio / --no-audio
	if flags.has("audio") {
		cfg.Audio = true
	}
	if flags.has("no-audio") {
		cfg.Audio = false
	}

	// --fg
	if flags.has("fg") {
		cfg.Daemon = false
	}

	// --extra-fwds
	if fwds := flags.str("extra-fwds", ""); fwds != "" {
		parseExtraFwds(fwds, cfg)
	}

	// ── ARM guard ─────────────────────────────────────────────
	if cfg.Arch == "aarch64" && cfg.Audio {
		fmt.Println("  ⚠ Audio not supported on aarch64 — disabling")
		cfg.Audio = false
	}

	// ── Disk existence check ──────────────────────────────────
	if _, err := os.Stat(cfg.Disk); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Disk file not found: %s\n", cfg.Disk)
		os.Exit(1)
	}

	// ── Detect QEMU binary ────────────────────────────────────
	qemuBin, err := exec.LookPath("qemu-system-" + cfg.Arch)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"❌ qemu-system-%s not found in PATH\n   Install: apt install qemu-system\n", cfg.Arch)
		os.Exit(1)
	}
	fmt.Println("  ✔ QEMU:", qemuBin)

	// ── ARM firmware ──────────────────────────────────────────
	bios := ""
	if cfg.Arch == "aarch64" {
		bios = detectARMFirmware()
	}

	// ── Environment ───────────────────────────────────────────
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		tmp := os.Getenv("TMPDIR")
		if tmp == "" {
			tmp = "/tmp"
		}
		os.Setenv("XDG_RUNTIME_DIR", tmp)
		fmt.Println("  ℹ XDG_RUNTIME_DIR not set — defaulting to:", tmp)
	}
	os.Unsetenv("PULSE_SERVER")

	if cfg.Audio {
		ensurePulseAudio()
	}

	useKVM := detectKVM()

	// ── Acquire locks ─────────────────────────────────────────
	defer func() { cleanupLocks(activeCfg) }()

	if err := acquireLocks(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	activeCfg = cfg

	// ── Summary & launch ──────────────────────────────────────
	printSummary(cfg)

	qemuArgs := buildQemuArgs(cfg, useKVM, bios)

	if err := launchVM(cfg, qemuBin, qemuArgs); err != nil {
		die("%s", err)
	}

	// Daemon success — locks must persist while QEMU runs.
	if cfg.Daemon {
		activeCfg = nil
	}
}
