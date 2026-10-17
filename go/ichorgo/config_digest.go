package ichorgo

import (
	"crypto/rand"
	"encoding/binary"
)

// The apps keep the summaries of their stored configs next to the stores, so that a launch
// shows the home without parsing every context. They key that cache by the digest of each
// text and by the mask generation: the summary is a function of the text alone while the
// mask is off, and of the text and the mask's mapping while it is on.

// ConfigDigest is the content hash of a talosconfig or kubeconfig text, the key of the Go
// config cache (config_cache.go): the hex SHA-256 of the text. Not sensitive.
func ConfigDigest(configYAML string) string {
	if p := configs.peek(configYAML); p != nil {
		return p.hash
	}

	return contentHash(configYAML)
}

// ParseConfigIfChanged is "" when knownDigest is the digest of configYAML (nothing parsed:
// the caller's summary is current), ParseConfig's output otherwise.
func ParseConfigIfChanged(configYAML, knownDigest string) (out string, err error) {
	defer maskResult(&out, &err)

	if knownDigest != "" && ConfigDigest(configYAML) == knownDigest {
		return "", nil
	}

	return parseConfig(configYAML)
}

// ParseKubeconfigIfChanged is ParseConfigIfChanged for ParseKubeconfig.
func ParseKubeconfigIfChanged(configYAML, knownDigest string) (out string, err error) {
	defer maskResult(&out, &err)

	if knownDigest != "" && ConfigDigest(configYAML) == knownDigest {
		return "", nil
	}

	return kubeconfigSummaryJSON(configYAML)
}

// maskEpoch is drawn once per process, so that a generation never repeats across launches:
// the mask's mapping lives in memory only, and a summary masked by an earlier process uses
// fakes this one does not know.
var maskEpoch = newMaskEpoch()

func newMaskEpoch() int {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 1
	}

	// 1 ... 2^30, room left below the 32-bit int limit for the resets.
	return int(binary.BigEndian.Uint32(b[:])>>2) + 1
}

// PrivacyGeneration identifies what ParseConfig's masking does: 0 while screenshot mode is
// off (the output is then the text's alone, the same in every launch), otherwise a value
// that changes each time the mapping is forgotten (screenshot mode switched, its words
// changed) and differs between launches. A summary cached under another generation is stale.
func PrivacyGeneration() int {
	enabled, resets := privacy.state()
	if !enabled {
		return 0
	}

	return maskEpoch + resets
}
