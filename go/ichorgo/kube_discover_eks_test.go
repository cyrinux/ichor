package ichorgo

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// ssoDiscoverySecrets is IAM Identity Center as the discovery card sends it (invented names).
func ssoDiscoverySecrets(extra map[string]string) map[string]string {
	s := map[string]string{awsFieldSSOStartURL: "https://example.awsapps.com/start", awsFieldSSORegion: "eu-west-3", awsFieldRegions: "eu-west-3, eu-north-1"}
	for k, v := range extra {
		s[k] = v
	}

	return s
}

// withFakeEKS serves EKS in every region at <srv>/<region>: eu-north-1 refuses; in
// eu-west-3, ReadOnly sees demo-eks then (second page) shop, Admin sees demo-eks and
// admin-only; prod (Admin) points at server. Every request must be signed by a role's key
// for the region of the path.
func withFakeEKS(t *testing.T, server string) {
	t.Helper()

	srv := withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		region, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		auth := r.Header.Get("Authorization")

		if region == "eu-north-1" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"User is not authorized to perform: eks:ListClusters"}`)

			return
		}

		if !strings.Contains(auth, "/"+region+"/eks/aws4_request") {
			w.WriteHeader(http.StatusForbidden)

			return
		}

		admin := strings.Contains(auth, "Credential=ASIAADMIN/")
		if !admin && !strings.Contains(auth, "Credential=ASIAREADONLY/") {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		switch {
		case path == "clusters" && region != "eu-west-3":
			_, _ = io.WriteString(w, `{"clusters":[]}`)
		case path == "clusters" && admin:
			_, _ = io.WriteString(w, `{"clusters":["demo-eks","admin-only","prod"]}`)
		case path == "clusters" && r.URL.Query().Get("nextToken") == "" && r.URL.Query().Get("maxResults") == "100":
			_, _ = io.WriteString(w, `{"clusters":["demo-eks"],"nextToken":"n2"}`)
		case path == "clusters" && r.URL.Query().Get("nextToken") == "n2":
			_, _ = io.WriteString(w, `{"clusters":["shop"]}`)
		case strings.HasPrefix(path, "clusters/"):
			name := strings.TrimPrefix(path, "clusters/")

			endpoint := "https://" + name + ".gr7." + region + ".eks.amazonaws.com"
			if name == "prod" {
				endpoint = server
			}

			_ = json.NewEncoder(w).Encode(map[string]any{"cluster": map[string]any{"endpoint": endpoint, "certificateAuthority": map[string]string{"data": "Q0E="}}})
		default:
			http.NotFound(w, r)
		}
	})

	old := eksEndpoint
	eksEndpoint = func(region string) string { return srv.URL + "/" + region }

	t.Cleanup(func() { eksEndpoint = old })
}

// eksDiscoverSignIn runs the discovery's Identity Center sign-in (the device code approves
// itself in the fake) and returns its error message.
func eksDiscoverSignIn(t *testing.T, secrets map[string]string) string {
	t.Helper()

	old := deviceMinInterval
	deviceMinInterval = 10 * time.Millisecond

	t.Cleanup(func() { deviceMinInterval = old })

	raw, _ := json.Marshal(secrets)
	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	StartDiscoverSignIn(discoverEKS, string(raw), rec)

	select {
	case p := <-rec.prompts:
		if p.Kind != "device" || p.UserCode == "" {
			t.Fatalf("prompt %+v", p)
		}
	case msg := <-rec.done:
		t.Fatalf("no prompt, done with %q", msg)
	case <-time.After(15 * time.Second):
		t.Fatal("no prompt")
	}

	select {
	case msg := <-rec.done:
		return msg
	case <-time.After(15 * time.Second):
		t.Fatal("sign-in did not end")
	}

	return ""
}

func eksDiscoverErr(secrets map[string]string) error {
	raw, _ := json.Marshal(secrets)
	_, err := DiscoverClusters(discoverEKS, string(raw))

	return err
}

func TestKubeDiscoverOptionsEKS(t *testing.T) {
	raw, err := KubeDiscoverOptions()
	if err != nil {
		t.Fatal(err)
	}

	var opts map[string][][]string
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		t.Fatal(err)
	}

	eks := opts[discoverEKS]
	if len(eks) != 2 || !slices.Equal(eks[0], []string{awsFieldSSOStartURL, awsFieldSSORegion, awsFieldRegions}) || !slices.Contains(eks[1], awsFieldAccessKey) {
		t.Fatalf("EKS options %v", eks)
	}

	if len(opts[discoverGKE]) == 0 {
		t.Error("GKE options gone")
	}
}

// Sign in once, pick two roles: their clusters in every region given, each once, a refusing
// region skipped and reported.
func TestDiscoverEKSWithIdentityCenter(t *testing.T) {
	aws := newFakeAWS(t)
	aws.perRoleKeys = true
	withFakeEKS(t, "https://prod.example")
	t.Cleanup(ForgetDiscoverSignIn)

	if msg := eksDiscoverSignIn(t, ssoDiscoverySecrets(nil)); msg != "" {
		t.Fatalf("sign-in: %s", msg)
	}

	// Three roles to pick from (two accounts).
	raw, err := DiscoverSignInChoices()
	if err != nil {
		t.Fatal(err)
	}

	var choices []awsSSORole
	if err := json.Unmarshal([]byte(raw), &choices); err != nil || len(choices) != 3 || choices[0].AccountName == "" {
		t.Fatalf("choices %s (%v)", raw, err)
	}

	// Nothing picked: the role is asked for.
	if err := eksDiscoverErr(ssoDiscoverySecrets(nil)); err == nil || !strings.Contains(err.Error(), "choose an account and role") {
		t.Fatalf("no role: %v", err)
	}

	s := discoverWith(t, discoverEKS, ssoDiscoverySecrets(map[string]string{awsFieldRoleSelection: "123456789012/ReadOnly, 123456789012/Admin, 210987654321/ReadOnly"}))
	if got := contextNames(s); got != "admin-only.eu-west-3.eks,demo-eks.eu-west-3.eks,prod.eu-west-3.eks,shop.eu-west-3.eks" {
		t.Fatalf("contexts %s", got)
	}

	for _, c := range s.Contexts {
		if c.Auth != authEKS {
			t.Errorf("%s signs in with %q", c.Name, c.Auth)
		}
	}

	p := readDiscoverProgress(t)

	if p.Projects != 4 || p.Scanned != 4 || !slices.Equal(p.Refused, []string{"eu-north-1"}) {
		t.Errorf("progress %+v", p)
	}

	// Each cluster found is named once (demo-eks is found by two roles), no region still read.
	slices.Sort(p.Found)

	if len(p.Current) != 0 || strings.Join(p.Found, ",") != "admin-only.eu-west-3.eks,demo-eks.eu-west-3.eks,prod.eu-west-3.eks,shop.eu-west-3.eks" {
		t.Errorf("current %v, found %v", p.Current, p.Found)
	}

	// demo-eks was found by ReadOnly first: that is the role it signs in with.
	discoverySignIn.mu.Lock()
	role := discoverySignIn.eksRoles["https://demo-eks.gr7.eu-west-3.eks.amazonaws.com"]
	discoverySignIn.mu.Unlock()

	if role.RoleName != "ReadOnly" || role.AccountName != "Production" {
		t.Errorf("demo-eks role %+v", role)
	}

	// Only refusing regions: an error naming them.
	only := ssoDiscoverySecrets(map[string]string{awsFieldRegions: "eu-north-1", awsFieldRoleSelection: "123456789012/Admin"})
	if err := eksDiscoverErr(only); err == nil || !strings.Contains(err.Error(), "no access in eu-north-1") {
		t.Errorf("refused everywhere: %v", err)
	}
}

func TestDiscoverEKSNeedsSignInFirst(t *testing.T) {
	newFakeAWS(t)
	withFakeEKS(t, "https://prod.example")
	t.Cleanup(ForgetDiscoverSignIn)

	err := eksDiscoverErr(ssoDiscoverySecrets(map[string]string{awsFieldRoleSelection: "123456789012/Admin"}))
	if err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), "before finding clusters") {
		t.Fatalf("without a sign-in: %v", err)
	}

	// A sign-in for another start URL is not used.
	if msg := eksDiscoverSignIn(t, ssoDiscoverySecrets(nil)); msg != "" {
		t.Fatal(msg)
	}

	other := ssoDiscoverySecrets(map[string]string{awsFieldSSOStartURL: "https://other.awsapps.com/start", awsFieldRoleSelection: "123456789012/Admin"})
	if err := eksDiscoverErr(other); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Errorf("another start URL: %v", err)
	}
}

// One role only: the sign-in takes it, nothing to pick, and the discovery reads with it.
func TestDiscoverEKSWithTheOnlyRole(t *testing.T) {
	aws := newFakeAWS(t)
	aws.singleRole, aws.perRoleKeys = true, true
	withFakeEKS(t, "https://prod.example")
	t.Cleanup(ForgetDiscoverSignIn)

	if msg := eksDiscoverSignIn(t, ssoDiscoverySecrets(nil)); msg != "" {
		t.Fatal(msg)
	}

	if raw, err := DiscoverSignInChoices(); err != nil || raw != "" {
		t.Fatalf("choices %q (%v)", raw, err)
	}

	s := discoverWith(t, discoverEKS, ssoDiscoverySecrets(map[string]string{awsFieldRegions: "eu-west-3"}))
	if got := contextNames(s); got != "demo-eks.eu-west-3.eks,shop.eu-west-3.eks" {
		t.Fatalf("contexts %s", got)
	}
}

// The clusters imported after the discovery sign in with its session and the role each was
// found with: one device code in all.
func TestImportedEKSClustersReuseTheDiscoverySignIn(t *testing.T) {
	withAuthStore(t)

	aws := newFakeAWS(t)
	aws.perRoleKeys = true
	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})
	withFakeEKS(t, api.URL)
	t.Cleanup(ForgetDiscoverSignIn)

	if msg := eksDiscoverSignIn(t, ssoDiscoverySecrets(nil)); msg != "" {
		t.Fatal(msg)
	}

	secrets := ssoDiscoverySecrets(map[string]string{awsFieldRoleSelection: "123456789012/ReadOnly 123456789012/Admin"})
	discoverWith(t, discoverEKS, secrets)

	stored, err := MergeKubeconfig("", "", eksKubeconfig(api), "")
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(secrets)
	if err := KubeSetCredentials(stored, "admin@test", string(raw)); err != nil {
		t.Fatalf("set credentials: %v", err)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	// prod is only Admin's: signed with Admin's key.
	if u := presignedFrom(t, api); !strings.HasPrefix(u.Query().Get("X-Amz-Credential"), "ASIAADMIN/") {
		t.Errorf("credential %s", u.Query().Get("X-Amz-Credential"))
	}

	aws.mu.Lock()
	defer aws.mu.Unlock()

	if aws.devices != 1 {
		t.Errorf("device codes %d", aws.devices)
	}
}

func TestNamedEKSClustersAcrossAccounts(t *testing.T) {
	t.Cleanup(ForgetDiscoverSignIn)

	a, b := awsSSORole{AccountID: "123456789012", RoleName: "ReadOnly"}, awsSSORole{AccountID: "210987654321", RoleName: "ReadOnly"}
	got := namedEKSClusters([]eksFoundCluster{
		{role: a, name: "demo-eks", region: "eu-west-3", server: "https://a"},
		{role: b, name: "demo-eks", region: "eu-west-3", server: "https://b"},
		{role: b, name: "seen-twice", region: "eu-west-3", server: "https://a"},
	})

	names := make([]string, 0, len(got))
	for _, c := range got {
		names = append(names, c.name)
	}

	if !slices.Equal(names, []string{"demo-eks.eu-west-3.123456789012.eks", "demo-eks.eu-west-3.210987654321.eks"}) {
		t.Fatalf("names %v", names)
	}

	if args := got[0].user.User.Exec.Args; !slices.Equal(args, []string{"--region", "eu-west-3", "eks", "get-token", "--cluster-name", "demo-eks"}) {
		t.Errorf("exec args %v", args)
	}
}

func TestEKSDiscoveryRegions(t *testing.T) {
	for _, tc := range []struct {
		raw, sso string
		first    string
		count    int
		bad      bool
	}{
		{raw: "", sso: "eu-west-3", first: "eu-west-3", count: len(eksCommonRegions)},
		{raw: "", sso: "ap-east-1", first: "ap-east-1", count: len(eksCommonRegions) + 1},
		{raw: "", sso: "cn-north-1", first: "cn-north-1", count: 2},
		{raw: "us-east-1, eu-west-1 us-east-1", sso: "eu-west-3", first: "us-east-1", count: 2},
		{raw: "eu-west-1;drop", sso: "eu-west-3", bad: true},
	} {
		got, err := eksDiscoveryRegions(tc.raw, tc.sso)
		if tc.bad {
			if err == nil {
				t.Errorf("%q: no error", tc.raw)
			}

			continue
		}

		if err != nil || len(got) != tc.count || got[0] != tc.first {
			t.Errorf("%q/%s: %v (%v)", tc.raw, tc.sso, got, err)
		}
	}

}
