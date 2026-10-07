package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestOmniInstancesAreDistinct(t *testing.T) {
	_, a, _ := resolveContext(omniTalosconfigYAML(testUserIdentity, "https://acme.eu-central-1.omni.example.com", ""), "")
	_, b, _ := resolveContext(omniTalosconfigYAML(testUserIdentity, "https://other.us-east-1.omni.example.com:443", ""), "")

	if omniHost(a) != "acme.eu-central-1.omni.example.com" || omniHost(b) != "other.us-east-1.omni.example.com:443" {
		t.Fatalf("hosts = %q, %q", omniHost(a), omniHost(b))
	}

	if sameCluster(a, b) || clusterID(a) == clusterID(b) || contextFingerprint("x", a) == contextFingerprint("x", b) {
		t.Fatal("the same cluster name on two Omni instances is two clusters")
	}

	// A self-hosted Omni's CA is the instance's: its clusters stay apart.
	_, ca1, _ := resolveContext(omniTalosconfigYAML(testUserIdentity, testOmniEndpoint, "Y2E="), "")
	_, ca2, _ := resolveContext(strings.Replace(omniTalosconfigYAML(testUserIdentity, testOmniEndpoint, "Y2E="), "cluster: demo", "cluster: prod", 1), "")

	if sameCluster(ca1, ca2) || clusterID(ca1) == clusterID(ca2) {
		t.Fatal("two clusters of one self-hosted Omni must not be the same cluster")
	}
}

// refusingStream is a stream whose server refused the key.
type refusingStream struct{ grpc.ClientStream }

func (refusingStream) RecvMsg(any) error { return status.Error(codes.Unauthenticated, "bad signature") }

func TestOmniStreamRefusalNeedsSignIn(t *testing.T) {
	s := &omniStream{ClientStream: refusingStream{}, signing: &omniSigning{method: omniUserMethod}}

	if err := s.RecvMsg(nil); !strings.HasPrefix(fmt.Sprint(err), KubeSignInRequired) {
		t.Fatalf("err = %v, want a sign-in request", err)
	}
}

func TestOmniSigningIgnoresTheEnvironment(t *testing.T) {
	envKey, _ := testServiceAccountKey(t, time.Hour)
	t.Setenv("OMNI_SERVICE_ACCOUNT_KEY", envKey)

	useMemAuthStore(t)

	encoded, key := testServiceAccountKey(t, time.Hour)
	endpoint, caB64 := startFakeOmni(t, &fakeOmni{key: key})
	cfg := omniTalosconfigYAML(testSAIdentity, endpoint, caB64)

	_, ctx, _ := resolveContext(cfg, "")
	kubeAuth.save(omniAuthKey(ctx), kubeAuthState{Method: omniServiceAccountMethod, Secrets: map[string]string{omniServiceAccountField: encoded}})

	s, err := openSession(cfg, "")
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	callCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := s.client.Version(callCtx); err != nil {
		t.Fatalf("the stored key must sign, not OMNI_SERVICE_ACCOUNT_KEY: %v", err)
	}
}

func TestOmniRenewsToANewerStoredKey(t *testing.T) {
	useMemAuthStore(t)

	oldKey, _ := testServiceAccountKey(t, time.Hour)
	newKey, serverKey := testServiceAccountKey(t, time.Hour)
	endpoint, caB64 := startFakeOmni(t, &fakeOmni{key: serverKey})
	cfg := omniTalosconfigYAML(testSAIdentity, endpoint, caB64)

	_, ctx, _ := resolveContext(cfg, "")
	key := omniAuthKey(ctx)
	kubeAuth.save(key, kubeAuthState{Method: omniServiceAccountMethod, Secrets: map[string]string{omniServiceAccountField: oldKey}})

	s, err := openSession(cfg, "")
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	// Signed in again elsewhere: the refused call retries with the stored key.
	kubeAuth.save(key, kubeAuthState{Method: omniServiceAccountMethod, Secrets: map[string]string{omniServiceAccountField: newKey}})

	callCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := s.client.Version(callCtx); err != nil {
		t.Fatalf("err = %v, want the newer key used", err)
	}
}

func TestOmniContextIsNotProbed(t *testing.T) {
	_, ctx, _ := resolveContext(omniTalosconfigYAML(testUserIdentity, testOmniEndpoint, ""), "")

	if _, err := probeEndpoint(context.Background(), ctx, "10.0.0.1:50000"); !errors.Is(err, errOmniNotProbed) {
		t.Fatalf("err = %v, want no probe of an Omni context", err)
	}
}

func TestMaskOmniIdentityAndCluster(t *testing.T) {
	enableMask(t, "")

	privacy.learnConfig(strings.Replace(omniTalosconfigYAML("jdoe@corp-example.net", testOmniEndpoint, ""), "cluster: demo", "cluster: payments", 1))

	got := privacy.maskPlain("signed in as jdoe@corp-example.net to payments")
	if strings.Contains(got, "jdoe") || strings.Contains(got, "corp-example") || strings.Contains(got, "payments") {
		t.Fatalf("masked = %q", got)
	}
}
