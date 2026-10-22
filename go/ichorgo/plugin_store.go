package ichorgo

import (
	"cmp"
	"context"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/internal/httpx"
	"github.com/cyrinux/ichor/go/plugin"
)

// User plugins: a plugin file the user brings (pasted, picked, downloaded) is checked first
// (PluginUserCheck: nothing is stored) and shown on the consent sheet, what it reads and can
// do; only its Enable imports it (PluginUserImport), on. It is kept on the phone in the app's
// data directory (see SetDataDir), sealed like the scan reports: one file per plugin, named by
// its id, and an index of their sha256 and whether each is on. So every stored plugin was
// consented to as it is: turning one back on needs no new consent. Every launch re-validates
// the stored files: one this app no longer reads (another schemaVersion, a built-in that took
// its id, a file changed on disk) is listed off with the reason. Enabled ones join the
// built-ins in every cluster call (knownPlugins).

const (
	pluginStoreDir  = "plugins"
	pluginIndexFile = "plugins.json"
	maxUserPlugins  = 64
	pluginFetchTime = 20 * time.Second
)

// userPluginIDRe is what a stored id may be (it names a file): stricter than the validator's
// [a-z0-9-]+, checked before anything is stored.
var userPluginIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// userPluginEntry is the index line of a stored plugin.
type userPluginEntry struct {
	ID         string `json:"id"`
	SHA256     string `json:"sha256"`
	Enabled    bool   `json:"enabled"`
	ImportedAt int64  `json:"importedAt"`
}

// userPlugin is a stored plugin as loaded: compiled, or the reason it is not.
type userPlugin struct {
	entry    userPluginEntry
	compiled *plugin.Compiled
	problem  string
}

// pluginUserList is PluginUserList's answer.
type pluginUserList struct {
	Items []pluginUserItem `json:"items"`
}

// pluginUserItem is a user plugin's row and consent entry: its catalog item, whether it is on,
// and why it cannot be when it is broken.
type pluginUserItem struct {
	pluginCatalogItem

	Enabled    bool   `json:"enabled"`
	Error      string `json:"error"`
	SHA256     string `json:"sha256"`
	ImportedAt int64  `json:"importedAt"`
	// Replaces is PluginUserCheck's: an imported plugin has this id, and importing replaces it.
	Replaces bool `json:"replaces,omitempty"`
}

// userPlugins is the store as last read, for the data directory and key it was read with
// (SetDataDir makes a new cipher: the store is read again).
var userPlugins struct {
	sync.Mutex
	dir    string
	aead   cipher.AEAD
	loaded bool
	items  []userPlugin
}

// PluginUserCheck validates a plugin file without storing it: the answer is its consent
// entry {id,name,…,resources:[{group,resource,kind,reveal}],actions:[{id,label,verb,rbac}],
// replaces}, replaces when an imported plugin has its id. A file that does not load or
// validate is an error of file:line lines; an id a built-in uses, an id the store cannot name a
// file by, a 65th plugin and a phone without a data directory are refused.
func PluginUserCheck(yaml string) (out string, err error) {
	defer maskResult(&out, &err)

	userPlugins.Lock()
	defer userPlugins.Unlock()

	c, at, err := checkUserPluginLocked([]byte(yaml))
	if err != nil {
		return "", err
	}

	item := userPlugin{compiled: c, entry: userPluginEntry{ID: c.Plugin.ID}}.item()
	item.Replaces = at >= 0

	return toJSON(item)
}

// PluginUserImport stores a plugin file, on: the consent sheet's Enable, after PluginUserCheck
// showed it. It is checked again the same way. The same id again replaces the stored file.
func PluginUserImport(yaml string) (out string, err error) {
	defer maskResult(&out, &err)

	src := []byte(yaml)

	userPlugins.Lock()
	defer userPlugins.Unlock()

	c, at, err := checkUserPluginLocked(src)
	if err != nil {
		return "", err
	}

	stored, err := storeUserPluginLocked(src, c, at, true)
	if err != nil {
		return "", err
	}

	return toJSON(stored.item())
}

// storeUserPluginLocked writes src, checked by checkUserPluginLocked as c replacing the stored
// plugin at index at (-1 for none), and its index line with enabled.
func storeUserPluginLocked(src []byte, c *plugin.Compiled, at int, enabled bool) (userPlugin, error) {
	dir, aead := dataStore()
	id := c.Plugin.ID

	if err := os.MkdirAll(filepath.Join(dir, pluginStoreDir), 0o700); err != nil {
		return userPlugin{}, err
	}

	// The new file goes in place only once the index names its sha256: a failed write keeps
	// the stored plugin as it was.
	pending := userPluginPath(dir, id) + ".new"
	if err := writeSealed(pending, src, aead); err != nil {
		return userPlugin{}, err
	}
	defer os.Remove(pending) //nolint:errcheck // gone once renamed

	items := loadUserPluginsLocked()
	stored := userPlugin{entry: userPluginEntry{ID: id, SHA256: sha256Hex(src), Enabled: enabled, ImportedAt: pluginNow().UnixMilli()}, compiled: c}

	next := slices.Clone(items)
	if at < 0 {
		next = append(next, stored)
	} else {
		next[at] = stored
	}

	if err := writeUserPluginsLocked(next); err != nil {
		return userPlugin{}, err
	}

	if err := os.Rename(pending, userPluginPath(dir, id)); err != nil {
		return userPlugin{}, err
	}

	return stored, nil
}

// checkUserPluginLocked compiles src and checks it can be stored: the compiled plugin and the
// index of the stored plugin it replaces (-1 for none).
func checkUserPluginLocked(src []byte) (*plugin.Compiled, int, error) {
	c, err := compileUserPlugin(src)
	if err != nil {
		return nil, 0, err
	}

	id := c.Plugin.ID
	if builtinIDs()[id] {
		return nil, 0, fmt.Errorf("the id %q is a built-in plugin's: give the plugin another id", id)
	}

	if !userPluginIDRe.MatchString(id) {
		return nil, 0, fmt.Errorf("the id %q cannot be imported: at most 63 lowercase letters, digits and dashes, starting with a letter or a digit", id)
	}

	if _, aead := dataStore(); aead == nil {
		return nil, 0, errors.New("this phone has no storage for plugins")
	}

	items := loadUserPluginsLocked()

	at := slices.IndexFunc(items, func(u userPlugin) bool { return u.entry.ID == id })
	if at < 0 && len(items) >= maxUserPlugins {
		return nil, 0, fmt.Errorf("at most %d plugins can be imported: remove one first", maxUserPlugins)
	}

	return c, at, nil
}

// PluginUserList lists the imported plugins, sorted by id: {"items":[…]} as PluginUserImport
// answers each one, error saying why a stored plugin cannot be enabled.
func PluginUserList() (out string, err error) {
	defer maskResult(&out, &err)

	userPlugins.Lock()
	items := loadUserPluginsLocked()
	userPlugins.Unlock()

	list := pluginUserList{Items: make([]pluginUserItem, 0, len(items))}
	for _, u := range items {
		list.Items = append(list.Items, u.item())
	}

	return toJSON(list)
}

// PluginUserSetEnabled turns an imported plugin on or off; a broken one cannot be turned on.
// Every stored plugin was consented to as stored (see PluginUserImport).
func PluginUserSetEnabled(id string, enabled bool) (err error) {
	defer maskErr(&err)

	return updateUserPlugin(id, func(items []userPlugin, at int) ([]userPlugin, error) {
		if enabled && items[at].compiled == nil {
			return nil, fmt.Errorf("plugin %q cannot be enabled: %s", id, items[at].problem)
		}

		next := slices.Clone(items)
		next[at].entry.Enabled = enabled

		return next, nil
	})
}

// PluginUserRemove deletes an imported plugin and its file.
func PluginUserRemove(id string) (err error) {
	defer maskErr(&err)

	return updateUserPlugin(id, func(items []userPlugin, at int) ([]userPlugin, error) {
		return slices.Delete(slices.Clone(items), at, at+1), nil
	})
}

// PluginFetchURL downloads a plugin file for the import screen: https only (redirects too),
// at most 1 MiB, within 20 s. The answer is the file as is; PluginUserImport validates it.
func PluginFetchURL(rawURL string) (out string, err error) {
	// The answer is the user's own file: only the error is masked.
	defer maskErr(&err)

	u, err := pluginURL(rawURL)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), pluginFetchTime)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}

	client := *pluginFetchClient()
	client.CheckRedirect = httpx.HTTPSRedirectsOnly

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, httpx.ErrInsecureRedirect) {
			return "", errors.New("the URL redirects to plain http: only https:// URLs can be downloaded")
		}

		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the server answered %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, plugin.MaxSize+1))
	if err != nil {
		return "", err
	}

	if len(data) > plugin.MaxSize {
		return "", errors.New("the file is larger than 1 MiB")
	}

	return string(data), nil
}

// pluginURL is a plugin file's URL as PluginFetchURL downloads it: https with a host.
func pluginURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("only https:// URLs can be downloaded")
	}

	return u, nil
}

func defaultPluginFetchClient() *http.Client {
	return httpx.New(httpx.Opts{Timeout: pluginFetchTime, FollowRedirects: true})
}

// pluginFetchClient is PluginFetchURL's client; the tests give a TLS test server's.
var pluginFetchClient = defaultPluginFetchClient

// installedPlugins are the built-ins, then the enabled user plugins sorted by id.
func installedPlugins() []*plugin.Compiled {
	return slices.Concat(plugin.Builtins(), enabledUserPlugins())
}

func enabledUserPlugins() []*plugin.Compiled {
	userPlugins.Lock()
	defer userPlugins.Unlock()

	var out []*plugin.Compiled

	for _, u := range loadUserPluginsLocked() {
		if u.entry.Enabled && u.compiled != nil {
			out = append(out, u.compiled)
		}
	}

	return out
}

// updateUserPlugin applies change to the stored plugins, id at index at, and writes them.
func updateUserPlugin(id string, change func(items []userPlugin, at int) ([]userPlugin, error)) error {
	if !userPluginIDRe.MatchString(id) {
		return fmt.Errorf("invalid plugin id %q", id)
	}

	userPlugins.Lock()
	defer userPlugins.Unlock()

	items := loadUserPluginsLocked()

	at := slices.IndexFunc(items, func(u userPlugin) bool { return u.entry.ID == id })
	if at < 0 {
		return fmt.Errorf("no imported plugin %q", id)
	}

	next, err := change(items, at)
	if err != nil {
		return err
	}

	return writeUserPluginsLocked(next)
}

// loadUserPluginsLocked is the store, read (and every file re-validated) once per data
// directory. Nothing is stored without one.
func loadUserPluginsLocked() []userPlugin {
	dir, aead := dataStore()
	if aead == nil {
		return nil
	}

	if userPlugins.loaded && userPlugins.dir == dir && userPlugins.aead == aead {
		return userPlugins.items
	}

	builtin := builtinIDs()

	var items []userPlugin

	for _, e := range readUserPluginIndex(dir, aead) {
		items = append(items, loadUserPlugin(dir, aead, e, builtin))
	}

	userPlugins.dir, userPlugins.aead, userPlugins.loaded, userPlugins.items = dir, aead, true, items

	return items
}

// loadUserPlugin reads and compiles a stored plugin; what keeps it off is its problem.
func loadUserPlugin(dir string, aead cipher.AEAD, e userPluginEntry, builtin map[string]bool) userPlugin {
	u := userPlugin{entry: e}

	sealed, err := os.ReadFile(userPluginPath(dir, e.ID))
	if err != nil {
		u.problem = "its file is missing"

		return u
	}

	src, err := openSealed(aead, sealed)
	if err != nil {
		u.problem = "its file cannot be read"

		return u
	}

	if sha256Hex(src) != e.SHA256 {
		u.problem = "its file changed since it was imported: import it again"

		return u
	}

	if u.problem = userPluginProblem(src, builtin); u.problem == "" {
		u.compiled, _ = compileUserPlugin(src) //nolint:errcheck // no problem: it compiles
	}

	return u
}

// userPluginProblem is why a user plugin's source cannot run here, "" when it can.
func userPluginProblem(src []byte, builtin map[string]bool) string {
	c, err := compileUserPlugin(src)
	if err != nil {
		return err.Error()
	}

	if builtin[c.Plugin.ID] {
		return fmt.Sprintf("a built-in plugin now has the id %q", c.Plugin.ID)
	}

	return ""
}

// compileUserPlugin loads, validates and compiles a user's file; a panic of hostile input is
// an error.
func compileUserPlugin(src []byte) (c *plugin.Compiled, err error) {
	if len(src) > plugin.MaxSize {
		return nil, errors.New("plugin.yaml: the file is larger than 1 MiB")
	}

	err = safeCall(func() error {
		c, err = plugin.CompileSource("plugin.yaml", src)

		return err
	})

	return c, err
}

func readUserPluginIndex(dir string, aead cipher.AEAD) []userPluginEntry {
	data, err := os.ReadFile(filepath.Join(dir, pluginIndexFile))
	if err != nil {
		return nil
	}

	plain, err := openSealed(aead, data)
	if err != nil {
		return nil
	}

	var index []userPluginEntry
	if json.Unmarshal(plain, &index) != nil {
		return nil
	}

	return slices.DeleteFunc(index, func(e userPluginEntry) bool { return !userPluginIDRe.MatchString(e.ID) })
}

// writeUserPluginsLocked writes the index of items, sorted by id, deletes the plugin files no
// entry names, and keeps items as the store. The installed plugins changed: the can-I answers
// are asked again, with the actions of the plugins now on.
func writeUserPluginsLocked(items []userPlugin) error {
	dir, aead := dataStore()
	if aead == nil {
		return errors.New("this phone has no storage for plugins")
	}

	sorted := slices.SortedFunc(slices.Values(items), func(a, b userPlugin) int { return cmp.Compare(a.entry.ID, b.entry.ID) })

	index := make([]userPluginEntry, len(sorted))
	for i, u := range sorted {
		index[i] = u.entry
	}

	data, err := json.Marshal(index)
	if err != nil {
		return err
	}

	if err := writeSealed(filepath.Join(dir, pluginIndexFile), data, aead); err != nil {
		return err
	}

	userPlugins.dir, userPlugins.aead, userPlugins.loaded, userPlugins.items = dir, aead, true, sorted
	actionAccess.clear()

	files, _ := os.ReadDir(filepath.Join(dir, pluginStoreDir))
	for _, f := range files {
		id, ok := strings.CutSuffix(f.Name(), ".yaml")
		if ok && !slices.ContainsFunc(index, func(e userPluginEntry) bool { return e.ID == id }) {
			_ = os.Remove(filepath.Join(dir, pluginStoreDir, f.Name()))
		}
	}

	return nil
}

// item is the plugin's row: its catalog item when it compiles, else its id and the problem.
func (u userPlugin) item() pluginUserItem {
	out := pluginUserItem{Enabled: u.entry.Enabled && u.compiled != nil, Error: u.problem, SHA256: u.entry.SHA256, ImportedAt: u.entry.ImportedAt}

	if u.compiled != nil {
		out.pluginCatalogItem = pluginCatalogItemOf(u.compiled)
	} else {
		out.pluginCatalogItem = pluginCatalogItem{ID: u.entry.ID, Name: u.entry.ID, Resources: []pluginCatalogResource{}, Actions: []pluginCatalogAction{}}
	}

	out.User = true

	return out
}

func builtinIDs() map[string]bool {
	out := map[string]bool{}
	for _, c := range plugin.Builtins() {
		out[c.Plugin.ID] = true
	}

	return out
}

func userPluginPath(dir, id string) string {
	return filepath.Join(dir, pluginStoreDir, id+".yaml")
}
