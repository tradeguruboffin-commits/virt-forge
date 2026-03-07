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
	if len(fields) > 1 && isNumeric(fields[0]) {
		return strings.Join(fields[1:], " ")
	}
	return strings.TrimSpace(line)
}

func getFullCmd(pid string) (string, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil || len(data) == 0 {
		return "", false
	}
	return strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", " ")), true
}

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
//  ARGUMENT EXTRACTION
// =============================================================

var (
	reArch      = regexp.MustCompile(`qemu-system-([a-z0-9_]+)`)
	reDrive     = regexp.MustCompile(`file=([^, ]+)`)
	reHDA       = regexp.MustCompile(`-hda\s+(\S+)`)
	reRAM       = regexp.MustCompile(`-m\s+([0-9]+)`)
	reSSH       = regexp.MustCompile(`hostfwd=tcp::([0-9]+)-:22\b`)
	reAllFwd    = regexp.MustCompile(`hostfwd=tcp::([0-9]+)-:([0-9]+)`)
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

func getExtraPorts(cmd string) []string {
	var ports []string
	for _, m := range reAllFwd.FindAllStringSubmatch(cmd, -1) {
		if len(m) > 2 && m[2] != "22" {
			ports = append(ports, m[1])
		}
	}
	return ports
}

// getVNCPort extracts the VNC display number from QEMU args and converts
// it to a user-facing port (display N → port 5900+N).
// Lock files use this port: vnc_5909.lock, matching qemu-run's convention.
func getVNCPort(cmd string) string {
	if m := reVNCDisp.FindStringSubmatch(cmd); len(m) > 1 {
		display, err := strconv.Atoi(m[1])
		if err != nil {
			return ""
		}
		return strconv.Itoa(5900 + display)
	}
	return ""
}

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
	if v := getVNCPort(cmd); v != "" {
		fmt.Println("    VNC       : 127.0.0.1:" + v)
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
// VNC lock uses port (vnc_5909.lock), matching qemu-run's convention.
func removeLocksFor(cmd string) {
	if ssh := getSSHPort(cmd); ssh != "" {
		os.Remove(filepath.Join(lockDir, "ssh_"+ssh+".lock"))
		os.Remove(filepath.Join(lockDir, "qemu_"+ssh+".pid"))
	}
	if port := getVNCPort(cmd); port != "" {
		os.Remove(filepath.Join(lockDir, "vnc_"+port+".lock"))
	}
	if sp := getSpicePort(cmd); sp != "" {
		os.Remove(filepath.Join(lockDir, "spice_"+sp+".lock"))
	}
	for _, p := range getExtraPorts(cmd) {
		os.Remove(filepath.Join(lockDir, "extra_"+p+".lock"))
	}
}

// scanStaleOrphans scans lockDir for lock files whose PID is dead.
func scanStaleOrphans(activePorts map[string]bool) {
	entries, err := os.ReadDir(lockDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".lock") {
			continue
		}
		if activePorts[name] {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(lockDir, name))
		pidStr := strings.TrimSpace(string(data))
		if pidStr == "" || pidStr == "0" || !pidAlive(pidStr) {
			fmt.Printf("    ⚠ Orphan  : %s (PID %s dead — run: rm %s/%s)\n",
				name, pidStr, lockDir, name)
		}
	}
}

// showLocks displays lock file status for a running VM.
// VNC lock uses port (vnc_5909.lock), matching qemu-run's convention.
func showLocks(cmd string) {
	lockInfo := ""
	activePorts := map[string]bool{}

	if ssh := getSSHPort(cmd); ssh != "" {
		name := "ssh_" + ssh + ".lock"
		activePorts[name] = true
		if fileExists(filepath.Join(lockDir, name)) {
			lockInfo += " ssh:" + ssh
		}
	}
	if port := getVNCPort(cmd); port != "" {
		name := "vnc_" + port + ".lock"
		activePorts[name] = true
		if fileExists(filepath.Join(lockDir, name)) {
			lockInfo += " vnc:" + port
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
		cmd := resolveCmd(line)
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
		cmd := resolveCmd(line)

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
//  COMMAND: DEBUG
// =============================================================

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
			fmt.Printf("  getSSHPort  : %q\n", getSSHPort(full))
			fmt.Printf("  getVNCPort  : %q\n", getVNCPort(full))
			fmt.Printf("  getSpicePort: %q\n", getSpicePort(full))
			fmt.Printf("  getExtraPorts: %v\n", getExtraPorts(full))
		} else {
			fmt.Println("  /proc read FAILED — falling back to ps cmd")
			fmt.Printf("  getSSHPort  : %q\n", getSSHPort(getCmd(line)))
		}
		fmt.Println()
	}
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
	fmt.Println("  debug   — raw parsed fields + lock dir contents")
	fmt.Println()
}
