package gitforge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const testToken = "ghp_test_not_a_real_token"

func hostJSON(kind, baseURL string) string {
	out, _ := json.Marshal(map[string]string{"kind": kind, "baseURL": baseURL, "token": testToken}) //nolint:errcheck

	return string(out)
}

func hostJSONAliases(kind, baseURL string, aliases ...string) string {
	out, _ := json.Marshal(map[string]any{"kind": kind, "baseURL": baseURL, "token": testToken, "aliases": aliases}) //nolint:errcheck

	return string(out)
}

// testForge is the client for host, which must parse.
func testForge(t *testing.T, host string) Forge {
	t.Helper()

	h, err := ParseHost(host)
	if err != nil {
		t.Fatal(err)
	}

	return New(h)
}

func TestRefusedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"message":"bad credentials %s"}`, testToken)
	}))
	t.Cleanup(srv.Close)

	_, err := testForge(t, hostJSON("github", srv.URL)).Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "the token for 127.0.0.1 was refused") || strings.Contains(err.Error(), testToken) {
		t.Errorf("refused: %v", err)
	}
}

// A redirect to another host is never followed: the token stays where it was sent.
func TestRefusesRedirects(t *testing.T) {
	var hits atomic.Int32

	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(other.Close)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	repo := RepoRef{Host: "127.0.0.1", Owner: "ops", Name: "fleet"}

	_, err := testForge(t, hostJSON("gitea", srv.URL)).Tree(context.Background(), repo, "main", "")
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("redirect: %v", err)
	}

	if hits.Load() != 0 {
		t.Error("the redirect was followed")
	}
}

func TestRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1900000000")
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	_, err := testForge(t, hostJSON("github", srv.URL)).Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rate limited by 127.0.0.1 until") {
		t.Errorf("rate limit: %v", err)
	}
}
