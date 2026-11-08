package helm

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cyrinux/ichor/go/kube/kubetest"
)

const refsTestPath = "/api/v1/namespaces/web/secrets"

func refsTable(continueToken string, rows ...string) string {
	return `{"kind":"Table","apiVersion":"meta.k8s.io/v1","metadata":{"continue":"` + continueToken + `"},"columnDefinitions":[],"rows":[` + strings.Join(rows, ",") + `]}`
}

// The list asks for a Table of metadata by the release's labels, on every page.
func TestSecretRefsPages(t *testing.T) {
	f := kubetest.New(t, nil)
	f.AnswerQuery = func(path, query string) (string, bool) {
		if path != refsTestPath {
			return "", false
		}

		if strings.Contains(query, "continue=next") {
			return refsTable("", helmRow("sh.helm.release.v1.site.v2", "site", 2, "deployed")), true
		}

		return refsTable("next", helmRow("sh.helm.release.v1.site.v1", "site", 1, "superseded")), true
	}

	refs, err := SecretRefs(context.Background(), f.KubeClient(t), "web", "site")
	if err != nil {
		t.Fatal(err)
	}

	if len(refs) != 2 || refs[0].Revision != 1 || refs[1].Revision != 2 || refs[1].Status != "deployed" ||
		refs[1].Namespace != "web" || refs[1].Secret != "sh.helm.release.v1.site.v2" || refs[1].Modified != 1767225600 {
		t.Fatalf("refs %+v", refs)
	}

	for _, r := range f.Recorded() {
		for _, want := range []string{"limit=500", "includeObject=Metadata", "labelSelector=owner%3Dhelm%2Cname%3Dsite"} {
			if !strings.Contains(r.Query, want) {
				t.Errorf("query %q lacks %s", r.Query, want)
			}
		}
	}
}

// A list the API expires mid-way starts again, dropping what it read, twice at most.
func TestSecretRefsRestartsAnExpiredList(t *testing.T) {
	const gone = `{"kind":"Status","reason":"Expired","code":410,"message":"the continue token expired"}`

	f := kubetest.New(t, map[string]string{})
	f.AnswerWith("GET "+refsTestPath, http.StatusGone, gone)

	var reads atomic.Int32

	f.BeforeAnswer = func(*http.Request) {
		if reads.Add(1) == 2 {
			f.AnswerWith("GET "+refsTestPath, http.StatusOK, refsTable("", helmRow("sh.helm.release.v1.site.v1", "site", 1, "deployed")))
		}
	}

	refs, err := SecretRefs(context.Background(), f.KubeClient(t), "web", "")
	if err != nil || len(refs) != 1 {
		t.Fatalf("%+v %v", refs, err)
	}

	f.AnswerWith("GET "+refsTestPath, http.StatusGone, gone)

	f.ForgetRequests()

	if _, err := SecretRefs(context.Background(), f.KubeClient(t), "web", ""); err == nil || !strings.Contains(err.Error(), "list expired") {
		t.Fatalf("got %v", err)
	}

	if n := len(f.Recorded()); n != refsExpiredRetries+1 {
		t.Fatalf("%d reads", n)
	}
}

func TestSecretRefsRefusals(t *testing.T) {
	f := kubetest.New(t, map[string]string{"GET " + refsTestPath: `{"kind":"SecretList","items":[]}`})

	if _, err := SecretRefs(context.Background(), f.KubeClient(t), "web", ""); err == nil || err.Error() != "the API server did not answer with a table" {
		t.Fatalf("not a table: %v", err)
	}

	f.AnswerWith("GET "+refsTestPath, http.StatusOK, "{")

	if _, err := SecretRefs(context.Background(), f.KubeClient(t), "web", ""); err == nil || !strings.HasPrefix(err.Error(), "decode Kubernetes API answer") {
		t.Fatalf("undecodable: %v", err)
	}

	f.AnswerWith("GET "+refsTestPath, http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"secrets is forbidden"}`)

	if _, err := SecretRefs(context.Background(), f.KubeClient(t), "web", ""); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("refused: %v", err)
	}
}

// Rows that are not revisions of a release are skipped; a missing modifiedAt falls back to
// the Secret's creation.
func TestSecretRefsSkipsOtherRows(t *testing.T) {
	f := kubetest.New(t, map[string]string{"GET " + refsTestPath: refsTable("",
		`{"object":{"metadata":{"name":"no-version","labels":{"name":"site"}}}}`,
		`{"object":{"metadata":{"name":"no-name","labels":{"version":"1"}}}}`,
		`{"object":{"metadata":{"name":"old","creationTimestamp":"2026-01-01T00:00:00Z","labels":{"name":"site","version":"3"}}}}`,
	)})

	refs, err := SecretRefs(context.Background(), f.KubeClient(t), "web", "")
	if err != nil || len(refs) != 1 || refs[0].Secret != "old" || refs[0].Modified != 1767225600 {
		t.Fatalf("%+v %v", refs, err)
	}
}
