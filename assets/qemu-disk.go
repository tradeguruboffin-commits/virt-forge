package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var imgBin string

// =============================================================
//  USAGE
// =============================================================

const usageText = `qemu-disk — QCOW2 image manager

Usage:
  qemu-disk create  --name <file> --size <size>
  qemu-disk info    --name <file>
  qemu-disk resize  --name <file> --size <size>
  qemu-disk convert --src <file>  --dst <file> --fmt <format>

Size format: number followed by K, M, G, or T  (e.g. 10G, 512M, 2T)

Formats for convert: qcow2, raw, vmdk, vdi, vpc, vhdx, qed, parallels

Examples:
  qemu-disk create  --name debian.qcow2 --size 20G
  qemu-disk info    --name debian.qcow2
  qemu-disk resize  --name debian.qcow2 --size 30G
  qemu-disk convert --src debian.qcow2 --dst debian.raw --fmt raw
`

func usage() {
	fmt.Fprint(os.Stderr, usageText)
	os.Exit(1)
}

func usageCmd(cmd string) {
	switch cmd {
	case "create":
		fmt.Fprintln(os.Stderr, "   Usage: qemu-disk create --name <file> --size <size>")
	case "info":
		fmt.Fprintln(os.Stderr, "   Usage: qemu-disk info --name <file>")
	case "resize":
		fmt.Fprintln(os.Stderr, "   Usage: qemu-disk resize --name <file> --size <size>")
	case "convert":
		fmt.Fprintln(os.Stderr, "   Usage: qemu-disk convert --src <file> --dst <file> --fmt <format>")
	}
}

// =============================================================
//  INIT
// =============================================================

func init() {
	path, err := exec.LookPath("qemu-img")
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌ qemu-img not found in PATH.")
		fmt.Fprintln(os.Stderr, "   Install with: apt install qemu-utils (or equivalent)")
		os.Exit(1)
	}
	imgBin = path
}

// =============================================================
//  MINIMAL FLAG PARSER
//  Supports: --key value   --key=value
// =============================================================

type flags map[string]string

func parseFlags(args []string) flags {
	f := make(flags)
	i := 0
	for i < len(args) {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			i++
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		if idx := strings.IndexByte(key, '='); idx >= 0 {
			f[key[:idx]] = key[idx+1:]
			i++
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			f[key] = args[i+1]
			i += 2
		} else {
			f[key] = ""
			i++
		}
	}
	return f
}

func (f flags) require(cmd, key string) (string, error) {
	v, ok := f[key]
	if !ok || strings.TrimSpace(v) == "" {
		usageCmd(cmd)
		return "", fmt.Errorf("--%s is required", key)
	}
	return strings.TrimSpace(v), nil
}

// =============================================================
//  VALIDATION
// =============================================================

func validateSize(size string) error {
	if size == "" {
		return errors.New("size cannot be empty")
	}
	valid := regexp.MustCompile(`^[0-9]+[GMKTgmkt]$`)
	if !valid.MatchString(size) {
		return fmt.Errorf("invalid size format: %q  (use e.g. 10G, 512M, 2T)", size)
	}
	return nil
}

func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("file not found: %s", path)
	}
	return nil
}

var allowedFormats = map[string]bool{
	"qcow2":     true,
	"raw":       true,
	"vmdk":      true,
	"vdi":       true,
	"vpc":       true,
	"vhdx":      true,
	"qed":       true,
	"parallels": true,
}

func validateFormat(fmt_ string) error {
	if !allowedFormats[fmt_] {
		return fmt.Errorf("unknown format: %q\n   Known: qcow2, raw, vmdk, vdi, vpc, vhdx, qed, parallels", fmt_)
	}
	return nil
}

// =============================================================
//  RUNNER
// =============================================================

func run(args ...string) {
	cmd := exec.Command(imgBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "❌ qemu-img failed:", err)
		os.Exit(1)
	}
}

// =============================================================
//  COMMANDS
// =============================================================

func cmdCreate(f flags) {
	name, err := f.require("create", "name")
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	size, err := f.require("create", "size")
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	if err := validateSize(size); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	fmt.Printf("Creating %s (%s)…\n", name, size)
	run("create", "-f", "qcow2", name, size)
	fmt.Println("✅ Done.")
}

func cmdInfo(f flags) {
	name, err := f.require("info", "name")
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	if err := requireFile(name); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	run("info", name)
}

func cmdResize(f flags) {
	name, err := f.require("resize", "name")
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	size, err := f.require("resize", "size")
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	if err := requireFile(name); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	if err := validateSize(size); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	fmt.Printf("Resizing %s → %s…\n", name, size)
	run("resize", name, size)
	fmt.Println("✅ Done.")
}

func cmdConvert(f flags) {
	src, err := f.require("convert", "src")
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	dst, err := f.require("convert", "dst")
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	fmt_, err := f.require("convert", "fmt")
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	if err := requireFile(src); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	if err := validateFormat(fmt_); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	fmt.Printf("Converting %s → %s (format: %s)…\n", src, dst, fmt_)
	run("convert", "-O", fmt_, src, dst)
	fmt.Println("✅ Done.")
}

// =============================================================
//  MAIN
// =============================================================

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	cmd := os.Args[1]
	f := parseFlags(os.Args[2:])

	switch cmd {
	case "create":
		cmdCreate(f)
	case "info":
		cmdInfo(f)
	case "resize":
		cmdResize(f)
	case "convert":
		cmdConvert(f)
	case "--help", "-h", "help":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "❌ Unknown command: %q\n\n", cmd)
		usage()
	}
}
