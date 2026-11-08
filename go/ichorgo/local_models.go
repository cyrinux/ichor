package ichorgo

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// The models the app offers to download for the "device" provider. The list ships with
// the app (local_models.json), each file pinned by repository revision, size and SHA-256,
// so the bytes a phone downloads are the ones the release named; scripts/local-models.py
// refreshes the pins. An entry without pins is kept in the file but not offered. A model
// file the user imports is not in the list: its id is "file:" and the file's name.

const (
	defaultLocalContext = 8192
	defaultLocalAnswer  = 1024

	localModelFilePrefix = "file:"
)

//go:embed local_models.json
var localModelsJSON []byte

// localModelEntry is one downloadable model.
type localModelEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Recommended bool   `json:"recommended,omitempty"`
	// Repo, Revision and File make the download URL; Revision is a commit hash.
	Repo     string `json:"repo"`
	Revision string `json:"revision"`
	File     string `json:"file"`
	URL      string `json:"url,omitempty"` // filled in by LocalModelCatalog
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
	License  string `json:"license"`
	// LicenseURL is shown next to the model before it is downloaded.
	LicenseURL string `json:"licenseUrl"`
	// The phone's memory below which the model is not offered (Android totalMem, iOS
	// physicalMemory: iOS gives an app about half of it).
	MinMemoryMiB    int64 `json:"minMemoryMiB"`
	MinMemoryMiBIOS int64 `json:"minMemoryMiBIOS"`
	ContextTokens   int   `json:"contextTokens"`
	// AssistantPrefix is written after the chat template's assistant turn: for Qwen, an
	// empty reasoning block, so the model answers at once instead of thinking for minutes.
	AssistantPrefix string `json:"assistantPrefix,omitempty"`
}

func (m localModelEntry) pinned() bool {
	return len(m.Revision) == 40 && len(m.SHA256) == 64 && m.Bytes > 0
}

func (m localModelEntry) url() string {
	return "https://huggingface.co/" + m.Repo + "/resolve/" + m.Revision + "/" + m.File
}

var loadLocalModels = sync.OnceValues(func() ([]localModelEntry, error) {
	var all []localModelEntry
	if err := json.Unmarshal(localModelsJSON, &all); err != nil {
		return nil, err
	}

	return all, nil
})

// localModels are the entries that can be downloaded, with their URL.
func localModels() ([]localModelEntry, error) {
	all, err := loadLocalModels()
	if err != nil {
		return nil, err
	}

	out := make([]localModelEntry, 0, len(all))

	for _, m := range all {
		if m.pinned() {
			m.URL = m.url()
			out = append(out, m)
		}
	}

	return out, nil
}

func localModelByID(id string) (localModelEntry, bool) {
	all, err := localModels()
	if err != nil {
		return localModelEntry{}, false
	}

	for _, m := range all {
		if m.ID == id {
			return m, true
		}
	}

	return localModelEntry{}, false
}

// defaultLocalModel is the recommended entry's id ("" when none can be downloaded).
func defaultLocalModel() string {
	all, _ := localModels() //nolint:errcheck // the embedded list is checked by the tests

	for _, m := range all {
		if m.Recommended {
			return m.ID
		}
	}

	if len(all) > 0 {
		return all[0].ID
	}

	return ""
}

// LocalModelCatalog lists the models the app can download, as JSON [{id, name,
// recommended, url, bytes, sha256, license, licenseUrl, minMemoryMiB, minMemoryMiBIOS,
// contextTokens, ...}]: both apps download and check them from it.
func LocalModelCatalog() (out string, err error) {
	defer maskResult(&out, &err)

	all, err := localModels()
	if err != nil {
		return "", err
	}

	return toJSON(all)
}

// localModelList is the catalog as the model picker shows it, with the imported file.
func localModelList(current string) []aiModel {
	all, _ := localModels() //nolint:errcheck // the embedded list is checked by the tests

	out := make([]aiModel, 0, len(all)+1)
	for _, m := range all {
		out = append(out, aiModel{ID: m.ID, Name: m.Name})
	}

	if name, ok := strings.CutPrefix(current, localModelFilePrefix); ok && name != "" {
		out = append(out, aiModel{ID: current, Name: name})
	}

	return out
}
