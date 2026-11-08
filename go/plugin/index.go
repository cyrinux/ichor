package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// The plugin index is a list of plugin files to pick from: plugins/index.json of this repository
// (`probe plugin-index`), or a company's own at another https URL. It is a list and nothing more:
// a file picked from it is downloaded, must have the digest the index published, and then goes
// through the same check and consent sheet as any file a person brings.

const (
	// IndexSchemaVersion is the format ParseIndex reads; another one is refused.
	IndexSchemaVersion = "ichor.dev/plugin-index/v1"
	// MaxIndexSize is the largest index a phone downloads.
	MaxIndexSize = 256 << 10
)

// Index is a plugin index file.
type Index struct {
	SchemaVersion string `json:"schemaVersion"`
	// GeneratedAt is when the list last changed (RFC 3339).
	GeneratedAt string       `json:"generatedAt"`
	Plugins     []IndexEntry `json:"plugins"`
}

// IndexEntry is a plugin of the index, as its file says.
type IndexEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Version is the plugin format the file is written in.
	Version string `json:"version"`
	// URL is where the file is, https; SHA256 is its digest, lowercase hex.
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	// Resources are what it reads: "sleepinfos.kube-green.com", "secrets".
	Resources []string `json:"resources"`
	// Icon is an app catalog id (a bundled icon), never an address.
	Icon string `json:"icon"`
	// Website is an https link, "" when the file has none.
	Website    string `json:"website"`
	Maintainer string `json:"maintainer"`
}

var (
	indexIDRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	indexIconRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	indexDigestRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// NewIndexEntry is the entry of the plugin file src, compiled as c and published at fileURL;
// maintainer stands in when the file does not name one.
func NewIndexEntry(c *Compiled, src []byte, fileURL, maintainer string) IndexEntry {
	p := c.Plugin

	resources := make([]string, 0, len(p.Resources))
	for _, r := range p.Resources {
		name := r.Resource
		if r.Group != "" {
			name += "." + r.Group
		}

		resources = append(resources, name)
	}

	slices.Sort(resources)

	entry := IndexEntry{
		ID: p.ID, Name: p.Name, Description: p.Description, Version: p.SchemaVersion, URL: fileURL, SHA256: SHA256Hex(src),
		Resources: slices.Compact(resources), Icon: p.Icon, Website: p.Website, Maintainer: p.Maintainer,
	}
	if entry.Maintainer == "" {
		entry.Maintainer = maintainer
	}

	return entry
}

// ParseIndex reads an index from the network. An entry that cannot be downloaded safely (no
// https URL, no digest) or stored (an id that cannot name a file) refuses the whole index, by
// name: it is somebody's generator that is wrong, not something to hide. What the apps would only
// show or follow is cleaned instead: a website that is not https and an icon that is not a
// bundled icon's name are dropped.
func ParseIndex(data []byte) (*Index, error) {
	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, errors.New("this is not a plugin index")
	}

	if index.SchemaVersion != IndexSchemaVersion {
		return nil, fmt.Errorf("this plugin index has a format this app does not read (%q)", index.SchemaVersion)
	}

	seen := map[string]bool{}
	plugins := make([]IndexEntry, 0, len(index.Plugins))

	for _, e := range index.Plugins {
		clean, err := cleanIndexEntry(e)
		if err != nil {
			return nil, err
		}

		if seen[clean.ID] {
			return nil, fmt.Errorf("the plugin index lists %q twice", clean.ID)
		}

		seen[clean.ID] = true
		plugins = append(plugins, clean)
	}

	index.Plugins = plugins

	return &index, nil
}

func cleanIndexEntry(e IndexEntry) (IndexEntry, error) {
	if !indexIDRe.MatchString(e.ID) {
		return e, fmt.Errorf("the plugin index has an invalid id %q", e.ID)
	}

	if !isHTTPSURL(e.URL) {
		return e, fmt.Errorf("the plugin index gives %q no https URL", e.ID)
	}

	if !indexDigestRe.MatchString(e.SHA256) {
		return e, fmt.Errorf("the plugin index gives %q no sha256 digest", e.ID)
	}

	if !isHTTPSURL(e.Website) {
		e.Website = ""
	}

	if !indexIconRe.MatchString(e.Icon) {
		e.Icon = ""
	}

	if e.Resources == nil {
		e.Resources = []string{}
	}

	return e, nil
}

func isHTTPSURL(raw string) bool {
	u, err := url.Parse(raw)

	return err == nil && u.Scheme == "https" && u.Host != ""
}

// NormalizeDigest is a sha256 digest as an index holds it, ok false when it is not one.
func NormalizeDigest(digest string) (string, bool) {
	d := strings.ToLower(strings.TrimSpace(digest))

	return d, indexDigestRe.MatchString(d)
}

// SHA256Hex is the digest an index publishes for a file.
func SHA256Hex(src []byte) string {
	sum := sha256.Sum256(src)

	return hex.EncodeToString(sum[:])
}
