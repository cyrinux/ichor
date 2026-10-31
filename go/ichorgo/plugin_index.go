package ichorgo

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The plugin index (Settings → Plugins → Browse the catalog): a published list of plugin files,
// not to be confused with PluginCatalog, the plugins installed here. The index only says what can
// be picked: PluginIndexDownload hands back a file with the digest the index published, and that
// file then goes through PluginUserCheck, the consent sheet and PluginUserImport like any other.

const (
	defaultPluginIndexURL = "https://raw.githubusercontent.com/cyrinux/ichor/main/plugins/index.json"
	pluginIndexTTL        = time.Hour
)

// pluginIndexAnswer is PluginIndexFetch's answer; URL is the index that was read.
type pluginIndexAnswer struct {
	URL         string            `json:"url"`
	GeneratedAt string            `json:"generatedAt"`
	Plugins     []pluginIndexItem `json:"plugins"`
}

// pluginIndexItem is an entry with what this phone already has: an imported plugin with its id
// (installed), or a built-in with its id, which cannot be imported (builtin).
type pluginIndexItem struct {
	plugin.IndexEntry

	Installed bool `json:"installed"`
	Builtin   bool `json:"builtin"`
}

// pluginIndexCache is the last index read, kept an hour: raw GitHub limits by address.
var pluginIndexCache struct {
	sync.Mutex
	url   string
	at    time.Time
	index *plugin.Index
}

func forgetPluginIndex() {
	pluginIndexCache.Lock()
	defer pluginIndexCache.Unlock()

	pluginIndexCache.index = nil
}

// PluginIndexFetch reads the plugin index at rawURL ("" for the project's): https only (redirects
// too), at most 256 KiB, kept an hour. The answer is {url,generatedAt,plugins:[{id,name,
// description,version,url,sha256,resources,icon,website,maintainer,installed,builtin}]}.
func PluginIndexFetch(rawURL string) (out string, err error) {
	// The answer holds addresses and digests, which masking a word would corrupt, about public
	// plugin files: only the error is masked.
	defer maskErr(&err)

	address := strings.TrimSpace(rawURL)
	if address == "" {
		address = defaultPluginIndexURL
	}

	index, err := loadPluginIndex(address)
	if err != nil {
		return "", err
	}

	userPlugins.Lock()
	stored := loadUserPluginsLocked()
	userPlugins.Unlock()

	installed := make(map[string]bool, len(stored))
	for _, u := range stored {
		installed[u.entry.ID] = true
	}

	builtin := builtinIDs()
	answer := pluginIndexAnswer{URL: address, GeneratedAt: index.GeneratedAt, Plugins: make([]pluginIndexItem, 0, len(index.Plugins))}

	for _, e := range index.Plugins {
		answer.Plugins = append(answer.Plugins, pluginIndexItem{IndexEntry: e, Installed: installed[e.ID], Builtin: builtin[e.ID]})
	}

	return toJSON(answer)
}

// loadPluginIndex is the index at address, from the cache within the hour. A failure is not kept.
func loadPluginIndex(address string) (*plugin.Index, error) {
	pluginIndexCache.Lock()
	cached, url, at := pluginIndexCache.index, pluginIndexCache.url, pluginIndexCache.at
	pluginIndexCache.Unlock()

	now := pluginNow()
	if cached != nil && url == address && now.Sub(at) < pluginIndexTTL {
		return cached, nil
	}

	data, err := fetchHTTPS(address, plugin.MaxIndexSize, "the plugin index is larger than 256 KiB")
	if err != nil {
		return nil, err
	}

	index, err := plugin.ParseIndex(data)
	if err != nil {
		return nil, err
	}

	pluginIndexCache.Lock()
	pluginIndexCache.index, pluginIndexCache.url, pluginIndexCache.at = index, address, now
	pluginIndexCache.Unlock()

	return index, nil
}

// PluginIndexDownload downloads a plugin file an index lists (as PluginFetchURL does: https only,
// 1 MiB, 20 s) and answers it only when it has the digest the index published. Nothing is stored:
// the file goes to PluginUserCheck and the consent sheet, whose Enable calls PluginUserImport. A
// mismatch also forgets the cached index, so that the next read has the new digest.
func PluginIndexDownload(rawURL, sha256 string) (out string, err error) {
	// The answer is a public plugin file, as PluginFetchURL's: only the error is masked.
	defer maskErr(&err)

	want, ok := plugin.NormalizeDigest(sha256)
	if !ok {
		return "", errors.New("the plugin index gives this plugin no sha256 digest")
	}

	data, err := fetchHTTPS(rawURL, plugin.MaxSize, "the file is larger than 1 MiB")
	if err != nil {
		return "", err
	}

	if plugin.SHA256Hex(data) != want {
		forgetPluginIndex()

		return "", errors.New("the file changed since the index was published")
	}

	return string(data), nil
}
