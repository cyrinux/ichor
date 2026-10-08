package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// omniAccountTalosconfig is Omni's talosconfig of a whole account: no cluster.
func omniAccountTalosconfig(endpoint, identity string) string {
	return fmt.Sprintf("context: acme\ncontexts:\n    acme:\n        endpoints:\n            - %s\n        auth:\n            siderov1:\n                identity: %s\n", endpoint, identity)
}

// A service account key for a context without a cluster is checked against Omni's own API:
// the Talos proxy has no cluster to route to.
func TestOmniAccountContextChecksTheKeyWithOmni(t *testing.T) {
	useMemAuthStore(t)

	encoded, key := testServiceAccountKey(t, time.Hour)
	endpoint := startFakeOmniAPI(t, &fakeOmniAPI{key: key})
	cfg := omniAccountTalosconfig(endpoint, testSAIdentity)

	if err := TalosSetCredentials(cfg, "acme", fmt.Sprintf(`{"serviceAccountKey":%q}`, encoded)); err != nil {
		t.Fatalf("the key should check out against Omni's API: %v", err)
	}

	// Opening it says why nothing answers, rather than timing out on Omni's proxy.
	s, err := openSession(cfg, "acme")
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	if err := s.ready(context.Background()); !errors.Is(err, errOmniNoCluster) {
		t.Fatalf("err = %v, want errOmniNoCluster", err)
	}
}
