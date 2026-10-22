package ichorgo

import (
	"encoding/json"
	"errors"
	"os"
)

// The imported plugins travel in the apps' encrypted backup (its "plugins" field, format 4):
// PluginUserExport gives each one's file and whether it is on, PluginUserRestore stores them
// on another phone. A restored file is checked as an import is (PluginUserCheck's rules): the
// user consented to it when importing it on the phone the backup came from, so it keeps its
// flag, and one this app no longer reads is reported, not stored.

// pluginUserExport is PluginUserExport's answer and PluginUserRestore's input.
type pluginUserExport struct {
	Plugins []pluginUserExportItem `json:"plugins"`
}

type pluginUserExportItem struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	YAML    string `json:"yaml"`
}

// pluginUserRestore is PluginUserRestore's answer: the ids stored, and the plugins left out.
type pluginUserRestore struct {
	Restored []string                 `json:"restored"`
	Errors   []pluginUserRestoreError `json:"errors"`
}

type pluginUserRestoreError struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// PluginUserExport is the imported plugins for the backup, sorted by id:
// {"plugins":[{id,enabled,yaml}]}. A plugin whose file cannot be read is left out.
func PluginUserExport() (out string, err error) {
	// The answer is the user's own files, for their backup: only the error is masked.
	defer maskErr(&err)

	userPlugins.Lock()
	defer userPlugins.Unlock()

	dir, aead := dataStore()
	export := pluginUserExport{Plugins: []pluginUserExportItem{}}

	for _, u := range loadUserPluginsLocked() {
		sealed, err := os.ReadFile(userPluginPath(dir, u.entry.ID))
		if err != nil {
			continue
		}

		src, err := openSealed(aead, sealed)
		if err != nil {
			continue
		}

		export.Plugins = append(export.Plugins, pluginUserExportItem{ID: u.entry.ID, Enabled: u.entry.Enabled, YAML: string(src)})
	}

	return toJSON(export)
}

// PluginUserRestore stores the plugins of a backup's "plugins" field (PluginUserExport's
// answer), each checked as an import is and kept on or off: {"restored":[id],
// "errors":[{id,error}]}. The same id again replaces the stored plugin.
func PluginUserRestore(exportJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var in pluginUserExport
	if err := json.Unmarshal([]byte(exportJSON), &in); err != nil {
		return "", errors.New("the backup's plugins cannot be read")
	}

	userPlugins.Lock()
	defer userPlugins.Unlock()

	result := pluginUserRestore{Restored: []string{}, Errors: []pluginUserRestoreError{}}

	for _, p := range in.Plugins {
		src := []byte(p.YAML)

		c, at, err := checkUserPluginLocked(src)
		if err == nil {
			_, err = storeUserPluginLocked(src, c, at, p.Enabled)
		}

		if err != nil {
			result.Errors = append(result.Errors, pluginUserRestoreError{ID: p.ID, Error: err.Error()})

			continue
		}

		result.Restored = append(result.Restored, c.Plugin.ID)
	}

	return toJSON(result)
}
