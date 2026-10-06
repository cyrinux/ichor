package ichorgo

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnifiedDiffEqual(t *testing.T) {
	if d := unifiedDiff("a\nb\n", "a\nb\n", "live", "wanted"); d != "" {
		t.Fatalf("got %q", d)
	}
}

func TestUnifiedDiffOneChange(t *testing.T) {
	a := "1\n2\n3\n4\n5\n6\n7\n8\n9\n"
	b := "1\n2\n3\n4\nfive\n6\n7\n8\n9\n"

	want := "--- live\n+++ wanted\n@@ -2,7 +2,7 @@\n 2\n 3\n 4\n-5\n+five\n 6\n 7\n 8\n"
	if d := unifiedDiff(a, b, "live", "wanted"); d != want {
		t.Fatalf("got\n%s\nwant\n%s", d, want)
	}
}

func TestUnifiedDiffCreatedAndDeleted(t *testing.T) {
	if d := unifiedDiff("", "a\nb\n", "live", "wanted"); d != "--- live\n+++ wanted\n@@ -0,0 +1,2 @@\n+a\n+b\n" {
		t.Fatalf("created: %q", d)
	}

	if d := unifiedDiff("a\nb\n", "", "live", "wanted"); d != "--- live\n+++ wanted\n@@ -1,2 +0,0 @@\n-a\n-b\n" {
		t.Fatalf("deleted: %q", d)
	}
}

func TestUnifiedDiffSeparateHunks(t *testing.T) {
	var a, b []string
	for i := range 30 {
		a = append(a, fmt.Sprint(i))
		b = append(b, fmt.Sprint(i))
	}

	b[2], b[25] = "two", "twenty-five"

	d := unifiedDiff(strings.Join(a, "\n")+"\n", strings.Join(b, "\n")+"\n", "live", "wanted")
	if strings.Count(d, "@@ -") != 2 || !strings.Contains(d, "@@ -1,6 +1,6 @@") || !strings.Contains(d, "@@ -23,7 +23,7 @@") {
		t.Fatalf("got\n%s", d)
	}
}

func TestUnifiedDiffInsertionInMiddle(t *testing.T) {
	d := unifiedDiff("a\nb\nc\n", "a\nb\nx\ny\nc\n", "l", "w")
	if d != "--- l\n+++ w\n@@ -1,3 +1,5 @@\n a\n b\n+x\n+y\n c\n" {
		t.Fatalf("got %q", d)
	}
}

func TestUnifiedDiffTooLargeFallsBackToReplace(t *testing.T) {
	var a, b strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&a, "a%d\n", i)
		fmt.Fprintf(&b, "b%d\n", i)
	}

	d := unifiedDiff(a.String(), b.String(), "l", "w")
	if strings.Count(d, "\n-a") != 3000 || strings.Count(d, "\n+b") != 3000 {
		t.Fatal("expected every line removed then added")
	}
}

func TestDiffNormalizeDropsServerFields(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{
			"name": "x", "uid": "u", "resourceVersion": "9", "generation": 2, "creationTimestamp": "t",
			"managedFields": []any{map[string]any{"manager": "m"}},
			"annotations":   map[string]any{"kubectl.kubernetes.io/last-applied-configuration": "{}"},
		},
		"data":   map[string]any{"b": "2", "a": "1"},
		"status": map[string]any{"x": 1},
	}

	got := diffNormalize(obj, newKubeDiffMasker())
	want := "apiVersion: v1\ndata:\n  a: \"1\"\n  b: \"2\"\nkind: ConfigMap\nmetadata:\n  name: x\n"

	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}

	if _, ok := obj["status"]; !ok {
		t.Fatal("the object itself was changed")
	}
}

func TestDiffNormalizeRedactsSecrets(t *testing.T) {
	secret := func(data string) map[string]any {
		return map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "s"}, "data": map[string]any{"password": data}}
	}

	m := newKubeDiffMasker()
	a, b := diffNormalize(secret("aHVudGVyMg=="), m), diffNormalize(secret("aHVudGVyMw=="), m)
	if strings.Contains(a, "aHVudGVy") || !strings.Contains(a, diffSecretMask) {
		t.Fatalf("secret shown:\n%s", a)
	}

	if a == b {
		t.Fatal("a changed value must still show as a change")
	}

	plain := diffNormalize(map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "s"}, "stringData": map[string]any{"password": "hunter2"}}, m)
	// "aHVudGVyMg==" is "hunter2": both forms of the same value hash the same.
	i := strings.Index(a, diffSecretMask)
	if hash := a[i : i+len(diffSecretMask)+8]; strings.Contains(plain, "hunter2") || !strings.Contains(plain, hash) {
		t.Fatalf("stringData not redacted the same way:\n%s\n%s", plain, a)
	}
}

func TestDiffObjects(t *testing.T) {
	cm := func(v string) map[string]any {
		return map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "x"}, "data": map[string]any{"token": v}}
	}

	masker := newKubeDiffMasker()
	masker.add("s3cr3t-value")
	masker.add("ok") // too short to mask

	var r kubeDiffResource

	diffObjects(&r, cm("old"), cm("s3cr3t-value"), masker)
	if r.Change != diffChangeChanged || strings.Contains(r.Diff, "s3cr3t") || !strings.Contains(r.Diff, "+  token: "+diffMaskedValue) {
		t.Fatalf("changed: %+v", r)
	}

	r = kubeDiffResource{}
	if diffObjects(&r, cm("a"), cm("a"), masker); r.Change != diffChangeUnchanged || r.Diff != "" {
		t.Fatalf("unchanged: %+v", r)
	}

	r = kubeDiffResource{}
	if diffObjects(&r, nil, cm("a"), masker); r.Change != diffChangeCreated || !strings.HasPrefix(r.Diff, "--- live\n+++ wanted\n@@ -0,0 ") {
		t.Fatalf("created: %+v", r)
	}

	r = kubeDiffResource{}
	if diffObjects(&r, cm("a"), nil, masker); r.Change != diffChangeDeleted {
		t.Fatalf("deleted: %+v", r)
	}
}

func TestDiffObjectsTruncates(t *testing.T) {
	big := map[string]any{}
	for i := range 4000 {
		big[fmt.Sprintf("key%05d", i)] = strings.Repeat("v", 30)
	}

	var r kubeDiffResource

	diffObjects(&r, nil, map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "data": big}, newKubeDiffMasker())
	if !r.Truncated || len(r.Diff) > diffMaxBytes || !strings.HasSuffix(r.Diff, "\n") {
		t.Fatalf("truncated=%v len=%d", r.Truncated, len(r.Diff))
	}
}

func TestSortDiffResources(t *testing.T) {
	rs := []kubeDiffResource{
		{Kind: "Service", Name: "b", Change: diffChangeUnchanged},
		{Kind: "Deployment", Name: "a", Change: diffChangeChanged},
		{Kind: "ConfigMap", Name: "c", Change: diffChangeCreated},
		{Kind: "Service", Name: "a", Change: diffChangeUnchanged},
		{Kind: "Job", Name: "x", Change: diffChangeError},
	}

	sortDiffResources(rs)

	var got []string
	for _, r := range rs {
		got = append(got, r.Kind+"/"+r.Name)
	}

	if strings.Join(got, " ") != "Job/x ConfigMap/c Deployment/a Service/a Service/b" {
		t.Fatalf("got %v", got)
	}
}

func TestDiffNormalizeSecretDigestIsKeyed(t *testing.T) {
	secret := map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "s"}, "data": map[string]any{"pin": "MTIzNA=="}}

	first, second := diffNormalize(secret, newKubeDiffMasker()), diffNormalize(secret, newKubeDiffMasker())
	if first == second {
		t.Fatal("the same digest in two diffs: a short value could be looked up")
	}
}

func TestMaskerHidesMultilineQuotedAndNestedValues(t *testing.T) {
	m := newKubeDiffMasker()
	m.add("abcdef")
	m.add("abcdefghij")
	m.add("-----BEGIN KEY-----\nline-two-secret\n-----END KEY-----")
	m.add("it's: a secret")

	obj := map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "x"}, "data": map[string]any{
		"long":  "abcdefghij",
		"short": "abcdef",
		"pem":   "-----BEGIN KEY-----\nline-two-secret\n-----END KEY-----",
		"quote": "it's: a secret",
	}}

	got := diffNormalize(obj, m)
	for _, leak := range []string{"ghij", "line-two", "a secret", "abcdef"} {
		if strings.Contains(got, leak) {
			t.Errorf("%q shows in\n%s", leak, got)
		}
	}

	if data, _ := obj["data"].(map[string]any); data["long"] != "abcdefghij" {
		t.Fatal("the object itself was changed")
	}
}
