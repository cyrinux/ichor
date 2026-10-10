package ichorgo

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func summaryForTest(t *testing.T, yaml string) configSummary {
	t.Helper()

	out, err := ParseConfig(yaml)
	if err != nil {
		t.Fatal(err)
	}

	var s configSummary
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}

	return s
}

func TestClusterIDIgnoresContextName(t *testing.T) {
	s := summaryForTest(t, testConfig(t, time.Now().Add(time.Hour)))
	lab, other := s.Contexts[0], s.Contexts[1]

	if lab.Fingerprint == other.Fingerprint {
		t.Fatal("contexts of one cluster keep distinct fingerprints")
	}

	if lab.ClusterID != other.ClusterID || !shareClusterID.MatchString(lab.ClusterID) {
		t.Fatalf("same CA, cluster ids %q and %q", lab.ClusterID, other.ClusterID)
	}

	if s2 := summaryForTest(t, testConfig(t, time.Now().Add(time.Hour))); s2.Contexts[0].ClusterID == lab.ClusterID {
		t.Fatal("another CA gives the same cluster id")
	}
}

func TestClusterIDOfDemoIsTheSameEverywhere(t *testing.T) {
	a := summaryForTest(t, demoConfigForTest(t)).Contexts[0].ClusterID
	b := summaryForTest(t, demoConfigForTest(t)).Contexts[0].ClusterID

	if a == "" || a != b {
		t.Fatalf("demo cluster ids %q and %q", a, b)
	}
}

func TestShareLinkRoundTrip(t *testing.T) {
	const c = "abcdefghijklmnop"

	targets := []shareTarget{
		{Cluster: c, Target: "cluster"},
		{Cluster: c, Target: "etcd"},
		{Cluster: c, Target: "health"},
		{Cluster: c, Target: "argocd"},
		{Cluster: c, Target: "flux"},
		{Cluster: c, Target: "node", Host: "cp-1", Addr: "10.0.0.2", Tab: "live"},
		{Cluster: c, Target: "node", Addr: "fd00::1"},
		{Cluster: c, Target: "workloads", Tab: "cronjobs"},
		{Cluster: c, Target: "workloads"},
		{Cluster: c, Target: "argo-app", Namespace: "argocd", Name: "guestbook"},
		{Cluster: c, Target: "flux-app", Kind: "HelmRelease", Namespace: "flux-system", Name: "podinfo"},
		{Cluster: c, Target: "workload", Kind: "StatefulSet", Namespace: "db", Name: "postgres"},
		{Cluster: c, Target: "pod", Namespace: "db", Name: "postgres-0"},
		{Cluster: c, Target: "cronjob", Namespace: "ops", Name: "backup.nightly"},
		{Cluster: c, Target: "data", Kind: "cloudnative-pg"},
		{Cluster: c, Target: "data"},
		{Cluster: c, Target: "checkup"},
		{Cluster: c, Target: "alerts"},
		{Cluster: c, Target: "storage", Host: "worker-1", Addr: "10.0.0.5"},
	}

	for _, want := range targets {
		in, _ := json.Marshal(want)

		link, err := BuildShareLink(string(in))
		if err != nil {
			t.Fatalf("%+v: %v", want, err)
		}

		if !strings.HasPrefix(link, shareLinkWeb+"#") {
			t.Fatalf("link %q", link)
		}

		fragment := link[strings.Index(link, "#")+1:]
		for _, form := range []string{link, "ichor://open?" + fragment, "ichor://open/?" + fragment} {
			got, err := parseShareLink(form)
			if err != nil || got != want {
				t.Fatalf("%s: got %+v, %v; want %+v", form, got, err, want)
			}
		}
	}
}

func TestShareLinkFragmentDecodedOnce(t *testing.T) {
	// %2526 is "%26" once decoded, a name character the validation refuses, not a new field.
	if got, err := parseShareLink(shareLinkWeb + "#v=1&c=abcdefghijklmnop&t=argo-app&ns=argocd&n=a%2526t=etcd"); err == nil {
		t.Fatalf("accepted as %+v", got)
	}
}

func TestShareLinkDropsFieldsTheTargetDoesNotTake(t *testing.T) {
	got, err := parseShareLink("ichor://open?v=1&c=abcdefghijklmnop&t=etcd&ns=kube-system&n=x&tab=live&action=reboot")
	if err != nil || got != (shareTarget{Cluster: "abcdefghijklmnop", Target: "etcd"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestShareLinkRefusesInvalid(t *testing.T) {
	const base = "ichor://open?v=1&c=abcdefghijklmnop&"

	links := []string{
		"",
		"ichor://demo",
		"ichor://open",
		"ichor://open?v=2&c=abcdefghijklmnop&t=etcd",
		"ichor://open?v=1&c=ABCDEFGHIJKLMNOP&t=etcd",
		"ichor://open?v=1&c=short&t=etcd",
		"ichor://open/elsewhere?v=1&c=abcdefghijklmnop&t=etcd",
		"ichor://user@open?v=1&c=abcdefghijklmnop&t=etcd",
		"https://evil.example/ichor/open/#v=1&c=abcdefghijklmnop&t=etcd",
		"https://cyrinux.github.io/ichor/elsewhere/#v=1&c=abcdefghijklmnop&t=etcd",
		"http://cyrinux.github.io/ichor/open/#v=1&c=abcdefghijklmnop&t=etcd",
		base + "t=shell",
		base + "t=node",
		base + "t=node&h=-bad&a=not%20an%20address",
		base + "t=node&h=cp-1&tab=shell",
		base + "t=workloads&tab=live",
		base + "t=argo-app&ns=argocd",
		base + "t=argo-app&n=guestbook",
		base + "t=argo-app&ns=Argo&n=guestbook",
		base + "t=argo-app&ns=argocd&n=../etc/passwd",
		base + "t=argo-app&ns=argocd&n=" + url.QueryEscape("a\"><script>"),
		base + "t=argo-app&ns=argocd&n=" + strings.Repeat("a", 254),
		base + "t=argo-app&ns=" + strings.Repeat("a", 64) + "&n=x",
		base + "t=workload&k=Job&ns=a&n=b",
		base + "t=flux-app&k=HelmChart&ns=a&n=b",
		base + "t=flux-app&ns=a&n=b",
		base + "t=data&k=Longhorn",
		base + "t=data&k=" + url.QueryEscape("a/b"),
		base + "t=data&k=" + strings.Repeat("a", 64),
		base + "t=etcd&pad=" + strings.Repeat("a", shareLinkMaxLen),
		"ichor://open?v=1&c=abcdefghijklmnop&t=etcd&bad=%zz",
	}

	for _, link := range links {
		if got, err := parseShareLink(link); err == nil {
			t.Errorf("%q accepted as %+v", link, got)
		}
	}
}

func TestBuildShareLinkRefusesInvalid(t *testing.T) {
	for _, in := range []string{"", "{", `{"cluster":"abcdefghijklmnop","target":"pod","ns":"a"}`} {
		if link, err := BuildShareLink(in); err == nil {
			t.Errorf("%q built %q", in, link)
		}
	}
}
