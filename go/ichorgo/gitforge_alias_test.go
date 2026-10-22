package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func gitHostJSONAliases(kind, baseURL string, aliases ...string) string {
	out, _ := json.Marshal(map[string]any{"kind": kind, "baseURL": baseURL, "token": gitTestToken, "aliases": aliases}) //nolint:errcheck

	return string(out)
}

func TestGitForgeAliasErrorNamesTheAddress(t *testing.T) {
	host := gitHostJSONAliases("gitea", "https://git.example.org")

	_, err := GitForgeTree(host, "http://gitea-http.gitea.svc:3000/example-org/platform.git", "main", "")
	want := "example-org/platform is on gitea-http.gitea.svc:3000, not on the Git host git.example.org: if it is the same server, add gitea-http.gitea.svc:3000 to the host's other names in Settings → Git hosts"

	if err == nil || err.Error() != want {
		t.Errorf("error %v, want %q", err, want)
	}
}

// A repository on an alias is read from the base server only: the alias server, which
// would get the token otherwise, sees no request.
func TestGitForgeAliasRequestsGoToBaseOnly(t *testing.T) {
	var aliasHits atomic.Int32

	alias := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aliasHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(alias.Close)

	var baseHits atomic.Int32

	base := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v3/user": func(w http.ResponseWriter, _ *http.Request) {
			baseHits.Add(1)
			w.Header().Set("X-OAuth-Scopes", "repo")
			fmt.Fprint(w, `{"login":"octo"}`)
		},
		"/api/v3/repos/example-org/platform/contents/apps": func(w http.ResponseWriter, r *http.Request) {
			baseHits.Add(1)
			jsonAnswer(`[{"type":"file","path":"apps/README.md","size":12}]`)(w, r)
		},
		"/api/v3/repos/example-org/platform/contents/apps/README.md": func(w http.ResponseWriter, r *http.Request) {
			baseHits.Add(1)
			jsonAnswer(`{"type":"file","path":"apps/README.md","sha":"abc","size":5,"encoding":"base64","content":"`+b64Content("hello")+`"}`)(w, r)
		},
	})

	// Base on localhost, alias on 127.0.0.1: both loopback, different names and ports.
	baseURL := strings.Replace(base.URL, "127.0.0.1", "localhost", 1)
	aliasAddr := strings.TrimPrefix(alias.URL, "http://")
	host := gitHostJSONAliases("github", baseURL, aliasAddr)
	repo := alias.URL + "/example-org/platform.git"

	if _, err := GitForgeCheck(host); err != nil {
		t.Fatal(err)
	}

	if _, err := GitForgeTree(host, repo, "main", "apps"); err != nil {
		t.Fatal(err)
	}

	if _, err := GitForgeFile(host, "git@"+strings.Split(aliasAddr, ":")[0]+":example-org/platform.git", "main", "apps/README.md"); err != nil {
		t.Fatal(err)
	}

	if aliasHits.Load() != 0 || baseHits.Load() != 3 {
		t.Errorf("alias got %d requests, base %d", aliasHits.Load(), baseHits.Load())
	}
}
