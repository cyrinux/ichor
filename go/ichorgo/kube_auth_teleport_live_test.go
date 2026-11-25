package ichorgo

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestTeleportLive reaches a real Teleport proxy with certificates `tctl auth sign` issued,
// when asked (the fixture prints these; see docs/build.md "Teleport fixture"):
//
//	just teleport-up
//	TELEPORT_LIVE_KUBECONFIG=… TELEPORT_LIVE_TTL_KUBECONFIG=… [TELEPORT_LIVE_MFA_KUBECONFIG=…] \
//	  go test ./ichorgo -run TestTeleportLive -v
//
// Skipped otherwise: nothing about any proxy lives in the repo. The SSO console sign-in is not
// covered (Teleport Community Edition cannot point its GitHub connector at a fake).
func TestTeleportLive(t *testing.T) {
	main, short := os.Getenv("TELEPORT_LIVE_KUBECONFIG"), os.Getenv("TELEPORT_LIVE_TTL_KUBECONFIG")
	if main == "" || short == "" {
		t.Skip("set TELEPORT_LIVE_KUBECONFIG and TELEPORT_LIVE_TTL_KUBECONFIG (just teleport-up prints them)")
	}

	withAuthStore(t)

	t.Run("list, exec and port-forward", func(t *testing.T) {
		target := withTeleportCertificate(t, main)
		k, _, err := kubeClients.get(target)
		if err != nil {
			t.Fatal(err)
		}

		if k.certs == nil || k.token != "" {
			t.Fatal("the client does not use the Teleport certificate method")
		}

		pod := liveListAndPickPod(t, k)
		liveExec(t, k, pod)
		livePortForward(t, k, pod)
	})

	t.Run("expiry asks for a sign-in", func(t *testing.T) {
		target := withTeleportCertificate(t, short)

		err := liveList(target)
		if err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), "Teleport certificate expired") {
			t.Fatalf("a one-minute certificate: %v", err)
		}
	})

	if mfa := os.Getenv("TELEPORT_LIVE_MFA_KUBECONFIG"); mfa != "" {
		t.Run("per-session MFA refusal", func(t *testing.T) {
			// Teleport redacts the cause: the sentence names both roles and MFA.
			err := liveList(withTeleportCertificate(t, mfa))
			if err == nil || !strings.Contains(err.Error(), "per-session MFA") {
				t.Fatalf("a role with require_session_mfa: %v", err)
			}
		})
	}
}

// withTeleportCertificate turns a certificate kubeconfig from `tctl auth sign
// --format=kubernetes` into the kubeconfig `tsh kube login` writes (exec: tsh) and stores the
// certificate as a sign-in would, so the calls go through teleportMethod, not the static
// client-certificate path.
func withTeleportCertificate(t *testing.T, path string) kubeTarget {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := loadKubeconfigDoc(string(raw))
	if err != nil {
		t.Fatal(err)
	}

	name := doc.currentName()
	kctx, _ := doc.context(name)
	user, okUser := doc.user(kctx.Context.User)
	cluster, okCluster := doc.cluster(kctx.Context.Cluster)

	if !okUser || !okCluster || user.User.ClientCertificateData == "" || user.User.ClientKeyData == "" {
		t.Fatalf("%s: context %q has no inline client certificate", path, name)
	}

	certPEM := mustBase64(t, user.User.ClientCertificateData)
	keyPEM := mustBase64(t, user.User.ClientKeyData)

	proxy := strings.TrimPrefix(cluster.Cluster.Server, "https://")
	user.User.ClientCertificateData, user.User.ClientKeyData = "", ""
	user.User.Exec = &kubeStoreExec{
		APIVersion: "client.authentication.k8s.io/v1beta1",
		Command:    "tsh",
		Args:       []string{"kube", "credentials", "--kube-cluster=" + teleportLiveKubeCluster, "--proxy=" + proxy},
	}

	cfg, err := encodeKubeconfigDoc(doc)
	if err != nil {
		t.Fatal(err)
	}

	sc, err := signInContext(cfg, name)
	if err != nil {
		t.Fatal(err)
	}

	if sc.method == nil || sc.method.name() != authTeleport {
		t.Fatalf("the rewritten kubeconfig is not a Teleport sign-in: %v", sc.method)
	}

	// The fixture's kubeconfigs share one context: drop what the previous one left.
	kubeClients.forgetConfig(cfg, name)
	kubeAuth.forget(sc.key)
	kubeAuth.save(sc.key, kubeAuthState{}.withSession(map[string]string{
		teleportSessionCert: certPEM, teleportSessionKey: keyPEM, teleportSessionUser: "live",
	}))

	target := kubeTarget{config: cfg, context: name}
	t.Cleanup(func() { kubeClients.forgetConfig(cfg, name) })

	return target
}

// teleportLiveKubeCluster is the name the fixture registers its kind cluster under.
const teleportLiveKubeCluster = "sample-kube"

func mustBase64(t *testing.T, s string) string {
	t.Helper()

	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func liveList(target kubeTarget) error {
	_, err := withKube(target, func(ctx context.Context, k *kubeClient) (struct{}, error) {
		return struct{}{}, k.get(ctx, "/api/v1/namespaces", nil)
	})

	return err
}

// liveListAndPickPod lists the namespaces and returns a running CoreDNS pod of kube-system.
func liveListAndPickPod(t *testing.T, k *kubeClient) string {
	t.Helper()

	var namespaces struct {
		Items []struct {
			Metadata struct{ Name string } `json:"metadata"`
		} `json:"items"`
	}
	if err := k.get(t.Context(), "/api/v1/namespaces", &namespaces); err != nil {
		t.Fatalf("namespaces: %v", err)
	}

	t.Logf("%d namespaces", len(namespaces.Items))

	var pods struct {
		Items []struct {
			Metadata struct{ Name string }  `json:"metadata"`
			Status   struct{ Phase string } `json:"status"`
		} `json:"items"`
	}
	if err := k.get(t.Context(), "/api/v1/namespaces/kube-system/pods?labelSelector=k8s-app%3Dkube-dns", &pods); err != nil {
		t.Fatalf("pods: %v", err)
	}

	for _, p := range pods.Items {
		if p.Status.Phase == "Running" {
			return p.Metadata.Name
		}
	}

	t.Fatal("no running CoreDNS pod in kube-system")

	return ""
}

// liveExec runs CoreDNS's own binary (the image has no shell) and reads its version line.
func liveExec(t *testing.T, k *kubeClient, pod string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var lines []string

	err := k.execLines(ctx, "kube-system", pod, "coredns", []string{"/coredns", "-version"}, func() {}, func(b []byte) {
		lines = append(lines, string(b))
	})
	if err != nil || len(lines) == 0 {
		t.Fatalf("exec: %v, %q", err, lines)
	}

	t.Logf("exec: %s", lines[0])
}

// livePortForward asks CoreDNS's health port (8080) for /health through the proxy.
func livePortForward(t *testing.T, k *kubeClient, pod string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	conn, err := k.dialPodPort(ctx, "kube-system", pod, 8080)
	if err != nil {
		t.Fatalf("port-forward: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	if _, err := fmt.Fprint(conn, "GET /health HTTP/1.0\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("port-forward answer: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "OK") {
		t.Fatalf("port-forward: HTTP %d %q", resp.StatusCode, body)
	}
}
