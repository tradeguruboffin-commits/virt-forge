package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// =============================================================
//  PATHS
// =============================================================

var (
	projectRoot string
	logFile     string
	lockDir     string
)

func initPaths() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	projectRoot = filepath.Dir(exe)

	logFile = os.Getenv("LOG")
	if logFile == "" {
		logFile = filepath.Join(projectRoot, "virt-forge-qemu.log")
	}

	lockDir = os.Getenv("LOCK_DIR")
	if lockDir == "" {
		home, _ := os.UserHomeDir()
		lockDir = filepath.Join(home, ".virt-forge-locks")
	}

	return os.MkdirAll(lockDir, 0755)
}

// =============================================================
//  LOGGING
// =============================================================

func logEvent(msg string) {
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	ts := time.Now().Format(time.RFC1123)
	fmt.Fprintf(f, "===== %s =====\n%s\n\n", ts, msg)
}

// =============================================================
//  PROCESS DISCOVERY  (BusyBox-safe fallback chain)
//
//  [bash parity] Mirrors get_procs() fallback order:
//    ps w  →  ps  →  ps aux  →  ps -eo pid,args
//
//  Self-exclusion: bash uses the [q]emu-system- bracket trick so
//  grep itself doesn't appear in the output.  In Go we filter by
//  checking strings.Contains(line, "qemu-system-") and explicitly
//  excluding lines that contain the word "grep" — same effect,
//  no shell required.
// =============================================================

func runPS(args ...string) (string, error) {
	out, err := exec.Command("ps", args...).Output()
	return string(out), err
}

func filterQemuLines(output string) string {
	var result []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "qemu-system-") &&
			!strings.Contains(line, "grep") {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

func getProcs() string {
	variants := [][]string{
		{"w"},
		{},
		{"aux"},
		{"-eo", "pid,args"},
	}
	for _, args := range variants {
		out, err := runPS(args...)
		if err != nil || out == "" {
			continue
		}
		if lines := filterQemuLines(out); lines != "" {
			return lines
		}
	}
	return ""
}

// =============================================================
//  FIELD PARSERS
//
//  getPID — scans fields for first numeric token, handles both
//  "PID CMD" and "USER PID %CPU... CMD" (ps aux) layouts.
//
//  getCmd — finds the first field containing "/" or "qemu" and
//  returns from there to EOL.  Fallback: strip leading PID field.
//
//  getFullCmd — reads the COMPLETE, untruncated command line from
//  /proc/<pid>/cmdline (Linux).  ps truncates long argument lists
//  which breaks hostfwd= and -vnc parsing when QEMU has many args.
//  Falls back to the ps-derived cmd on non-Linux or read failure.
// =============================================================

func getPID(line string) string {
	for _, f := range strings.Fields(line) {
		if isNumeric(f) {
			return f
		}
	}
	return ""
}

func isNumeric(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func getCmd(line string) string {
	fields := strings.Fields(line)
	for i, f := range fields {
		if strings.Contains(f, "/") || strings.Contains(f, "qemu") {
			return strings.Join(fields[i:], " ")
		}
	}
	// Fallback: strip leading numeric PID field
	if len(fields) > 1 && isNumeric(fields[0]) {
		return strings.Join(fields[1:], " ")
	}
	return strings.TrimSpace(line)
}

// getFullCmd reads /proc/<pid>/cmdline — argv stored as NUL-separated
// bytes, never truncated unlike ps output.
// Returns (fullCmd, true) on Linux, ("", false) on failure/non-Linux.
func getFullCmd(pid string) (string, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil || len(data) == 0 {
		return "", false
	}
	// NUL separators → spaces, matching how ps would render it
	return strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", " ")), true
}

// resolveCmd returns the best available command string for a ps line:
// prefers /proc/<pid>/cmdline (untruncated), falls back to ps-parsed cmd.
func resolveCmd(line string) string {
	pid := getPID(line)
	if pid != "" {
		if full, ok := getFullCmd(pid); ok {
			return full
		}
	}
	return getCmd(line)
}

// =============================================================
//  ARGUMENT EXTRACTION  (compiled once at startup)
// =============================================================

var (
	reArch      = regexp.MustCompile(`qemu-system-([a-z0-9_]+)`)
	reDrive     = regexp.MustCompile(`file=([^, ]+)`)
	reHDA       = regexp.MustCompile(`-hda\s+(\S+)`)
	reRAM       = regexp.MustCompile(`-m\s+([0-9]+)`)
	reSSH       = regexp.MustCompile(`hostfwd=tcp::([0-9]+)-:22\b`)
	reAllFwd    = regexp.MustCompile(`hostfwd=tcp::([0-9]+)-:([0-9]+)`)
	reVNC       = regexp.MustCompile(`-vnc\s+(\S+)`)
	reVNCDisp   = regexp.MustCompile(`-vnc\s+127\.0\.0\.1:([0-9]+)`)
	reSpiceBlk  = regexp.MustCompile(`-spice\s+(\S+)`)
	reSpicePort = regexp.MustCompile(`\bport=([0-9]+)`)
)

func getArch(cmd string) string {
	if m := reArch.FindStringSubmatch(cmd); len(m) > 1 {
		return m[1]
	}
	return ""
}

// getDisk — tries -drive file= first, then legacy -hda
func getDisk(cmd string) string {
	if m := reDrive.FindStringSubmatch(cmd); len(m) > 1 {
		return m[1]
	}
	if m := reHDA.FindStringSubmatch(cmd); len(m) > 1 {
		return m[1]
	}
	return ""
}

func getRAM(cmd string) string {
	if m := reRAM.FindStringSubmatch(cmd); len(m) > 1 {
		return m[1]
	}
	return ""
}

func getSSHPort(cmd string) string {
	if m := reSSH.FindStringSubmatch(cmd); len(m) > 1 {
		return m[1]
	}
	return ""
}

// getExtraPorts — host-side ports for all hostfwd rules whose guest port != 22
func getExtraPorts(cmd string) []string {
	var ports []string
	for _, m := range reAllFwd.FindAllStringSubmatch(cmd, -1) {
		if len(m) > 2 && m[2] != "22" {
			ports = append(ports, m[1])
		}
	}
	return ports
}

func getVNC(cmd string) string {
	if m := reVNC.FindStringSubmatch(cmd); len(m) > 1 {
		return m[1]
	}
	return ""
}

// getVNCDisplay — extracts the bare display number from -vnc 127.0.0.1:N
// for lock filenames (vnc_N.lock).
func getVNCDisplay(cmd string) string {
	if m := reVNCDisp.FindStringSubmatch(cmd); len(m) > 1 {
		return m[1]
	}
	return ""
}

// getSpicePort — grabs the -spice argument block first, then port= within it.
// Required because -spice and port= are space-separated in QEMU args.
func getSpicePort(cmd string) string {
	if blk := reSpiceBlk.FindStringSubmatch(cmd); len(blk) > 1 {
		if m := reSpicePort.FindStringSubmatch(blk[1]); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// =============================================================
//  INFO PRINTER
// =============================================================

func parseInfo(cmd string) {
	if v := getArch(cmd); v != "" {
		fmt.Println("    Arch      :", v)
	}
	if v := getDisk(cmd); v != "" {
		fmt.Println("    Disk      :", v)
	}
	if v := getRAM(cmd); v != "" {
		fmt.Println("    RAM       :", v, "MB")
	}
	if v := getSSHPort(cmd); v != "" {
		fmt.Println("    SSH       : localhost:" + v)
	}
	if v := getVNC(cmd); v != "" {
		fmt.Println("    VNC       :", v)
	}
	if v := getSpicePort(cmd); v != "" {
		fmt.Println("    SPICE     : localhost:" + v)
	}
	if extras := getExtraPorts(cmd); len(extras) > 0 {
		fmt.Println("    Extra Ports:")
		for _, p := range extras {
			fmt.Println("      localhost:" + p)
		}
	}
}

// =============================================================
//  LOCK HANDLING
// =============================================================

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func pidAlive(pidStr string) bool {
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// removeLocksFor removes all lock files associated with a QEMU command line.
func removeLocksFor(cmd string) {
	if ssh := getSSHPort(cmd); ssh != "" {
		os.Remove(filepath.Join(lockDir, "ssh_"+ssh+".lock"))
		os.Remove(filepath.Join(lockDir, "qemu_"+ssh+".pid"))
	}
	if d := getVNCDisplay(cmd); d != "" {
		os.Remove(filepath.Join(lockDir, "vnc_"+d+".lock"))
	}
	if sp := getSpicePort(cmd); sp != "" {
		os.Remove(filepath.Join(lockDir, "spice_"+sp+".lock"))
	}
	for _, p := range getExtraPorts(cmd) {
		os.Remove(filepath.Join(lockDir, "extra_"+p+".lock"))
	}
}

// scanStaleOrphans scans lockDir for lock files whose PID is dead.
// These are leftover from crashed/killed virt-forge sessions where
// cleanup was skipped. Reports them but does not remove automatically.
func scanStaleOrphans(activePorts map[string]bool) {
	entries, err := os.ReadDir(lockDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		// Only look at .lock files not belonging to any running VM
		if !strings.HasSuffix(name, ".lock") {
			continue
		}
		// Strip prefix and suffix to get the port/display value
		port := strings.TrimSuffix(name, ".lock")
		for _, pfx := range []string{"ssh_", "vnc_", "spice_", "extra_"} {
			port = strings.TrimPrefix(port, pfx)
		}
		if activePorts[name] {
			continue // belongs to a running VM — already shown
		}
		// Check if the PID inside is still alive
		data, _ := os.ReadFile(filepath.Join(lockDir, name))
		pidStr := strings.TrimSpace(string(data))
		if pidStr == "" || pidStr == "0" || !pidAlive(pidStr) {
			fmt.Printf("    ⚠ Orphan  : %s (PID %s dead — run: rm %s/%s)\n",
				name, pidStr, lockDir, name)
		}
	}
}

// showLocks displays which lock files exist for a running VM,
// validates pidfile liveness, and warns about stale orphan locks
// left over from previous sessions.
func showLocks(cmd string) {
	lockInfo := ""
	// activePorts tracks lock filenames that belong to this VM
	// so scanStaleOrphans can skip them.
	activePorts := map[string]bool{}

	if ssh := getSSHPort(cmd); ssh != "" {
		name := "ssh_" + ssh + ".lock"
		activePorts[name] = true
		if fileExists(filepath.Join(lockDir, name)) {
			lockInfo += " ssh:" + ssh
		}
	}
	if d := getVNCDisplay(cmd); d != "" {
		name := "vnc_" + d + ".lock"
		activePorts[name] = true
		if fileExists(filepath.Join(lockDir, name)) {
			lockInfo += " vnc:" + d
		}
	}
	if sp := getSpicePort(cmd); sp != "" {
		name := "spice_" + sp + ".lock"
		activePorts[name] = true
		if fileExists(filepath.Join(lockDir, name)) {
			lockInfo += " spice:" + sp
		}
	}
	for _, p := range getExtraPorts(cmd) {
		name := "extra_" + p + ".lock"
		activePorts[name] = true
		if fileExists(filepath.Join(lockDir, name)) {
			lockInfo += " extra:" + p
		}
	}

	if lockInfo == "" {
		fmt.Println("    Locks     : (none — expected locks missing)")
	} else {
		fmt.Println("    Locks     :" + lockInfo)
	}

	// PIDFile check
	if ssh := getSSHPort(cmd); ssh != "" {
		pf := filepath.Join(lockDir, "qemu_"+ssh+".pid")
		if fileExists(pf) {
			data, _ := os.ReadFile(pf)
			pidStr := strings.TrimSpace(string(data))
			if pidStr != "" {
				if pidAlive(pidStr) {
					fmt.Printf("    PIDFile   : %s (PID %s — running)\n", pf, pidStr)
				} else {
					fmt.Printf("    PIDFile   : stale (PID %s gone) — removing\n", pidStr)
					os.Remove(pf)
				}
			}
		}
	}

	// Warn about orphan locks from previous sessions
	scanStaleOrphans(activePorts)
}

// =============================================================
//  SIGNAL HELPERS
// =============================================================

func terminatePID(pidStr string) error {
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return errors.New("invalid PID")
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

func forceKillPID(pidStr string) error {
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return errors.New("invalid PID")
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}

// =============================================================
//  COMMAND: LIST
// =============================================================

func cmdList() {
	fmt.Println("\n====== Running VMs ======\n")

	procs := strings.TrimSpace(getProcs())
	if procs == "" {
		fmt.Println("  No running VMs.\n")
		return
	}

	for i, line := range strings.Split(procs, "\n") {
		pid := getPID(line)
		cmd := resolveCmd(line) // uses /proc if available
		fmt.Printf("  [%d] PID: %s\n", i+1, pid)
		parseInfo(cmd)
		fmt.Println()
	}
}

// =============================================================
//  COMMAND: STATUS
// =============================================================

func cmdStatus() {
	fmt.Println("\n====== VM Status ======\n")

	procs := strings.TrimSpace(getProcs())
	if procs == "" {
		fmt.Println("  No running VMs.\n")
		return
	}

	for _, line := range strings.Split(procs, "\n") {
		pid := getPID(line)
		cmd := resolveCmd(line) // uses /proc if available

		status := "✘ Not responding"
		if pidAlive(pid) {
			status = "✔ Running"
		}

		fmt.Printf("  PID %s — %s\n", pid, status)
		parseInfo(cmd)
		showLocks(cmd)
		fmt.Println()
	}

	logEvent("Status checked by user")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		fmt.Fprintln(f, "Running QEMU Processes:")
		fmt.Fprintln(f, procs)
		fmt.Fprintln(f)
	}
}

// =============================================================
//  COMMAND: STOP
// =============================================================

func cmdStop() {
	fmt.Println("\n====== Stop a VM ======\n")

	procs := strings.TrimSpace(getProcs())
	if procs == "" {
		fmt.Println("  No running VMs.\n")
		return
	}

	lines := strings.Split(procs, "\n")
	// Resolve full cmds once — used for both display and lock removal
	cmds := make([]string, len(lines))
	for i, line := range lines {
		cmds[i] = resolveCmd(line)
	}

	for i, line := range lines {
		fmt.Printf("  [%d] PID: %s\n", i+1, getPID(line))
		parseInfo(cmds[i])
		fmt.Println()
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Which VM do you want to stop? (1-%d, or 'all'): ", len(lines))
	choiceRaw, _ := reader.ReadString('\n')
	choice := strings.TrimSpace(choiceRaw)

	if choice == "all" {
		for i, line := range lines {
			pid := getPID(line)
			fmt.Printf("  Stopping PID %s...", pid)
			if terminatePID(pid) == nil {
				fmt.Println(" OK")
			} else {
				fmt.Println(" FAIL (permission denied?)")
			}
			removeLocksFor(cmds[i])
			logEvent("VM stopped (PID " + pid + ") by user")
		}
		fmt.Println()
		return
	}

	index, err := strconv.Atoi(choice)
	if err != nil || index < 1 || index > len(lines) {
		fmt.Printf("  ! Enter a number between 1 and %d, or 'all'\n", len(lines))
		return
	}

	pid := getPID(lines[index-1])
	cmd := cmds[index-1]

	fmt.Printf("  PID %s — sending SIGTERM...", pid)
	if err := terminatePID(pid); err != nil {
		fmt.Println(" FAIL")
		fmt.Println("  Permission denied or PID no longer exists.")
		return
	}

	fmt.Println(" OK")
	removeLocksFor(cmd)
	logEvent("VM stopped (PID " + pid + ") by user")

	time.Sleep(2 * time.Second)

	if pidAlive(pid) {
		fmt.Print("  VM still running. Force kill (SIGKILL)? (y/N): ")
		forceRaw, _ := reader.ReadString('\n')
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(forceRaw)), "y") {
			if forceKillPID(pid) == nil {
				fmt.Println("  Force killed.")
				logEvent("VM force-killed (PID " + pid + ") by user")
			} else {
				fmt.Println("  FAIL.")
			}
		} else {
			fmt.Println("  Cancel — VM continues running.")
		}
	} else {
		fmt.Println("  VM successfully stopped.")
	}
	fmt.Println()
}

// =============================================================
//  MAIN
// =============================================================

func main() {
	if err := initPaths(); err != nil {
		fmt.Println("Failed to initialize paths:", err)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		printUsage()
		return
	}

	switch os.Args[1] {
	case "list":
		cmdList()
	case "status":
		cmdStatus()
	case "stop":
		cmdStop()
	case "debug":
		cmdDebug()
	default:
		printUsage()
	}
}

func printUsage() {
	fmt.Printf("\nUsage: %s <command>\n\n", filepath.Base(os.Args[0]))
	fmt.Println("  list    — list all running VMs (arch + ports)")
	fmt.Println("  status  — show PID, lock, pidfile info + log")
	fmt.Println("  stop    — stop a single or all VMs")
	fmt.Println()
}

// cmdDebug prints raw parsed fields and lock dir contents to diagnose
// lock detection issues without modifying any state.
func cmdDebug() {
	fmt.Println("\n====== Debug ======\n")
	fmt.Printf("  lockDir : %s\n", lockDir)
	fmt.Printf("  logFile : %s\n\n", logFile)

	entries, err := os.ReadDir(lockDir)
	if err != nil {
		fmt.Println("  lockDir read error:", err)
	} else if len(entries) == 0 {
		fmt.Println("  (no files in lockDir)")
	} else {
		fmt.Println("  Lock files present:")
		for _, e := range entries {
			fmt.Println("    ", e.Name())
		}
	}
	fmt.Println()

	procs := strings.TrimSpace(getProcs())
	if procs == "" {
		fmt.Println("  No running VMs found by getProcs().")
		return
	}

	for i, line := range strings.Split(procs, "\n") {
		pid := getPID(line)
		fmt.Printf("  --- VM %d  PID=%s ---\n", i+1, pid)
		fmt.Printf("  ps cmd (truncated): %s\n\n", getCmd(line))

		if full, ok := getFullCmd(pid); ok {
			fmt.Printf("  /proc cmd (full)  : %s\n\n", full)
			fmt.Printf("  getSSHPort   : %q\n", getSSHPort(full))
			fmt.Printf("  getVNCDisplay: %q\n", getVNCDisplay(full))
			fmt.Printf("  getSpicePort : %q\n", getSpicePort(full))
			fmt.Printf("  getExtraPorts: %v\n", getExtraPorts(full))
		} else {
			fmt.Println("  /proc read FAILED — falling back to ps cmd")
			fmt.Printf("  getSSHPort   : %q\n", getSSHPort(getCmd(line)))
		}
		fmt.Println()
	}
}
