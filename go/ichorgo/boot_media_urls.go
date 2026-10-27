package ichorgo

import (
	"fmt"
	"strings"
)

// The links and commands that boot a new machine into Talos (S8 step 0), built from a Talos
// version and an Image Factory schematic without any network call. The phone cannot serve a
// PXE boot itself (UDP 67 and 69 are privileged ports), so it hands these to the person next
// to the machine: an ISO to write, an iPXE URL to chain, or the booter command that runs the
// proxyDHCP and TFTP server on a laptop of the same network.

const (
	factoryImageURL = "https://" + factoryHost
	factoryPXEURL   = "https://pxe." + factoryHost

	// defaultSchematic is the Image Factory's schematic without customization (no extension,
	// no extra kernel argument): the one of ghcr.io/siderolabs/installer.
	defaultSchematic = "376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba"

	// booterImage runs siderolabs/booter. Bump the tag when a new booter release keeps the flags
	// used below (--talos-version, --schematic-id, --secure-boot-enabled, -k); v0.3.0 and
	// v0.4.0 both take them.
	booterImage = "ghcr.io/siderolabs/booter:v0.4.0"

	bootArchAMD64 = "amd64"
	bootArchARM64 = "arm64"
)

// bootTarget is what the boot media are built for.
type bootTarget struct {
	version    string // vX.Y.Z
	schematic  string // 64 hex
	arch       string // amd64 or arm64
	secureBoot bool
}

// bootLinks are the Image Factory URLs of a target.
type bootLinks struct {
	ISO       string `json:"iso"`
	IPXE      string `json:"ipxe"`
	Kernel    string `json:"kernel"`
	Initramfs string `json:"initramfs"`
	Cmdline   string `json:"cmdline"`
}

// normalizeBootArch accepts "", amd64 or arm64 (and their usual aliases); "" stays "" so the
// caller can take the template member's.
func normalizeBootArch(arch string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "":
		return "", nil
	case "amd64", "x86_64", "x86-64":
		return bootArchAMD64, nil
	case "arm64", "aarch64":
		return bootArchARM64, nil
	}

	return "", fmt.Errorf("unknown architecture %q (amd64, arm64)", arch)
}

// bootMediaLinks builds the Image Factory URLs: the ISO, the iPXE script, and the kernel,
// initramfs and command line for a PXE server of one's own. Secure Boot media exist from
// Talos 1.5 on; the factory answers 404 for an older version.
func bootMediaLinks(t bootTarget) bootLinks {
	image := factoryImageURL + "/image/" + t.schematic + "/" + t.version + "/"
	suffix := ""

	if t.secureBoot {
		suffix = "-secureboot"
	}

	return bootLinks{
		ISO:       image + "metal-" + t.arch + suffix + ".iso",
		IPXE:      factoryPXEURL + "/pxe/" + t.schematic + "/" + t.version + "/metal-" + t.arch + suffix,
		Kernel:    image + "kernel-" + t.arch,
		Initramfs: image + "initramfs-" + t.arch + ".xz",
		Cmdline:   image + "cmdline-metal-" + t.arch,
	}
}

// booterCommand is the command that serves the PXE boot of t from a Linux machine of the new
// node's network, with runtime docker or podman. With kernelArgs (Omni's join arguments)
// booter makes its own schematic with them, so the schematic is not passed: the two flags
// exclude each other.
func booterCommand(runtime string, t bootTarget, kernelArgs string) string {
	parts := []string{"sudo", runtime, "run", "--rm", "--network", "host", booterImage, "--talos-version=" + t.version}

	if kernelArgs == "" {
		parts = append(parts, "--schematic-id="+t.schematic)
	}

	if t.secureBoot {
		parts = append(parts, "--secure-boot-enabled")
	}

	if kernelArgs != "" {
		parts = append(parts, "-k", shellQuote(kernelArgs))
	}

	return strings.Join(parts, " ")
}

// shellQuote quotes s for a POSIX shell: Omni's join URL holds '?' and '&'.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
