package ichorgo

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// signInImportYAML is a kubeconfig as a GKE import stores it (invented names): three clusters
// that sign in with the account's credentials and one with a static token.
func signInImportYAML(t *testing.T) string {
	t.Helper()

	var static kubeStoreUser
	static.User.Token = "static-token"

	yaml, err := discoveredKubeconfig([]discoveredCluster{
		{name: "web.europe-west1.gke", server: "https://192.0.2.10", user: execUser("gke-gcloud-auth-plugin")},
		{name: "api.europe-west1.gke", server: "https://192.0.2.11", user: execUser("gke-gcloud-auth-plugin")},
		{name: "jobs.europe-west1.gke", server: "https://192.0.2.12", user: execUser("gke-gcloud-auth-plugin")},
		{name: "lab", server: "https://192.0.2.13", user: static},
	})
	if err != nil {
		t.Fatal(err)
	}

	return yaml
}

// withSignInOne replaces the per-cluster sign-in for the test.
func withSignInOne(t *testing.T, fake func(storedYAML, contextName, secretsJSON string) error) {
	t.Helper()

	prev := signInOne
	signInOne = fake

	t.Cleanup(func() { signInOne = prev })
}

func TestSignInDiscovered(t *testing.T) {
	yaml := signInImportYAML(t)

	var (
		signed   []string
		progress []string
	)

	withSignInOne(t, func(_, name, secrets string) error {
		raw, _ := SignInProgress()
		progress = append(progress, raw)
		signed = append(signed, name)

		switch name {
		case "api.europe-west1.gke":
			return errors.New("the key was refused")
		case "jobs.europe-west1.gke":
			// Needs the browser: left to the user, not a failure.
			return signInRequired(authGKE, "")
		}

		return nil
	})

	out, err := SignInDiscovered(yaml, `["web.europe-west1.gke","api.europe-west1.gke","jobs.europe-west1.gke","lab","gone"]`, `{"k":"v"}`)
	if err != nil {
		t.Fatal(err)
	}

	// The static-token context and the unknown one are not signed in.
	if strings.Join(signed, ",") != "web.europe-west1.gke,api.europe-west1.gke,jobs.europe-west1.gke" {
		t.Fatalf("signed in %v", signed)
	}

	var res struct{ Failed []signInFailure }
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Failed) != 1 ||
		res.Failed[0] != (signInFailure{Name: "api.europe-west1.gke", Error: "the key was refused"}) {
		t.Fatalf("result %s (%v)", out, err)
	}

	var second signInProgressView
	if err := json.Unmarshal([]byte(progress[1]), &second); err != nil ||
		!second.Running || second.Done != 1 || second.Total != 3 || second.Current != "api.europe-west1.gke" || len(second.Failed) != 0 {
		t.Fatalf("while signing in api: %s (%v)", progress[1], err)
	}

	last := readSignInProgress(t)
	if last.Running || last.Done != 3 || last.Total != 3 || last.Current != "" || strings.Join(last.Failed, ",") != "api.europe-west1.gke" {
		t.Fatalf("after: %+v", last)
	}
}

func TestSignInDiscoveredBadNames(t *testing.T) {
	if _, err := SignInDiscovered(signInImportYAML(t), "web", "{}"); err == nil {
		t.Fatal("names must be a JSON array")
	}
}

// The context names it reports go through the privacy mask.
func TestSignInProgressMasked(t *testing.T) {
	enableMask(t, "")

	yaml := signInImportYAML(t)

	withSignInOne(t, func(_, _, _ string) error { return errors.New("refused") })

	out, err := SignInDiscovered(yaml, `["web.europe-west1.gke"]`, "{}")
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := SignInProgress()
	if strings.Contains(out+raw, "web.europe-west1") {
		t.Fatalf("not masked: %s %s", out, raw)
	}

	// The masked name the app shows signs the real context in.
	var res struct{ Failed []signInFailure }
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Failed) != 1 {
		t.Fatalf("result %s (%v)", out, err)
	}

	var got string

	withSignInOne(t, func(_, name, _ string) error {
		got = name

		return nil
	})

	names, _ := json.Marshal([]string{res.Failed[0].Name})
	if _, err := SignInDiscovered(yaml, string(names), "{}"); err != nil || got != "web.europe-west1.gke" {
		t.Fatalf("masked name signed in %q (%v)", got, err)
	}
}

type signInProgressView struct {
	Running     bool
	Done, Total int
	Current     string
	Failed      []string
}

func readSignInProgress(t *testing.T) signInProgressView {
	t.Helper()

	raw, err := SignInProgress()
	if err != nil {
		t.Fatal(err)
	}

	var p signInProgressView
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}

	return p
}
