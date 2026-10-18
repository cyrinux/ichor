package ichorgo

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// TestKubePluginActionChangedSinceChecked: a pinned patch the API server refuses because the
// object moved reads "changed since it was checked", for a merge patch (409) and a JSON patch
// whose resourceVersion test failed.
func TestKubePluginActionChangedSinceChecked(t *testing.T) {
	for _, tc := range []struct {
		action, params string
		status         int
		body           string
	}{
		{"pause", "", http.StatusConflict, `{"kind":"Status","reason":"Conflict","message":"the object has been modified","code":409}`},
		{"resize", `{"size":7}`, http.StatusUnprocessableEntity, `{"kind":"Status","reason":"Invalid","message":"the test operation on /metadata/resourceVersion failed","code":422}`},
	} {
		f, cfg := actionCluster(t)
		f.answerWith("PATCH "+actAlpha, tc.status, tc.body)

		err := KubePluginAction(cfg, "admin@test", "", "demo-actions", tc.action, "demo-shop", "alpha", tc.params)

		var apiErr *kubeAPIError
		if !errors.As(err, &apiErr) || apiErr.Code != http.StatusConflict || !strings.Contains(err.Error(), "Widget alpha changed since it was checked: refresh and try again") {
			t.Errorf("%s: %v", tc.action, err)
		}

		if got := mutation(t, f); !strings.Contains(got.body, `"812"`) {
			t.Errorf("%s not pinned: %s", tc.action, got.body)
		}
	}

	// An action without enabledWhen is not pinned: its 409 stays the API server's.
	f, cfg := actionCluster(t)
	f.answerWith("PATCH "+actAlpha, http.StatusConflict, `{"kind":"Status","reason":"Conflict","message":"busy","code":409}`)

	if err := KubePluginAction(cfg, "admin@test", "", "demo-actions", "tag", "demo-shop", "alpha", ""); err == nil || strings.Contains(err.Error(), "changed since") {
		t.Errorf("tag: %v", err)
	}
}

// TestKubePluginActionParamsCap: params over 16 KiB are refused and recorded by size only.
func TestKubePluginActionParamsCap(t *testing.T) {
	withDataDir(t)
	SetAuditEvents(false)
	usePlugins(t, "actions.yaml")
	noKubeDial(t)

	big := `{"reason":"` + strings.Repeat("x", maxPluginParams) + `"}`

	if err := KubePluginAction("cfg", "ctx", "", "demo-actions", "pause", "demo-shop", "alpha", big); err == nil || !strings.Contains(err.Error(), "at most 16 KiB") {
		t.Errorf("got %v", err)
	}

	entries := readAudit(t, "", "plugin:demo-actions:pause")
	if len(entries) != 1 || entries[0].Params != "("+strconv.Itoa(len(big))+" bytes, over 16 KiB)" {
		t.Errorf("audit %+v", entries)
	}
}

// TestKubePluginActionSecretsInScreenshotMode: a Secret is read masked in screenshot mode,
// so no action runs on one then.
func TestKubePluginActionSecretsInScreenshotMode(t *testing.T) {
	src := `schemaVersion: ichor.dev/plugin/v1alpha1
id: demo-secrets
name: Demo Secrets
detect:
  - group: actions.example.com
resources:
  secret: { group: "", version: v1, resource: secrets, kind: Secret, scope: Namespaced }
cards:
  summary:
    title: '"Secrets"'
actions:
  touch:
    label: "Touch"
    on: secret
    verb: annotate
    body: { "actions.example.com/touched": "yes" }
    rbac: { verbs: [patch], group: "", resource: secrets }
    audit: demo-secrets.touch
samples:
  secret:
    - { metadata: { name: demo-key, namespace: demo-shop } }
`

	c, err := plugin.CompileSource("secrets.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	saved := knownPlugins
	knownPlugins = func() []*plugin.Compiled { return []*plugin.Compiled{c} }

	t.Cleanup(func() { knownPlugins = saved })
	noKubeDial(t)

	SetPrivacyMask(true, "")
	defer SetPrivacyMask(false, "")

	if err := KubePluginAction("cfg", "ctx", "", "demo-secrets", "touch", "demo-shop", "demo-key", ""); err == nil || !strings.Contains(err.Error(), "screenshot mode") {
		t.Errorf("got %v", err)
	}
}

// TestPluginActionAccessUnknown: a review that cannot be asked leaves allowed unset (unknown),
// not true.
func TestPluginActionAccessUnknown(t *testing.T) {
	_, cfg := actionCluster(t)

	saved, savedCache := kubeActionChecks, actionAccess
	kubeActionChecks = pluginActionChecks(knownPlugins())
	actionAccess = newAccessCache(accessCacheTTL)

	t.Cleanup(func() { kubeActionChecks, actionAccess = saved, savedCache })

	tree, err := KubePluginData(cfg, "admin@test", "", "demo-actions", "demo-shop", "")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(tree, `"allowed"`) || !strings.Contains(tree, `"id":"pause"`) {
		t.Errorf("unknown access answered: %s", tree)
	}
}
