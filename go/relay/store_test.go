package relay

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"testing"
)

func TestStoreCreatesSecret(t *testing.T) {
	fk, kube := newFakeKube(t)
	store := NewStore(kube, testNS, testSecret)
	if err := store.Load(t.Context()); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(fk.calls, []string{"GET", "POST"}) {
		t.Errorf("calls %v, want GET then POST", fk.calls)
	}

	state := store.Snapshot()
	for key, token := range map[string]string{keyPairing: state.PairingToken, keyWebhook: state.WebhookToken} {
		if len(token) != 43 || fk.data(t, key) != token {
			t.Errorf("%s: %d chars, stored %v", key, len(token), fk.data(t, key) == token)
		}
	}
	if state.PairingToken == state.WebhookToken {
		t.Error("the two tokens are equal")
	}
	if pub, err := base64.RawURLEncoding.DecodeString(state.VAPIDPublicKey); err != nil || len(pub) != 65 {
		t.Errorf("VAPID public key: %d bytes, %v", len(pub), err)
	}
	if got := fk.data(t, keyDevices); got != "[]" {
		t.Errorf("devices.json = %q", got)
	}

	// A restart reads the same state and writes nothing.
	fk.calls = nil
	again := NewStore(kube, testNS, testSecret)
	if err := again.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fk.calls, []string{"GET"}) || again.Snapshot().WebhookToken != state.WebhookToken {
		t.Errorf("restart: calls %v, same token %v", fk.calls, again.Snapshot().WebhookToken == state.WebhookToken)
	}
}

func TestStoreFillsAPreCreatedSecret(t *testing.T) {
	fk, kube := newFakeKube(t)
	fk.store(testSecret, map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": testSecret, "namespace": testNS, "annotations": map[string]any{"argocd.argoproj.io/sync-options": "Prune=false"}},
		"data": map[string]any{
			keyWebhook: base64.StdEncoding.EncodeToString([]byte("set-by-the-admin")),
			"other":    base64.StdEncoding.EncodeToString([]byte("kept")),
		},
	})

	store := NewStore(kube, testNS, testSecret)
	if err := store.Load(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := store.Snapshot().WebhookToken; got != "set-by-the-admin" {
		t.Errorf("webhook token %q, want the admin's", got)
	}
	if fk.data(t, "other") != "kept" || fk.data(t, keyPairing) == "" || fk.data(t, keyVAPIDPrivate) == "" {
		t.Error("the PUT lost a key or did not fill the missing ones")
	}
	meta := fk.secrets[testSecret]["metadata"].(map[string]any)
	if _, ok := meta["annotations"]; !ok {
		t.Error("the PUT dropped the annotations")
	}
}

func TestStoreUpdateRetriesOnConflict(t *testing.T) {
	fk, kube := newFakeKube(t)
	store := NewStore(kube, testNS, testSecret)
	if err := store.Load(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Another replica pairs "other" between our read and our write.
	fk.conflicts = 1
	fk.sideWrite = func(data map[string]any) {
		raw, _ := json.Marshal([]Device{{ID: "other", Platform: PlatformNtfy, Endpoint: "https://ntfy.example/o"}})
		data[keyDevices] = base64.StdEncoding.EncodeToString(raw)
	}

	err := store.Update(t.Context(), func(s *State) error {
		return upsertDevice(s, Device{ID: "mine", Platform: PlatformNtfy, Endpoint: "https://ntfy.example/m"}, testNow)
	})
	if err != nil {
		t.Fatal(err)
	}

	var devices []Device
	if err := json.Unmarshal([]byte(fk.data(t, keyDevices)), &devices); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, d := range devices {
		ids = append(ids, d.ID)
	}
	if !slices.Equal(ids, []string{"other", "mine"}) {
		t.Errorf("devices %v, want other then mine", ids)
	}
}

func TestStoreGivesUpOnEndlessConflicts(t *testing.T) {
	fk, kube := newFakeKube(t)
	store := NewStore(kube, testNS, testSecret)
	if err := store.Load(t.Context()); err != nil {
		t.Fatal(err)
	}

	fk.conflicts = 100
	err := store.Update(t.Context(), func(s *State) error {
		return upsertDevice(s, Device{ID: "mine"}, testNow)
	})
	if err == nil {
		t.Fatal("no error after endless conflicts")
	}
	if len(store.Snapshot().Devices) != 0 {
		t.Error("the failed update changed the in-memory state")
	}
}
