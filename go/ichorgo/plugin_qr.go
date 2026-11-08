package ichorgo

import (
	"errors"
	"net/url"
	"strings"
)

// A plugin travels by QR code as a link to its file, ichor://plugin?url=https://…, or as the
// file itself, split over several codes by `just qr-parts` when it is too large for one (the
// apps join the parts first). Either way the apps then show the consent sheet: a scanned
// plugin is never stored or enabled before the user reads what it does.

// pluginQRScheme starts a plugin link.
const pluginQRScheme = "ichor://plugin"

// errNotPluginQR is a code that holds neither a plugin link nor a plugin file.
var errNotPluginQR = errors.New("this QR code is not a plugin: scan a code made from a plugin file or an ichor://plugin link")

// pluginQRPayload is PluginQRPayload's answer: kind "url" with url, or "yaml" with yaml.
type pluginQRPayload struct {
	Kind string `json:"kind"`
	URL  string `json:"url,omitempty"`
	YAML string `json:"yaml,omitempty"`
}

// PluginQRPayload reads a scanned code: {"kind":"url","url":…} for a plugin link (the URL
// passes PluginFetchURL's checks: https with a host), {"kind":"yaml","yaml":…} for a plugin
// file (its first line that is not a comment is its schemaVersion), else an error. Nothing is
// fetched or stored: the URL goes to PluginFetchURL, the file to PluginUserCheck.
func PluginQRPayload(text string) (out string, err error) {
	// The answer is what the user scanned: only the error is masked.
	defer maskErr(&err)

	trimmed := strings.TrimSpace(text)

	if rest, ok := strings.CutPrefix(trimmed, pluginQRScheme); ok && (rest == "" || rest[0] == '?') {
		query, _ := url.ParseQuery(strings.TrimPrefix(rest, "?")) //nolint:errcheck // a bad query has no url
		u, err := pluginURL(query.Get("url"))
		if err != nil {
			return "", err
		}

		return toJSON(pluginQRPayload{Kind: "url", URL: u.String()})
	}

	if isPluginYAML(trimmed) {
		return toJSON(pluginQRPayload{Kind: "yaml", YAML: text})
	}

	return "", errNotPluginQR
}

// isPluginYAML is whether text's first line that is neither blank nor a comment declares the
// plugin schema version, as every plugin file starts.
func isPluginYAML(text string) bool {
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		return strings.HasPrefix(line, "schemaVersion:")
	}

	return false
}
