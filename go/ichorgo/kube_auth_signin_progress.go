package ichorgo

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Signing the clusters of a cloud import in: after the preview, every discovered cluster that
// signs in with credentials is signed in with the account's, one after the other (identity
// providers rate-limit). The apps poll SignInProgress meanwhile to name the cluster being
// signed in. One import runs at a time (the import sheet is modal).

var signInProgress signInCounts

// signInCounts is how far the running SignInDiscovered got.
type signInCounts struct {
	mu          sync.Mutex
	running     bool
	done, total int
	current     string
	failed      []string
}

// signInOne signs one stored context in; tests replace it.
var signInOne = KubeSetCredentials

// signInFailure is a context SignInDiscovered could not sign in, and why.
type signInFailure struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// SignInDiscovered signs the contexts namesJSON (a JSON array, as the import kept them) of
// storedYAML in with secretsJSON, the fields of the discovery that found them, one after the
// other. Contexts that do not sign in with credentials (OIDC, Azure device code) are left to
// the user, as are those that need a browser sign-in. Returns {"failed":[{"name","error"}]}.
func SignInDiscovered(storedYAML, namesJSON, secretsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var names []string
	if err := json.Unmarshal([]byte(namesJSON), &names); err != nil {
		return "", fmt.Errorf("context names: %w", err)
	}

	privacy.learnConfig(storedYAML)

	targets := credentialContexts(storedYAML, names)
	signInProgress.start(len(targets))

	defer signInProgress.stop()

	failed := []signInFailure{}

	for _, name := range targets {
		signInProgress.signingIn(name)

		err := signInOne(storedYAML, name, secretsJSON)
		if err != nil && !strings.Contains(err.Error(), KubeSignInRequired) {
			failed = append(failed, signInFailure{Name: name, Error: err.Error()})
			signInProgress.fail(name)
		}

		signInProgress.signedIn()
	}

	return toJSON(map[string]any{"failed": failed})
}

// credentialContexts is names (real or masked) as real context names, those whose cluster
// signs in with credentials.
func credentialContexts(storedYAML string, names []string) []string {
	var out []string

	for _, name := range names {
		name = unmaskContext(storedYAML, name)

		sc, err := signInContext(storedYAML, name)
		if err != nil {
			continue
		}

		if _, ok := sc.method.(credentialMethod); ok {
			out = append(out, name)
		}
	}

	return out
}

func (p *signInCounts) start(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.running, p.done, p.total, p.current, p.failed = true, 0, total, "", nil
}

func (p *signInCounts) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.running, p.current = false, ""
}

func (p *signInCounts) signingIn(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.current = name
}

func (p *signInCounts) signedIn() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.done++
}

func (p *signInCounts) fail(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.failed = append(p.failed, name)
}

// SignInProgress is, as JSON, how far the running SignInDiscovered got:
// {"running":true,"done":2,"total":10,"current":"web.europe-west1.gke","failed":["…"]}.
func SignInProgress() (out string, err error) {
	defer maskResult(&out, &err)

	signInProgress.mu.Lock()
	defer signInProgress.mu.Unlock()

	return toJSON(map[string]any{
		"running": signInProgress.running,
		"done":    signInProgress.done,
		"total":   signInProgress.total,
		"current": signInProgress.current,
		"failed":  append([]string{}, signInProgress.failed...),
	})
}
