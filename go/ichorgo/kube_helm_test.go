package ichorgo

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// helmSecretData is a release as Helm stores it in a Secret's data (JSON-escaped for the fake).
func helmSecretData(t *testing.T, release map[string]any) string {
	t.Helper()

	raw, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	_ = zw.Close()

	helm := base64.StdEncoding.EncodeToString(buf.Bytes())

	return base64.StdEncoding.EncodeToString([]byte(helm))
}

func helmRow(secret, name string, version int, status string) string {
	return `{"cells":[],"object":{"metadata":{"name":"` + secret + `","namespace":"web","labels":{"owner":"helm","name":"` + name +
		`","version":"` + itoa(version) + `","status":"` + status + `","modifiedAt":"1767225600"}}}}`
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestKubeHelmReleases(t *testing.T) {
	rel := func(version int, chartVersion string) map[string]any {
		return map[string]any{
			"name": "site", "namespace": "web", "version": version,
			"info":     map[string]any{"status": "deployed", "description": "Upgrade complete", "notes": "visit me"},
			"chart":    map[string]any{"metadata": map[string]any{"name": "site", "version": chartVersion, "appVersion": "2.0"}},
			"config":   map[string]any{"replicas": 3},
			"manifest": "kind: Deployment\n",
		}
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/web/secrets": `{"kind":"Table","apiVersion":"meta.k8s.io/v1","columnDefinitions":[],"rows":[` +
			helmRow("sh.helm.release.v1.site.v1", "site", 1, "superseded") + "," +
			helmRow("sh.helm.release.v1.site.v2", "site", 2, "deployed") + `]}`,
		"GET /api/v1/namespaces/web/secrets/sh.helm.release.v1.site.v2": `{"data":{"release":"` + helmSecretData(t, rel(2, "1.2.0")) + `"}}`,
	})

	stored := kubeStoreFor(t, f)

	out, err := KubeHelmReleases(stored, "admin@test", "", "web")
	if err != nil {
		t.Fatal(err)
	}

	var list helmReleaseList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}

	if len(list.Releases) != 1 || list.Releases[0].Revision != 2 || list.Releases[0].ChartVersion != "1.2.0" || list.Releases[0].Status != "deployed" {
		t.Fatalf("releases %+v", list.Releases)
	}

	out, err = KubeHelmRelease(stored, "admin@test", "", "web", "site")
	if err != nil {
		t.Fatal(err)
	}

	var detail helmReleaseDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatal(err)
	}

	if detail.Values != "replicas: 3\n" || detail.Notes != "visit me" || detail.Manifest != "kind: Deployment\n" ||
		len(detail.History) != 2 || detail.History[0].Revision != 2 || detail.Updated != 1767225600 {
		t.Fatalf("detail %+v", detail)
	}

	for _, r := range f.recorded() {
		if strings.HasSuffix(r.path, ".v1") {
			t.Error("the superseded revision's payload was read")
		}

		if r.path == "/api/v1/namespaces/web/secrets" && !strings.Contains(r.query, "labelSelector=owner%3Dhelm") {
			t.Errorf("list query %q", r.query)
		}
	}
}

func TestDecodeHelmReleaseRejectsGarbage(t *testing.T) {
	for _, data := range []string{"!!", base64.StdEncoding.EncodeToString([]byte("!!")), base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString([]byte("{"))))} {
		if _, err := decodeHelmRelease(data); err == nil {
			t.Errorf("%q accepted", data)
		}
	}
}
