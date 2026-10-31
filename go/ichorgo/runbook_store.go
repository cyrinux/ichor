package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Runbooks kept on the phone: each cluster's in one file of the app's data directory (see
// SetDataDir), sealed like the audit log. scope is the app's name for the cluster's list: its
// fingerprint, and apart while screenshot mode is on (as the pins are), so masked and real
// names never mix. A runbook is stored only once its plan was shown (the import's consent),
// and it is never run on import.

const (
	runbookStoreDir = "runbooks"
	maxRunbooks     = 50
)

var (
	runbookStoreMu  sync.Mutex
	runbookStoreNow = time.Now
)

type storedRunbook struct {
	ID         string `json:"id"`
	YAML       string `json:"yaml"`
	ImportedAt int64  `json:"importedAt"`
}

// runbookItem is a list row: the summary, or why the stored runbook no longer loads.
type runbookItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Steps       int    `json:"steps"`
	Destructive int    `json:"destructive"`
	ImportedAt  int64  `json:"importedAt"`
	Problem     string `json:"problem,omitempty"`
}

func (s storedRunbook) item() runbookItem {
	it := runbookItem{ID: s.ID, ImportedAt: s.ImportedAt}

	rb, err := loadRunbook(s.YAML)
	if err != nil {
		it.Name, it.Problem = s.ID, err.Error()

		return it
	}

	it.Name, it.Description, it.Steps = rb.Name, rb.Description, len(rb.Steps)
	for _, st := range rb.Steps {
		if st.action().destructive {
			it.Destructive++
		}
	}

	return it
}

// runbookID names a runbook by its name: importing the same name again replaces it.
func runbookID(name string) string {
	var b strings.Builder

	dash := false

	for _, c := range strings.ToLower(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)

			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')

			dash = true
		}
	}

	id := strings.TrimSuffix(b.String(), "-")
	if len(id) > 48 {
		id = strings.TrimSuffix(id[:48], "-")
	}

	if id == "" {
		id = "runbook-" + sha256Hex([]byte(name))[:8]
	}

	return id
}

func runbookStorePath(dir, scope string) string {
	return filepath.Join(dir, runbookStoreDir, sha256Hex([]byte(scope))[:32]+".json")
}

var errNoRunbookStore = errors.New("this phone has no storage for runbooks")

func loadRunbooksLocked(scope string) ([]storedRunbook, error) {
	dir, aead := dataStore()
	if aead == nil {
		return nil, errNoRunbookStore
	}

	sealed, err := os.ReadFile(runbookStorePath(dir, scope))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	data, err := openSealed(aead, sealed)
	if err != nil {
		return nil, fmt.Errorf("the stored runbooks cannot be read: %w", err)
	}

	var items []storedRunbook
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("the stored runbooks cannot be read: %w", err)
	}

	return items, nil
}

func writeRunbooksLocked(scope string, items []storedRunbook) error {
	dir, aead := dataStore()
	if aead == nil {
		return errNoRunbookStore
	}

	path := runbookStorePath(dir, scope)
	if len(items) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.Marshal(items)
	if err != nil {
		return err
	}

	return writeSealed(path, data, aead)
}

// RunbookStoreList lists the runbooks kept for scope, by name: {"items":[{"id","name",
// "description","steps","destructive","importedAt","problem"}]}; problem says why a stored
// runbook no longer loads (it can only be removed then).
func RunbookStoreList(scope string) (out string, err error) {
	defer maskResult(&out, &err)

	runbookStoreMu.Lock()
	items, err := loadRunbooksLocked(scope)
	runbookStoreMu.Unlock()

	if err != nil {
		return "", err
	}

	list := struct {
		Items []runbookItem `json:"items"`
	}{Items: make([]runbookItem, 0, len(items))}

	for _, s := range items {
		list.Items = append(list.Items, s.item())
	}

	slices.SortFunc(list.Items, func(a, b runbookItem) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })

	return toJSON(list)
}

// RunbookStoreImport keeps runbookYAML for scope, after the import showed its plan: checked
// as RunbookPlan checks it; a runbook of the same name replaces the stored one. Answers its
// list row.
func RunbookStoreImport(scope, runbookYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	rb, err := loadRunbook(runbookYAML)
	if err != nil {
		return "", err
	}

	runbookStoreMu.Lock()
	defer runbookStoreMu.Unlock()

	items, err := loadRunbooksLocked(scope)
	if err != nil {
		return "", err
	}

	stored := storedRunbook{ID: runbookID(rb.Name), YAML: runbookYAML, ImportedAt: runbookStoreNow().UnixMilli()}
	next := slices.Clone(items)

	if at := slices.IndexFunc(next, func(s storedRunbook) bool { return s.ID == stored.ID }); at >= 0 {
		next[at] = stored
	} else if len(next) >= maxRunbooks {
		return "", fmt.Errorf("at most %d runbooks per cluster: remove one first", maxRunbooks)
	} else {
		next = append(next, stored)
	}

	if err := writeRunbooksLocked(scope, next); err != nil {
		return "", err
	}

	return toJSON(stored.item())
}

// RunbookStoreGet is the YAML of the runbook id kept for scope.
func RunbookStoreGet(scope, id string) (out string, err error) {
	// The answer is the user's own file: only the error is masked.
	defer maskErr(&err)

	runbookStoreMu.Lock()
	defer runbookStoreMu.Unlock()

	items, err := loadRunbooksLocked(scope)
	if err != nil {
		return "", err
	}

	at := slices.IndexFunc(items, func(s storedRunbook) bool { return s.ID == id })
	if at < 0 {
		return "", fmt.Errorf("no runbook %q", id)
	}

	return items[at].YAML, nil
}

// RunbookStoreRemove deletes the runbook id kept for scope; removing none is not an error.
func RunbookStoreRemove(scope, id string) (err error) {
	defer maskErr(&err)

	runbookStoreMu.Lock()
	defer runbookStoreMu.Unlock()

	items, err := loadRunbooksLocked(scope)
	if err != nil {
		return err
	}

	next := slices.DeleteFunc(slices.Clone(items), func(s storedRunbook) bool { return s.ID == id })
	if len(next) == len(items) {
		return nil
	}

	return writeRunbooksLocked(scope, next)
}
