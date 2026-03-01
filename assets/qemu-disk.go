package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var imgBin string
var reader = bufio.NewReader(os.Stdin)

//
// =============================================================
// INIT
// =============================================================
//

func init() {
	path, err := exec.LookPath("qemu-img")
	if err != nil {
		fmt.Println("❌ qemu-img not found in PATH.")
		fmt.Println("   Install with: apt install qemu-utils (or equivalent)")
		os.Exit(1)
	}
	imgBin = path
}

//
// =============================================================
// HELPERS
// =============================================================
//

func usage() {
	fmt.Printf("\nUsage: %s <command>\n\n", os.Args[0])
	fmt.Println("Commands:")
	fmt.Println("  create    Create new QCOW2 image (interactive)")
	fmt.Println("  info      Show image info")
	fmt.Println("  resize    Resize existing image (interactive)")
	fmt.Println("  convert   Convert image format (interactive)")
	fmt.Println("\nSizes: 10G, 512M, etc.\n")
	os.Exit(1)
}

func prompt(text string) string {
	fmt.Print(text)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func confirm(text string) bool {
	fmt.Println(text)
	fmt.Print("Proceed? [y/N]: ")
	input, _ := reader.ReadString('\n')
	choice := strings.TrimSpace(input)

	switch strings.ToLower(choice) {
	case "y", "yes":
		return true
	default:
		fmt.Println("❌ Operation aborted.")
		return false
	}
}

func validateSize(size string) error {
	if size == "" {
		return errors.New("empty")
	}
	// [bash parity] bash uses: ''|*[!0-9GMKTgmkt]*)
	// Go regex is stricter (requires digit(s) then exactly one suffix) — intentional improvement
	valid := regexp.MustCompile(`^[0-9]+[GMKTgmkt]$`)
	if !valid.MatchString(size) {
		return fmt.Errorf("❌ Invalid size format: %s (use e.g. 10G, 512M)", size)
	}
	return nil
}

func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("❌ File not found: %s", path)
	}
	return nil
}

// runCommand runs qemu-img and exits on failure.
// [bash parity] bash uses set -e so any qemu-img failure exits immediately.
// Go must replicate this by calling os.Exit(1) on non-zero exit.
func runCommand(args ...string) {
	cmd := exec.Command(imgBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println("❌ Failed:", err)
		os.Exit(1)
	}
}

//
// =============================================================
// COMMANDS
// =============================================================
//

func cmdCreate() {
	img := prompt("Enter image name [default.qcow2]: ")
	if img == "" {
		img = "default.qcow2"
	}

	size := prompt("Enter image size [10G]: ")
	if size == "" {
		size = "10G"
	}

	if err := validateSize(size); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Printf("You are about to create image: %s (%s)\n", img, size)

	if confirm("Creating image...") {
		runCommand("create", "-f", "qcow2", img, size)
	}
}

func cmdInfo() {
	img := prompt("Enter image name to inspect: ")
	if img == "" {
		fmt.Println("❌ No image provided.")
		usage()
	}

	if err := requireFile(img); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	runCommand("info", img)
}

func cmdResize() {
	img := prompt("Enter image to resize: ")
	if img == "" {
		fmt.Println("❌ No image provided.")
		usage()
	}

	if err := requireFile(img); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	size := prompt("Enter new size (e.g., 20G, 512M): ")
	if size == "" {
		fmt.Println("❌ No size provided.")
		usage()
	}

	if err := validateSize(size); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Printf("You are about to resize image: %s → %s\n", img, size)

	if confirm("Resizing image...") {
		runCommand("resize", img, size)
	}
}

func cmdConvert() {
	src := prompt("Enter source image: ")
	if src == "" {
		fmt.Println("❌ No source image provided.")
		usage()
	}

	if err := requireFile(src); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	dst := prompt("Enter destination image: ")
	if dst == "" {
		fmt.Println("❌ No destination image provided.")
		usage()
	}

	fmtStr := prompt("Enter destination format (qcow2, raw, vmdk, etc.): ")
	if fmtStr == "" {
		fmt.Println("❌ No format provided.")
		usage()
	}

	allowed := map[string]bool{
		"qcow2":     true,
		"raw":       true,
		"vmdk":      true,
		"vdi":       true,
		"vpc":       true,
		"vhdx":      true,
		"qed":       true,
		"parallels": true,
	}

	if !allowed[fmtStr] {
		fmt.Printf("❌ Unknown format: %s\n", fmtStr)
		fmt.Println("   Known formats: qcow2, raw, vmdk, vdi, vpc, vhdx, qed, parallels")
		os.Exit(1)
	}

	fmt.Printf("You are about to convert: %s → %s (format: %s)\n", src, dst, fmtStr)

	if confirm("Converting image...") {
		runCommand("convert", "-O", fmtStr, src, dst)
	}
}

//
// =============================================================
// MAIN
// =============================================================
//

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	switch os.Args[1] {
	case "create":
		cmdCreate()
	case "info":
		cmdInfo()
	case "resize":
		cmdResize()
	case "convert":
		cmdConvert()
	default:
		usage()
	}
}
