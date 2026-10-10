package ichorgo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Cloud discovery: with an account's credentials, list its clusters and write a kubeconfig
// for them, signing in the way each cloud's CLI would (aws eks get-token, the GKE plugin,
// kubelogin, doctl); Rancher hands out ready kubeconfigs. The app shows the result in the
// usual import preview, then signs the added contexts in with the same credentials.

const (
	discoverEKS          = "eks"
	discoverGKE          = "gke"
	discoverAKS          = "aks"
	discoverDigitalOcean = "digitalocean"
	discoverRancher      = "rancher"

	// Fields only discovery asks for.
	awsFieldRegion      = "awsRegion"
	azureFieldTenant    = "azureTenantId"
	azureFieldSubscript = "azureSubscriptionId"
	rancherFieldServer  = "rancherServer"
)

// Discovery endpoints, overridable in tests.
var (
	eksEndpoint = func(region string) string {
		return "https://eks." + region + ".amazonaws.com" + awsDomainSuffix(region)
	}
	gkeEndpoint     = "https://container.googleapis.com"
	azureARM        = "https://management.azure.com"
	azureARMVersion = "2024-05-01"
)

// discoveredCluster is one cluster to write into the kubeconfig.
type discoveredCluster struct {
	name, server, caData string
	user                 kubeStoreUser
}

// KubeDiscoverFields lists, as JSON, the fields DiscoverClusters asks for per provider.
func KubeDiscoverFields() (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(map[string][]string{
		discoverEKS:          {awsFieldRegion, awsFieldAccessKey, awsFieldSecretKey, awsFieldSessionToken},
		discoverGKE:          {gcpFieldServiceAccount},
		discoverAKS:          {azureFieldTenant, azureFieldSubscript, azureFieldClientID, azureFieldClientSecret},
		discoverDigitalOcean: {doFieldToken},
		discoverRancher:      {rancherFieldServer, rancherFieldKey},
	})
}

// KubeDiscoverOptions lists, as JSON, the providers whose discovery takes one of several
// credentials, each a set of fields. GKE, in this order: a service account key; gcloud user
// credentials; the organisation's OAuth client (ID, secret, redirect URL); "Sign in with
// Google" (gcpGoogleSignIn, a marker the apps draw as a button, never a text field; only when
// the build registered a Google client). Every set but the key's ends with the optional
// project IDs. The OAuth client and Google sign-in on iOS sign in with StartDiscoverSignIn
// first. A provider absent from it has the one set KubeDiscoverFields lists.
func KubeDiscoverOptions() (out string, err error) {
	defer maskResult(&out, &err)

	var gke [][]string

	for _, set := range (gkeMethod{}).fieldSets() {
		if !slices.Contains(set, gcpFieldServiceAccount) {
			set = append(slices.Clone(set), gcpFieldProjects)
		}

		gke = append(gke, set)
	}

	return toJSON(map[string][][]string{discoverGKE: gke})
}

// DiscoverClusters lists the clusters of a cloud account (provider: eks, gke, aks,
// digitalocean, rancher; secretsJSON: the fields KubeDiscoverFields names) and returns a
// kubeconfig of them, for the import preview. Context names say the cloud and the region.
func DiscoverClusters(provider, secretsJSON string) (out string, err error) {
	// The result is a kubeconfig the user imports (Rancher's carry tokens): only the error
	// is masked.
	defer maskErr(&err)

	secrets, err := discoverySecrets(secretsJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*callTimeout)
	defer cancel()

	defer startDiscoveryProgress()()

	var clusters []discoveredCluster

	switch provider {
	case discoverEKS:
		clusters, err = discoverEKSClusters(ctx, secrets)
	case discoverGKE:
		clusters, err = discoverGKEClusters(ctx, secrets)
	case discoverAKS:
		return discoverAKSClusters(ctx, secrets)
	case discoverDigitalOcean:
		clusters, err = discoverDOClusters(ctx, secrets)
	case discoverRancher:
		return discoverRancherClusters(ctx, secrets)
	default:
		return "", fmt.Errorf("unknown provider %q", provider)
	}

	if err != nil {
		return "", err
	}

	return discoveredKubeconfig(clusters)
}

func discoveredKubeconfig(clusters []discoveredCluster) (string, error) {
	if len(clusters) == 0 {
		return "", errors.New("no cluster found with these credentials")
	}

	doc := emptyKubeconfigDoc()

	for _, c := range clusters {
		var cluster kubeStoreCluster
		cluster.Name = c.name
		cluster.Cluster.Server = c.server
		cluster.Cluster.CertificateAuthorityData = c.caData

		user := c.user
		user.Name = c.name

		var x kubeStoreContext
		x.Name = c.name
		x.Context.Cluster, x.Context.User = c.name, c.name

		doc.Clusters = append(doc.Clusters, cluster)
		doc.Users = append(doc.Users, user)
		doc.Contexts = append(doc.Contexts, x)
	}

	doc.CurrentContext = doc.Contexts[0].Name

	return encodeKubeconfigDoc(doc)
}

func execUser(command string, args ...string) kubeStoreUser {
	var u kubeStoreUser
	u.User.Exec = &kubeStoreExec{APIVersion: "client.authentication.k8s.io/v1beta1", Command: command, Args: args}

	return u
}

// cloudGet is a GET with bearer, decoded as JSON.
func cloudGet(ctx context.Context, u, bearer string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+bearer)

	return cloudJSON(req, out)
}

func discoverEKSClusters(ctx context.Context, s map[string]string) ([]discoveredCluster, error) {
	region := s[awsFieldRegion]
	if region == "" || !kubeNamePattern.MatchString(region) {
		return nil, errors.New("enter the AWS region")
	}

	if s[awsFieldAccessKey] == "" || s[awsFieldSecretKey] == "" {
		return nil, errors.New("enter an access key and its secret")
	}

	creds := awsCredentials{s[awsFieldAccessKey], s[awsFieldSecretKey], s[awsFieldSessionToken]}
	get := func(path string, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, eksEndpoint(region)+path, nil)
		if err != nil {
			return err
		}

		sigV4Sign(req, emptySHA256, creds, region, "eks", time.Now())

		return cloudJSON(req, out)
	}

	var list struct {
		Clusters []string `json:"clusters"`
	}

	if err := get("/clusters", &list); err != nil {
		return nil, fmt.Errorf("EKS clusters: %w", err)
	}

	var out []discoveredCluster

	for _, name := range list.Clusters {
		var d struct {
			Cluster struct {
				Endpoint             string `json:"endpoint"`
				CertificateAuthority struct {
					Data string `json:"data"`
				} `json:"certificateAuthority"`
			} `json:"cluster"`
		}

		if err := get("/clusters/"+url.PathEscape(name), &d); err != nil {
			return nil, fmt.Errorf("EKS cluster %s: %w", name, err)
		}

		out = append(out, discoveredCluster{
			name: name + "." + region + ".eks", server: d.Cluster.Endpoint, caData: d.Cluster.CertificateAuthority.Data,
			user: execUser("aws", "--region", region, "eks", "get-token", "--cluster-name", name),
		})
	}

	return out, nil
}

func discoverDOClusters(ctx context.Context, s map[string]string) ([]discoveredCluster, error) {
	token := s[doFieldToken]
	if token == "" {
		return nil, errors.New("enter a DigitalOcean API token")
	}

	var list struct {
		Clusters []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Region   string `json:"region"`
			Endpoint string `json:"endpoint"`
		} `json:"kubernetes_clusters"`
	}

	if err := cloudGet(ctx, doAPIEndpoint+"/v2/kubernetes/clusters", token, &list); err != nil {
		return nil, fmt.Errorf("DigitalOcean clusters: %w", err)
	}

	var out []discoveredCluster

	for _, c := range list.Clusters {
		var creds struct {
			CA string `json:"certificate_authority_data"`
		}

		if err := cloudGet(ctx, doAPIEndpoint+"/v2/kubernetes/clusters/"+url.PathEscape(c.ID)+"/credentials", token, &creds); err != nil {
			return nil, fmt.Errorf("DigitalOcean cluster %s: %w", c.Name, err)
		}

		out = append(out, discoveredCluster{
			name: c.Name + "." + c.Region + ".do", server: c.Endpoint, caData: creds.CA,
			user: execUser("doctl", "kubernetes", "cluster", "kubeconfig", "exec-credential", "--version=v1beta1", c.ID),
		})
	}

	return out, nil
}

// discoverAKSClusters lists the subscription's AKS clusters with a service principal and
// merges the user kubeconfigs Azure hands out (kubelogin ones when Entra ID is on).
func discoverAKSClusters(ctx context.Context, s map[string]string) (string, error) {
	tenant, sub := s[azureFieldTenant], s[azureFieldSubscript]
	if tenant == "" || sub == "" || s[azureFieldClientID] == "" || s[azureFieldClientSecret] == "" {
		return "", errors.New("enter the tenant, the subscription and the service principal's client ID and secret")
	}

	arm := &azureSPNMethod{authority: azureAuthority("", tenant), serverID: azureARM, clientID: s[azureFieldClientID]}

	token, _, _, err := arm.mint(ctx, kubeAuthState{Secrets: pick(s, azureFieldClientID, azureFieldClientSecret)})
	if err != nil {
		return "", err
	}

	var list struct {
		Value []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"value"`
	}

	if err := cloudGet(ctx, azureARM+"/subscriptions/"+url.PathEscape(sub)+"/providers/Microsoft.ContainerService/managedClusters?api-version="+azureARMVersion, token, &list); err != nil {
		return "", fmt.Errorf("AKS clusters: %w", err)
	}

	merged := ""

	for _, c := range list.Value {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, azureARM+c.ID+"/listClusterUserCredential?api-version="+azureARMVersion, nil)
		if err != nil {
			return "", err
		}

		req.Header.Set("Authorization", "Bearer "+token)

		var creds struct {
			Kubeconfigs []struct {
				Value string `json:"value"`
			} `json:"kubeconfigs"`
		}

		if err := cloudJSON(req, &creds); err != nil {
			return "", fmt.Errorf("AKS cluster %s: %w", c.Name, err)
		}

		for _, k := range creds.Kubeconfigs {
			yaml, err := base64.StdEncoding.DecodeString(k.Value)
			if err != nil {
				return "", fmt.Errorf("AKS cluster %s: %w", c.Name, err)
			}

			if merged, err = mergeDiscovered(merged, string(yaml)); err != nil {
				return "", err
			}
		}
	}

	if merged == "" {
		return "", errors.New("no cluster found with these credentials")
	}

	return merged, nil
}

// discoverRancherClusters asks Rancher for a kubeconfig of each cluster the API key sees.
func discoverRancherClusters(ctx context.Context, s map[string]string) (string, error) {
	server, err := url.Parse(s[rancherFieldServer])
	if err != nil || server.Scheme != "https" || server.Host == "" {
		return "", errors.New("the Rancher server must be an https URL")
	}

	if _, err := (rancherMethod{}).fromSecrets(s); err != nil {
		return "", err
	}

	base := strings.TrimSuffix(server.String(), "/")
	key := s[rancherFieldKey]

	var list struct {
		Data []struct {
			ID      string            `json:"id"`
			Name    string            `json:"name"`
			Actions map[string]string `json:"actions"`
		} `json:"data"`
	}

	if err := cloudGet(ctx, base+"/v3/clusters", key, &list); err != nil {
		return "", fmt.Errorf("list Rancher clusters: %w", err)
	}

	merged := ""

	for _, c := range list.Data {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v3/clusters/"+url.PathEscape(c.ID)+"?action=generateKubeconfig", nil)
		if err != nil {
			return "", err
		}

		req.Header.Set("Authorization", "Bearer "+key)

		var answer struct {
			Config string `json:"config"`
		}

		if err := cloudJSON(req, &answer); err != nil {
			return "", fmt.Errorf("kubeconfig of Rancher cluster %s: %w", c.Name, err)
		}

		if merged, err = mergeDiscovered(merged, answer.Config); err != nil {
			return "", err
		}
	}

	if merged == "" {
		return "", errors.New("no cluster found with this API key")
	}

	return merged, nil
}

// mergeDiscovered adds the contexts of next to acc (both kubeconfigs), each with its own
// cluster and user, renaming a clash; contexts the app cannot use are kept for the preview
// to explain.
func mergeDiscovered(acc, next string) (string, error) {
	doc, err := loadStoredKubeconfig(acc)
	if err != nil {
		return "", err
	}

	add, err := loadKubeconfigDoc(next)
	if err != nil {
		return "", err
	}

	taken := kubeNames(doc)
	merged := *doc

	for _, name := range add.sortedNames() {
		single, err := add.single(name)
		if err != nil {
			continue
		}

		final := name
		if taken[final] {
			final = freeName(taken, map[string]bool{}, name)
		}

		taken[final] = true
		merged = merged.with(renamedKubeContext(single, final))
	}

	if merged.CurrentContext == "" && len(merged.Contexts) > 0 {
		merged.CurrentContext = merged.Contexts[0].Name
	}

	slices.SortStableFunc(merged.Contexts, func(a, b kubeStoreContext) int { return strings.Compare(a.Name, b.Name) })

	return encodeKubeconfigDoc(&merged)
}
