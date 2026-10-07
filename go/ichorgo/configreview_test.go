package ichorgo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Cases found by reviewing the config edit path: each was a way for a secret to end up in
// the wrong place, or for a try to be misreported.

func TestRestoreSecretsRefusesAnchors(t *testing.T) {
	node := newFakeConfigNode(t)

	snap, err := node.current(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// An alias of the masked token would publish the real one as a node label.
	draft := strings.Replace(snap.redacted, "token: '"+redacted+"'", "token: &t '"+redacted+"'", 1)
	draft = strings.Replace(draft, "machine:\n", "machine:\n    nodeLabels:\n        leak: *t\n", 1)

	if draft == snap.redacted || !strings.Contains(draft, "&t") || !strings.Contains(draft, "*t") {
		t.Fatal("the fixture did not place the anchor and its alias")
	}

	plan, err := planConfigChange(snap, snap.redacted, draft)
	if err == nil {
		t.Fatalf("an alias of a secret must be refused; diff:\n%s", unifiedDiff(plan.before, plan.after, "a", "b"))
	}

	for name, y := range map[string]string{
		"anchor only": "a: &x 1\n",
		"alias":       "a: &x 1\nb: *x\n",
		"merge key":   "a: &x {k: v}\nb:\n    <<: *x\n",
	} {
		if _, err := restoreSecrets("a: 1\n", "a: 1\n", y); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestRestoreSecretsNeverGuessesBetweenItems(t *testing.T) {
	real := "peers:\n    - name: a\n      key: secret-a\n    - name: b\n      key: secret-b\n"
	base := strings.NewReplacer("secret-a", "'******'", "secret-b", "'******'").Replace(real)

	// Swapped and both edited: nothing says which is which.
	swapped := "peers:\n    - name: b\n      mtu: 2\n      key: '******'\n    - name: a\n      mtu: 2\n      key: '******'\n"
	if got, err := restoreSecrets(real, base, swapped); err == nil {
		t.Fatalf("two edited items must not be paired by position, got:\n%s", got)
	}

	// Both edited in place is refused too: it cannot be told from the swap above.
	both := "peers:\n    - name: a\n      mtu: 2\n      key: '******'\n    - name: b\n      mtu: 2\n      key: '******'\n"
	if _, err := restoreSecrets(real, base, both); err == nil {
		t.Fatal("two edited items holding secrets must be refused")
	}

	// Secrets that look the same once redacted: the list must keep its shape.
	realKeys := "keys:\n    - s1\n    - s2\n    - s3\n"
	baseKeys := "keys:\n    - '******'\n    - '******'\n    - '******'\n"

	if got, err := restoreSecrets(realKeys, baseKeys, baseKeys); err != nil || got != realKeys {
		t.Fatalf("an untouched list keeps its secrets: %q %v", got, err)
	}

	for name, draft := range map[string]string{
		"one removed": "keys:\n    - '******'\n    - '******'\n",
		"one added":   baseKeys + "    - new\n",
	} {
		if got, err := restoreSecrets(realKeys, baseKeys, draft); err == nil {
			t.Errorf("%s: secrets that cannot be told apart must not be guessed, got:\n%s", name, got)
		}
	}
}

func TestRestoreSecretsGuardsEveryShape(t *testing.T) {
	real := "machine:\n    token: tok-1\n    type: worker\n"
	base := "machine:\n    token: '******'\n    type: worker\n"

	for name, draft := range map[string]string{
		"secret replaced by a list":    "machine:\n    token: []\n    type: worker\n",
		"secret replaced by a map":     "machine:\n    token:\n        k: v\n    type: worker\n",
		"padded mask in a plain field": "machine:\n    token: '******'\n    type: ' ****** '\n",
		"block mask in a plain field":  "machine:\n    token: '******'\n    type: |\n        ******\n",
	} {
		if got, err := restoreSecrets(real, base, draft); err == nil {
			t.Errorf("%s must be refused, got:\n%s", name, got)
		}
	}
}

func TestEditWritesNumbersAsTyped(t *testing.T) {
	value := func(typ, in string) string {
		t.Helper()

		out := edit(t, "a: x\n", `{"doc":0,"path":["a"],"op":"set","type":"`+typ+`","value":"`+in+`"}`)

		return describeConfig(out, nil).Documents[0].Node.Children[0].Value
	}

	// YAML reads 010 as octal: what was typed in base 10 is written in base 10.
	if got := value("integer", "010"); got != "10" {
		t.Errorf("010 was written as %q", got)
	}

	if got := value("number", "1.50"); got != "1.5" {
		t.Errorf("1.50 was written as %q", got)
	}

	for _, bad := range []string{"NaN", "Inf", "-Inf", "abc"} {
		if _, err := MachineConfigEdit("a: x\n", `{"doc":0,"path":["a"],"op":"set","type":"number","value":"`+bad+`"}`); err == nil {
			t.Errorf("%s is not a number", bad)
		}
	}

	// Text of unknown type is kept whole, even where YAML would cut or fold it.
	for _, text := range []string{"x # y", `line1\nline2`} {
		out, err := MachineConfigEdit("a: x\n", `{"doc":0,"path":["a"],"op":"set","type":"any","value":"`+text+`"}`)
		if err != nil {
			t.Errorf("%q: %v", text, err)

			continue
		}

		want := strings.ReplaceAll(text, `\n`, "\n")
		if got := describeConfig(out, nil).Documents[0].Node.Children[0]; got.Value != want || got.Type != configTypeString {
			t.Errorf("%q became %q (%s)", want, got.Value, got.Type)
		}
	}

	if got := describeConfig(edit(t, "a: x\n", `{"doc":0,"path":["a"],"op":"set","type":"any","value":"true"}`), nil).Documents[0].Node.Children[0]; got.Type != configTypeBoolean {
		t.Errorf("true typed as any is a boolean, got %s", got.Type)
	}
}

func TestSchemaCachedDoesNotWaitForADownload(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}
		<-release
		_, _ = w.Write([]byte(testSchema)) //nolint:errcheck
	}))
	defer srv.Close()
	defer close(release)

	store := testSchemaStore(srv, t.TempDir())

	go store.prepare(context.Background(), "v1.13.6") //nolint:errcheck

	<-started

	done := make(chan struct{})

	go func() {
		store.cached("v1.14.0")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("describing a config waited for the download of another schema")
	}
}

func TestTryWhoseAnswerIsLostGoesOn(t *testing.T) {
	h := newTryHarness(t)
	h.node.tryErr = errors.New("context deadline exceeded")
	h.node.tryApplies = true
	h.commands <- tryCommandKeep

	outcome, err := h.run(context.Background(), renamed(t), 300)
	if err != nil || outcome != tryOutcomeKept {
		t.Fatalf("a try the node applied without answering can still be kept: %q %v", outcome, err)
	}

	if strings.Join(h.phases, ",") != "applying,trying,keeping" || len(h.messages) != 1 || !strings.Contains(h.messages[0], "reverts by itself") {
		t.Fatalf("phases %v messages %v", h.phases, h.messages)
	}
}

func TestTryTheNodeRefusedIsAnError(t *testing.T) {
	h := newTryHarness(t)
	h.node.tryErr = errors.New("invalid config")

	if outcome, err := h.run(context.Background(), renamed(t), 300); err == nil || !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("a refused try is an error: %q %v", outcome, err)
	}

	if strings.Join(h.phases, ",") != "applying" {
		t.Fatalf("phases %v", h.phases)
	}
}
