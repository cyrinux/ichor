package ichorgo

import (
	"testing"
	"time"
)

// A sign-in v1.14 stored under the context's fingerprint moves to the identity's auth key.
func TestOmniLegacySignInMoves(t *testing.T) {
	useMemAuthStore(t)

	encoded, _ := testServiceAccountKey(t, time.Hour)
	cfg := omniTalosconfigYAML(testSAIdentity, testOmniEndpoint, "")
	name, ctx, _ := resolveContext(cfg, "")

	legacy := contextFingerprint(name, ctx)
	kubeAuth.save(legacy, kubeAuthState{Method: omniServiceAccountMethod, Secrets: map[string]string{omniServiceAccountField: encoded}})

	sc, err := loadOmniSignInContext(cfg, "")
	if err != nil {
		t.Fatal(err)
	}

	if sc.key != omniAuthKey(ctx) {
		t.Fatalf("key = %q, want the auth key", sc.key)
	}

	if _, err := loadOmniSigner(sc.key, ctx); err != nil {
		t.Fatalf("the legacy sign-in did not move: %v", err)
	}

	if kubeAuth.load(legacy).Method != "" {
		t.Fatal("the legacy entry must go once moved")
	}
}
