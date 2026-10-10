package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClassifyImportText(t *testing.T) {
	for _, tc := range []struct{ text, kind, field string }{
		{`{"type":"authorized_user","refresh_token":"invented-secret"}`, "credentials", gcpFieldUserCredentials},
		{`{"type":"external_account_authorized_user","refresh_token":"invented-secret"}`, "credentials", gcpFieldUserCredentials},
		{`{"type":"service_account","private_key":"invented-secret"}`, "credentials", gcpFieldServiceAccount},
		{"kind: Config\nclusters: []\ncontexts: []\n", "kubeconfig", ""},
		{"context: cp\ncontexts:\n  cp:\n    endpoints: [127.0.0.1]\n", "talosconfig", ""},
		{`{"type":"external_account"}`, "unknown", ""},
		{`{"type":"authorized_user"`, "unknown", ""},
		{"garbage", "unknown", ""},
	} {
		out, err := ClassifyImportText(tc.text)
		if err != nil {
			t.Fatal(err)
		}
		var got struct{ Kind, Provider, Field string }
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		if got.Kind != tc.kind || got.Field != tc.field {
			t.Errorf("route = %s, want %s / %s", out, tc.kind, tc.field)
		}
		if got.Kind == "credentials" && got.Provider != "gke" {
			t.Errorf("provider = %s", got.Provider)
		}
		if strings.Contains(out, "invented-secret") {
			t.Fatal("classifier returned credentials")
		}
	}
}
