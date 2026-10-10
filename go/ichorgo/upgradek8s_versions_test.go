package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestK8sChoices(t *testing.T) {
	stable := func(_ context.Context, minor int) (string, error) {
		return map[int]string{34: "1.34.3", 35: "1.35.1"}[minor], nil
	}

	c := k8sChoices(context.Background(), k8sUpgradePlan{From: "1.34.0", SupportedRange: "1.30–1.35"}, stable)
	if !slices.Equal(c.Versions, []string{"1.34.3", "1.35.1"}) || c.Lo != 30 || c.Hi != 35 || c.Warning != "" {
		t.Errorf("choice = %+v", c)
	}

	// The next minor outside the range is not offered; the current patch is not either.
	c = k8sChoices(context.Background(), k8sUpgradePlan{From: "1.34.3", SupportedRange: "1.30–1.34"}, stable)
	if len(c.Versions) != 0 {
		t.Errorf("at the top of the range = %+v", c)
	}

	// The release list unreadable: nothing offered, said why.
	failing := func(context.Context, int) (string, error) { return "", errors.New("offline") }

	c = k8sChoices(context.Background(), k8sUpgradePlan{From: "1.34.0", SupportedRange: "1.30–1.35"}, failing)
	if len(c.Versions) != 0 || c.Warning == "" {
		t.Errorf("offline = %+v", c)
	}

	// No current version: nothing to offer from.
	if c := k8sChoices(context.Background(), k8sUpgradePlan{}, stable); len(c.Versions) != 0 {
		t.Errorf("unknown = %+v", c)
	}
}

func TestFetchK8sStable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stable-1.34.txt":
			fmt.Fprintln(w, "v1.34.3")
		case "/stable-1.35.txt":
			fmt.Fprintln(w, "v1.33.0") // not the minor asked for
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	saved := k8sStableURL
	k8sStableURL = srv.URL

	t.Cleanup(func() { k8sStableURL = saved })

	if v, err := fetchK8sStable(context.Background(), 34); err != nil || v != "1.34.3" {
		t.Errorf("1.34 = %q, %v", v, err)
	}

	if _, err := fetchK8sStable(context.Background(), 35); err == nil {
		t.Error("a release of another minor must be refused")
	}

	if _, err := fetchK8sStable(context.Background(), 36); err == nil {
		t.Error("a missing file must fail")
	}
}

func TestK8sUpgradeVersionsDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := K8sUpgradeVersions(cfg, "")
	c := decodeJSON[k8sVersionChoice](t, out, err)

	if c.From == "" || len(c.Versions) == 0 {
		t.Fatalf("demo = %+v", c)
	}

	for _, v := range c.Versions {
		if !newerK8s(v, c.From) {
			t.Errorf("%s is not after %s", v, c.From)
		}
	}
}

func TestNewerK8s(t *testing.T) {
	if !newerK8s("1.35.0", "1.34.9") || newerK8s("1.34.0", "1.34.0") || newerK8s("1.33.9", "1.34.0") || !newerK8s("1.34.10", "1.34.9") {
		t.Error("version order")
	}
}
