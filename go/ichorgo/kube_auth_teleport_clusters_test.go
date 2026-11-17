package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const teleportOtherContext = "teleport.example.org-other-kube"

func teleportClustersOf(t *testing.T, cfg, contextName string) teleportClusters {
	t.Helper()

	out, err := KubeTeleportClusters(cfg, contextName)
	if err != nil {
		t.Fatal(err)
	}

	var list teleportClusters
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}

	return list
}

// teleportSignedInClusters is "name:signedIn" per cluster, in order.
func teleportSignedInClusters(list teleportClusters) string {
	parts := make([]string, 0, len(list.Clusters))
	for _, c := range list.Clusters {
		state := "out"
		if c.SignedIn {
			state = "in"
		}

		parts = append(parts, c.Name+"@"+c.Context+":"+state)
	}

	return strings.Join(parts, " ")
}

func teleportAddCluster(t *testing.T, cfg, contextName, kubeCluster string) teleportAdded {
	t.Helper()

	out, err := KubeTeleportAddCluster(cfg, contextName, kubeCluster)
	if err != nil {
		t.Fatal(err)
	}

	var added teleportAdded
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatal(err)
	}

	return added
}

func teleportSignInValues(t *testing.T, cfg, contextName string) kubeSignInKind {
	t.Helper()

	out, err := KubeSignInInfo(cfg, contextName)
	if err != nil {
		t.Fatal(err)
	}

	var info kubeSignInKind
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatal(err)
	}

	return info
}

func TestTeleportClustersShareTheSignIn(t *testing.T) {
	store := withAuthStore(t)
	f := newFakeTeleport(t, teleportGitHubPing)
	first := teleportStored(t, teleportKubeconfig(t, f))

	added := teleportAddCluster(t, first, teleportTestContext, "other-kube")
	if added.Context != teleportOtherContext {
		t.Fatalf("new context %q", added.Context)
	}

	cfg := added.Kubeconfig
	if got := teleportSignedInClusters(teleportClustersOf(t, cfg, teleportTestContext)); got !=
		"sample-kube@admin@test:out other-kube@"+teleportOtherContext+":out" {
		t.Fatalf("before any sign-in: %s", got)
	}

	// One sign-in, on the first cluster.
	if err := teleportSignIn(t, cfg, teleportBrowser{notAfter: time.Now().Add(12 * time.Hour)}, f); err != "" {
		t.Fatal(err)
	}

	list := teleportClustersOf(t, cfg, teleportOtherContext)
	if got := teleportSignedInClusters(list); got != "sample-kube@admin@test:in other-kube@"+teleportOtherContext+":out" {
		t.Fatalf("after the first sign-in: %s", got)
	}

	if list.User != "sample-user" || list.Proxy != strings.TrimPrefix(f.URL, "https://") || list.TeleportCluster != "teleport.example.org" {
		t.Errorf("list %+v", list)
	}

	// The second cluster has no certificate, but knows who is signed in at the proxy.
	info := teleportSignInValues(t, cfg, teleportOtherContext)
	if info.SignedIn || info.Values[teleportUserValue] != "sample-user" || info.Values[teleportConnector] != "sample-github" ||
		info.Values["kubeCluster"] != "other-kube" {
		t.Fatalf("second cluster's info %+v", info)
	}

	// Its sign-in asks the proxy for its own Kubernetes cluster (the fake checks the request).
	f.mu.Lock()
	f.kubeCluster = "other-kube"
	f.mu.Unlock()

	if err := teleportSignInAt(t, cfg, teleportOtherContext, teleportBrowser{notAfter: time.Now().Add(12 * time.Hour)}, f); err != "" {
		t.Fatal(err)
	}

	if got := teleportSignedInClusters(teleportClustersOf(t, cfg, teleportTestContext)); got !=
		"sample-kube@admin@test:in other-kube@"+teleportOtherContext+":in" {
		t.Fatalf("after both sign-ins: %s", got)
	}

	// Signing one cluster out keeps the other and the proxy's sign-in.
	if err := KubeSignOut(cfg, teleportOtherContext); err != nil {
		t.Fatal(err)
	}

	if got := teleportSignedInClusters(teleportClustersOf(t, cfg, teleportTestContext)); got !=
		"sample-kube@admin@test:in other-kube@"+teleportOtherContext+":out" {
		t.Fatalf("after one sign-out: %s", got)
	}

	if teleportSignInValues(t, cfg, teleportOtherContext).Values[teleportUserValue] != "sample-user" {
		t.Error("signing one cluster out forgot the proxy's sign-in")
	}

	// Signing out of the proxy forgets everything of it.
	if err := KubeTeleportSignOutAll(cfg, teleportOtherContext); err != nil {
		t.Fatal(err)
	}

	list = teleportClustersOf(t, cfg, teleportTestContext)
	if got := teleportSignedInClusters(list); got != "sample-kube@admin@test:out other-kube@"+teleportOtherContext+":out" || list.User != "" {
		t.Fatalf("after signing out of the proxy: %s, user %q", got, list.User)
	}

	if len(store.states) != 0 {
		t.Errorf("signing out of the proxy left %v", store.states)
	}
}

func TestTeleportAddClusterRefusals(t *testing.T) {
	withAuthStore(t)
	f := newFakeTeleport(t, teleportGitHubPing)
	cfg := teleportStored(t, teleportKubeconfig(t, f))

	for _, name := range []string{"", "Sample", "sample_kube", "-sample", "sample-", strings.Repeat("a", 64)} {
		if _, err := KubeTeleportAddCluster(cfg, teleportTestContext, name); err == nil || !strings.Contains(err.Error(), "lowercase") {
			t.Errorf("%q: %v", name, err)
		}
	}

	if _, err := KubeTeleportAddCluster(cfg, teleportTestContext, "sample-kube"); err == nil || !strings.Contains(err.Error(), "already in the list") {
		t.Errorf("the same cluster again: %v", err)
	}

	// A name tsh would give that the store already uses gets a suffix.
	doc, err := loadKubeconfigDoc(teleportAddCluster(t, cfg, teleportTestContext, "other-kube").Kubeconfig)
	if err != nil {
		t.Fatal(err)
	}

	own, _, err := teleportSiblings(doc, teleportTestContext)
	if err != nil {
		t.Fatal(err)
	}

	if got := teleportContextName(doc, own, "other-kube"); got != teleportOtherContext+"-2" {
		t.Errorf("clashing name %q", got)
	}

	// Not a Teleport cluster.
	plain := newFakeKubeAPI(t, nil).kubeconfigFor("https://plain.example.org")
	if _, err := KubeTeleportAddCluster(plain, "admin@test", "other-kube"); err == nil || !strings.Contains(err.Error(), "Teleport") {
		t.Errorf("plain kubeconfig: %v", err)
	}
}

func TestTeleportAddedContextAsksForItsCluster(t *testing.T) {
	withAuthStore(t)
	f := newFakeTeleport(t, teleportGitHubPing)
	cfg := teleportAddCluster(t, teleportStored(t, teleportKubeconfig(t, f, "--insecure")), teleportTestContext, "other-kube").Kubeconfig

	doc, err := loadKubeconfigDoc(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, ok := doc.context(teleportOtherContext)
	if !ok {
		t.Fatal("no new context")
	}

	user, _ := doc.user(ctx.Context.User)
	cluster, _ := doc.cluster(ctx.Context.Cluster)
	first, _ := doc.cluster("admin@test")

	if flagValue(user.User.Exec.Args, "--kube-cluster") != "other-kube" || flagValue(user.User.Exec.Args, "--proxy") == "" ||
		!strings.Contains(strings.Join(user.User.Exec.Args, " "), "--insecure") {
		t.Errorf("new user's args %v", user.User.Exec.Args)
	}

	if cluster.Cluster.Server != first.Cluster.Server || cluster.Cluster.CertificateAuthorityData != first.Cluster.CertificateAuthorityData {
		t.Errorf("new cluster %+v", cluster.Cluster)
	}

	// The first context is left as it was.
	firstUser, _ := doc.user("admin@test")
	if flagValue(firstUser.User.Exec.Args, "--kube-cluster") != "sample-kube" {
		t.Errorf("first user's args %v", firstUser.User.Exec.Args)
	}

	// Both carry the proxy's shared key, which the apps keep in the auth store.
	out, err := kubeconfigSummaryJSON(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var summary kubeconfigSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}

	want := teleportIdentityKey(strings.TrimPrefix(f.URL, "https://"), "teleport.example.org")
	for _, c := range summary.Contexts {
		if c.AuthKey != want {
			t.Errorf("%s authKey %q, want %q", c.Name, c.AuthKey, want)
		}
	}
}

func TestTeleportClustersOfTheDemo(t *testing.T) {
	demo, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	if list := teleportClustersOf(t, demo, demoKubeContext); len(list.Clusters) != 0 {
		t.Errorf("demo clusters %+v", list)
	}
}

func TestWithFlagValue(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"kube", "credentials", "--kube-cluster=a", "--proxy=p"}, "kube credentials --kube-cluster=b --proxy=p"},
		{[]string{"--kube-cluster", "a", "--proxy=p"}, "--kube-cluster b --proxy=p"},
		{[]string{"--proxy=p"}, "--proxy=p --kube-cluster=b"},
	}

	for _, c := range cases {
		before := strings.Join(c.args, " ")

		if got := strings.Join(withFlagValue(c.args, "--kube-cluster", "b"), " "); got != c.want {
			t.Errorf("%v: %q", c.args, got)
		}

		if strings.Join(c.args, " ") != before {
			t.Errorf("args changed: %v", c.args)
		}
	}
}

// teleportStored is the named test context alone, as the app stores a kubeconfig cluster.
func teleportStored(t *testing.T, cfg string) string {
	t.Helper()

	out, err := kubeContextYAML(cfg, teleportTestContext)
	if err != nil {
		t.Fatal(err)
	}

	return out
}
