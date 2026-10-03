package ichorgo

import (
	"regexp"
	"strings"
)

const (
	defaultInstallerRepo = "ghcr.io/siderolabs/installer"
	factoryHost          = "factory.talos.dev"
)

var (
	talosVersionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?$`)
	schematicRe    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// UpgradeImage returns the installer image for version built like currentImage: same
// registry, repository and Image Factory schematic, with the tag replaced (a digest is
// dropped). "1.14.2" and "v1.14.2" both give ":v1.14.2". An empty currentImage means
// ghcr.io/siderolabs/installer. Returns "" when version is not a Talos version (vX.Y.Z or
// vX.Y.Z-suffix).
func UpgradeImage(currentImage, version string) (out string) {
	defer maskResult(&out, new(error))

	v, ok := normalizeTalosVersion(version)
	if !ok {
		return ""
	}

	repo, _ := splitImageRef(currentImage)
	if repo == "" {
		repo = defaultInstallerRepo
	}

	return repo + ":" + v
}

// normalizeTalosVersion validates a Talos version and returns it with its leading "v".
func normalizeTalosVersion(version string) (string, bool) {
	version = strings.TrimSpace(version)
	if !talosVersionRe.MatchString(version) {
		return "", false
	}

	return "v" + strings.TrimPrefix(version, "v"), true
}

// splitImageRef splits "registry[:port]/repo:tag@sha256:..." into the repository and the tag.
func splitImageRef(ref string) (repo, tag string) {
	ref = strings.TrimSpace(ref)

	if i := strings.IndexByte(ref, '@'); i >= 0 {
		ref = ref[:i]
	}

	slash := strings.LastIndexByte(ref, '/')
	if colon := strings.LastIndexByte(ref, ':'); colon > slash {
		return ref[:colon], ref[colon+1:]
	}

	return ref, ""
}

// imageSchematic returns the Image Factory schematic ID of an installer image
// (factory.talos.dev/<installer kind>/<schematic>:<tag>), or "".
func imageSchematic(ref string) string {
	repo, _ := splitImageRef(ref)

	parts := strings.Split(repo, "/")
	if len(parts) < 3 || parts[0] != factoryHost {
		return ""
	}

	if s := parts[len(parts)-1]; schematicRe.MatchString(s) {
		return s
	}

	return ""
}
