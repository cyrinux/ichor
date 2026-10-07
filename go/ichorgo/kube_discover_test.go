package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func discoverWith(t *testing.T, provider string, secrets map[string]string) kubeconfigSummary {
	t.Helper()

	raw, _ := json.Marshal(secrets)

	yaml, err := DiscoverClusters(provider, string(raw))
	if err != nil {
		t.Fatal(err)
	}

	return parseKubeSummary(t, yaml)
}

func TestDiscoverEKS(t *testing.T) {
	srv := withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "/eu-west-3/eks/aws4_request") {
			w.WriteHeader(http.StatusForbidden)

			return
		}

		switch r.URL.Path {
		case "/clusters":
			_, _ = io.WriteString(w, `{"clusters":["prod"]}`)
		case "/clusters/prod":
			_, _ = io.WriteString(w, `{"cluster":{"endpoint":"https://ABC.gr7.eu-west-3.eks.amazonaws.com","certificateAuthority":{"data":"Q0E="}}}`)
		}
	})

	old := eksEndpoint
	eksEndpoint = func(string) string { return srv.URL }

	t.Cleanup(func() { eksEndpoint = old })

	s := discoverWith(t, discoverEKS, map[string]string{awsFieldRegion: "eu-west-3", awsFieldAccessKey: "AKID", awsFieldSecretKey: "s"})
	if len(s.Contexts) != 1 || s.Contexts[0].Name != "prod.eu-west-3.eks" || s.Contexts[0].Auth != authEKS || s.Contexts[0].AuthDetail != "prod" {
		t.Fatalf("contexts %+v", s.Contexts)
	}
}

func TestDiscoverDigitalOcean(t *testing.T) {
	withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/kubernetes/clusters":
			_, _ = io.WriteString(w, `{"kubernetes_clusters":[{"id":"c1","name":"shop","region":"fra1","endpoint":"https://c1.k8s.ondigitalocean.com"}]}`)
		case "/v2/kubernetes/clusters/c1/credentials":
			_, _ = io.WriteString(w, `{"certificate_authority_data":"Q0E=","token":"t"}`)
		}
	})

	s := discoverWith(t, discoverDigitalOcean, map[string]string{doFieldToken: "dop"})
	if len(s.Contexts) != 1 || s.Contexts[0].Name != "shop.fra1.do" || s.Contexts[0].Auth != authDigitalOcean {
		t.Fatalf("contexts %+v", s.Contexts)
	}
}

func TestDiscoverRancher(t *testing.T) {
	var srvURL string

	srv := withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-ab:cd" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		switch {
		case r.URL.Path == "/v3/clusters":
			_, _ = io.WriteString(w, `{"data":[{"id":"c-1","name":"edge"},{"id":"local","name":"local"}]}`)
		case r.URL.Query().Get("action") == "generateKubeconfig":
			id := strings.TrimPrefix(r.URL.Path, "/v3/clusters/")
			cfg := fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: %[1]s\n  cluster: {server: %[2]q}\nusers:\n- name: %[1]s\n  user: {token: \"kubeconfig-u-x:y\"}\ncontexts:\n- name: %[1]s\n  context: {cluster: %[1]s, user: %[1]s}\ncurrent-context: %[1]s\n", id, srvURL+"/k8s/clusters/"+id)
			js, _ := json.Marshal(map[string]string{"config": cfg})
			_, _ = w.Write(js)
		}
	})
	srvURL = srv.URL

	s := discoverWith(t, discoverRancher, map[string]string{rancherFieldServer: srv.URL, rancherFieldKey: "token-ab:cd"})
	if len(s.Contexts) != 2 || s.Contexts[0].Name != "c-1" || s.Contexts[1].Name != "local" || s.Contexts[0].Problem != "" || s.Contexts[0].Auth != authToken {
		t.Fatalf("contexts %+v", s.Contexts)
	}
}

func TestDiscoverAKS(t *testing.T) {
	idp := withFakeEntra(t)

	kubeconfig := "apiVersion: v1\nkind: Config\nclusters:\n- name: aks1\n  cluster: {server: \"https://aks1.hcp.westeurope.azmk8s.io:443\"}\n" +
		"users:\n- name: u\n  user:\n    exec:\n      command: kubelogin\n      args: [get-token, --login, devicecode, --server-id, 6dae42f8, --client-id, 80faf920, --tenant-id, t1]\n" +
		"contexts:\n- name: aks1\n  context: {cluster: aks1, user: u}\n"

	arm := withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		switch {
		case strings.HasSuffix(r.URL.Path, "/managedClusters"):
			_, _ = io.WriteString(w, `{"value":[{"id":"/subscriptions/s1/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/aks1","name":"aks1"}]}`)
		case strings.HasSuffix(r.URL.Path, "/listClusterUserCredential"):
			fmt.Fprintf(w, `{"kubeconfigs":[{"name":"clusterUser","value":%q}]}`, base64.StdEncoding.EncodeToString([]byte(kubeconfig)))
		}
	})

	old := azureARM
	azureARM = arm.URL

	t.Cleanup(func() { azureARM = old })

	s := discoverWith(t, discoverAKS, map[string]string{azureFieldTenant: "t1", azureFieldSubscript: "s1", azureFieldClientID: "spn", azureFieldClientSecret: "x"})
	if len(s.Contexts) != 1 || s.Contexts[0].Name != "aks1" || s.Contexts[0].Auth != authAzure || s.Contexts[0].SignIn != authAzure {
		t.Fatalf("contexts %+v", s.Contexts)
	}

	if g := idp.grantList(); len(g) != 1 || g[0] != "client_credentials" {
		t.Errorf("grants %v", g)
	}
}

func TestDiscoverRejects(t *testing.T) {
	for provider, secrets := range map[string]string{
		"nope":               `{}`,
		discoverEKS:          `{"awsRegion":"eu-west-3"}`,
		discoverDigitalOcean: `{}`,
		discoverRancher:      `{"rancherServer":"http://r","rancherApiKey":"token-a:b"}`,
		discoverAKS:          `{"azureTenantId":"t"}`,
		discoverGKE:          `{"gcpServiceAccountJson":"{}"}`,
	} {
		if _, err := DiscoverClusters(provider, secrets); err == nil {
			t.Errorf("%s: accepted", provider)
		}
	}
}
