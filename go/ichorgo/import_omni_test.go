package ichorgo

import (
	"encoding/json"
	"maps"
	"testing"
)

func TestClassifyOmniConfigs(t *testing.T) {
	const (
		omniconfig = "contexts:\n    default:\n        url: https://acme.eu-central-1.omni.siderolabs.io\n        auth:\n            siderov1:\n                identity: ops@example.com\ncontext: default\n"
		account    = "context: acme\ncontexts:\n    acme:\n        endpoints:\n            - https://acme.omni.siderolabs.io\n        auth:\n            siderov1:\n                identity: ops@example.com\n"
		sa         = "context: acme\ncontexts:\n    acme:\n        url: https://acme.omni.siderolabs.io/\n        auth:\n            siderov1:\n                identity: ci" + serviceAccountDomain + "\n"
		noAuth     = "context: acme\ncontexts:\n    acme:\n        url: https://acme.omni.siderolabs.io\n"
		picked     = "context: second\ncontexts:\n    first:\n        url: https://first.omni.siderolabs.io\n    second:\n        url: https://second.omni.siderolabs.io\n        auth:\n            siderov1:\n                identity: ops@example.com\n"
		// Omni's talosconfig of one cluster imports as is.
		withCluster = "context: acme-prod\ncontexts:\n    acme-prod:\n        endpoints:\n            - https://acme.omni.siderolabs.io\n        auth:\n            siderov1:\n                identity: ops@example.com\n        cluster: prod\n"
		plainURL    = "context: acme\ncontexts:\n    acme:\n        url: http://acme.omni.siderolabs.io\n"
	)

	for _, tc := range []struct {
		name, text, kind, field string
		values                  map[string]string
	}{
		{"omniconfig", omniconfig, "credentials", omniFieldEmail, map[string]string{omniFieldURL: "https://acme.eu-central-1.omni.siderolabs.io", omniFieldEmail: "ops@example.com"}},
		{"account talosconfig", account, "credentials", omniFieldEmail, map[string]string{omniFieldURL: "https://acme.omni.siderolabs.io", omniFieldEmail: "ops@example.com"}},
		{"service account", sa, "credentials", omniFieldServiceAccountKey, map[string]string{omniFieldURL: "https://acme.omni.siderolabs.io"}},
		{"no identity", noAuth, "credentials", omniFieldEmail, map[string]string{omniFieldURL: "https://acme.omni.siderolabs.io"}},
		{"current context", picked, "credentials", omniFieldEmail, map[string]string{omniFieldURL: "https://second.omni.siderolabs.io", omniFieldEmail: "ops@example.com"}},
		{"cluster talosconfig", withCluster, "talosconfig", "", nil},
		{"not https", plainURL, "talosconfig", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ClassifyImportText(tc.text)
			if err != nil {
				t.Fatal(err)
			}

			var got struct {
				Kind, Provider, Field string
				Values                map[string]string
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}

			if got.Kind != tc.kind || got.Field != tc.field || !maps.Equal(got.Values, tc.values) {
				t.Fatalf("route = %s", out)
			}

			if tc.kind == "credentials" && got.Provider != discoverOmni {
				t.Fatalf("provider = %s", got.Provider)
			}
		})
	}
}
