package ichorgo

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestOmniLive runs the Omni calls against a real instance, read only, when asked:
//
//	OMNI_LIVE_ENDPOINT=https://<instance> OMNI_LIVE_KEY_FILE=<file with OMNI_SERVICE_ACCOUNT_KEY> \
//	  go test ./ichorgo -run TestOmniLive -v
//
// Skipped otherwise: nothing about any instance lives in the repo.
func TestOmniLive(t *testing.T) {
	endpoint, keyFile := os.Getenv("OMNI_LIVE_ENDPOINT"), os.Getenv("OMNI_LIVE_KEY_FILE")
	if endpoint == "" || keyFile == "" {
		t.Skip("set OMNI_LIVE_ENDPOINT and OMNI_LIVE_KEY_FILE to run against a real Omni")
	}

	raw, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}

	signer, err := decodeServiceAccount(string(raw))
	if err != nil {
		t.Fatal(err)
	}

	identity := signer.identity
	if !strings.Contains(identity, "@") {
		identity += serviceAccountDomain
	}

	cfgCtx, err := omniInstanceContext(endpoint, identity)
	if err != nil {
		t.Fatal(err)
	}

	cc, err := omniDial(cfgCtx, &omniSigning{method: omniServiceAccountMethod, signer: signer})
	if err != nil {
		t.Fatal(err)
	}

	defer cc.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clusters, err := omniListClusters(ctx, cc)
	t.Logf("State/List Clusters: %d clusters, err %v", len(clusters), err)

	if err != nil {
		t.FailNow()
	}

	for _, cl := range clusters {
		config, err := omniTalosconfig(ctx, cc, cl.Name)
		t.Logf("Talosconfig of a cluster (Talos %s): %d bytes, err %v", cl.TalosVersion, len(config), err)

		kube, err := omniKubeconfig(ctx, cc, cl.Name)
		t.Logf("Kubeconfig of a cluster: %d bytes, err %v", len(kube), err)
	}
}
