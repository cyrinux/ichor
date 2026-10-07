package ichorgo

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

// Helm releases, read-only, the way `helm list` and `helm get` find them: each revision is a
// Secret labelled owner=helm (the default storage driver) whose "release" holds the release
// as base64 of gzipped JSON. Decoded here rather than with the Helm SDK, which would weigh
// on the app for a few fields.

// helmReleaseMax bounds a decoded release (charts with large CRDs reach a few MiB).
const helmReleaseMax = 16 << 20

type helmReleaseSummary struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Revision     int    `json:"revision"`
	Status       string `json:"status"`
	Chart        string `json:"chart"`
	ChartVersion string `json:"chartVersion"`
	AppVersion   string `json:"appVersion,omitempty"`
	// Updated is Unix seconds of the last deployment.
	Updated int64 `json:"updated"`
}

type helmReleaseList struct {
	Releases []helmReleaseSummary `json:"releases"`
}

type helmRevision struct {
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	Updated     int64  `json:"updated"`
	Description string `json:"description,omitempty"`
}

type helmReleaseDetail struct {
	helmReleaseSummary

	Description string         `json:"description,omitempty"`
	Notes       string         `json:"notes,omitempty"`
	Values      string         `json:"values"`
	Manifest    string         `json:"manifest"`
	History     []helmRevision `json:"history"`
}

// helmRelease is the part of Helm's release JSON the app reads.
type helmRelease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	Info      struct {
		Status       string    `json:"status"`
		LastDeployed time.Time `json:"last_deployed"`
		Description  string    `json:"description"`
		Notes        string    `json:"notes"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
	} `json:"chart"`
	Config   map[string]any `json:"config"`
	Manifest string         `json:"manifest"`
}

// helmSecretRef is a release revision as its Secret's labels tell it.
type helmSecretRef struct {
	secret, name, namespace, status string
	revision                        int
	modified                        int64
}

// KubeHelmReleases lists the latest revision of each Helm release (in namespace, "" for all),
// as a JSON helmReleaseList sorted by namespace then name.
func KubeHelmReleases(configYAML, contextName, kubeServer, namespace string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))

	if err := validateNamespace(namespace); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoHelmReleases, func(ctx context.Context, k *kubeClient) (helmReleaseList, error) {
		refs, err := helmSecretRefs(ctx, k, namespace, "")
		if err != nil {
			return helmReleaseList{}, err
		}

		out := helmReleaseList{Releases: []helmReleaseSummary{}}

		for _, ref := range latestRevisions(refs) {
			rel, err := readHelmRelease(ctx, k, ref)
			if err != nil {
				return helmReleaseList{}, err
			}

			out.Releases = append(out.Releases, summarizeRelease(rel, ref))
		}

		slices.SortFunc(out.Releases, func(a, b helmReleaseSummary) int {
			return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
		})

		return out, nil
	})
}

// KubeHelmRelease reads one release: its latest revision (values, manifest, notes) and its
// history, as a JSON helmReleaseDetail.
func KubeHelmRelease(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("Helm release", namespace, name); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() helmReleaseDetail { return demoHelmRelease(namespace, name) }, func(ctx context.Context, k *kubeClient) (helmReleaseDetail, error) {
		refs, err := helmSecretRefs(ctx, k, namespace, name)
		if err != nil {
			return helmReleaseDetail{}, err
		}

		if len(refs) == 0 {
			return helmReleaseDetail{}, fmt.Errorf("no Helm release %s/%s", namespace, name)
		}

		slices.SortFunc(refs, func(a, b helmSecretRef) int { return cmp.Compare(b.revision, a.revision) })

		rel, err := readHelmRelease(ctx, k, refs[0])
		if err != nil {
			return helmReleaseDetail{}, err
		}

		values := ""

		if len(rel.Config) > 0 {
			data, err := yaml.Marshal(rel.Config)
			if err != nil {
				return helmReleaseDetail{}, fmt.Errorf("encode values: %w", err)
			}

			values = string(data)
		}

		detail := helmReleaseDetail{
			helmReleaseSummary: summarizeRelease(rel, refs[0]),
			Description:        rel.Info.Description, Notes: rel.Info.Notes, Values: values, Manifest: rel.Manifest,
			History: []helmRevision{},
		}

		for _, ref := range refs {
			detail.History = append(detail.History, helmRevision{Revision: ref.revision, Status: ref.status, Updated: ref.modified})
		}

		return detail, nil
	})
}

// helmSecretRefs lists the release Secrets by their labels only (a Table with metadata):
// their payload is read for the revisions shown, not for the whole history.
func helmSecretRefs(ctx context.Context, k *kubeClient, namespace, name string) ([]helmSecretRef, error) {
	selector := "owner=helm"
	if name != "" {
		selector += ",name=" + name
	}

	refs := []helmSecretRef{}

	err := k.listAll(ctx, scopedPath("/api/v1", namespace, "secrets"), pageQuery{labelSelector: selector, table: true}, func() { refs = refs[:0] }, func(page kubePage) error {
		if page.table == nil {
			return errors.New("the API server did not answer with a table")
		}

		for _, row := range page.table.Rows {
			meta := row.Object.Metadata

			revision, err := strconv.Atoi(meta.Labels["version"])
			if err != nil || meta.Labels["name"] == "" {
				continue
			}

			modified, _ := strconv.ParseInt(meta.Labels["modifiedAt"], 10, 64)
			if modified == 0 {
				modified = meta.CreationTimestamp.Unix()
			}

			refs = append(refs, helmSecretRef{
				secret: meta.Name, name: meta.Labels["name"], namespace: meta.Namespace,
				status: meta.Labels["status"], revision: revision, modified: modified,
			})
		}

		return nil
	})

	return refs, err
}

// latestRevisions keeps the highest revision of each release.
func latestRevisions(refs []helmSecretRef) []helmSecretRef {
	latest := map[string]helmSecretRef{}

	for _, ref := range refs {
		key := ref.namespace + "/" + ref.name
		if cur, ok := latest[key]; !ok || ref.revision > cur.revision {
			latest[key] = ref
		}
	}

	out := make([]helmSecretRef, 0, len(latest))
	for _, ref := range latest {
		out = append(out, ref)
	}

	return out
}

func readHelmRelease(ctx context.Context, k *kubeClient, ref helmSecretRef) (helmRelease, error) {
	var secret struct {
		Data map[string]string `json:"data"`
	}

	if err := k.get(ctx, scopedPath("/api/v1", ref.namespace, "secrets")+"/"+url.PathEscape(ref.secret), &secret); err != nil {
		return helmRelease{}, err
	}

	return decodeHelmRelease(secret.Data["release"])
}

// decodeHelmRelease reads a release Secret's "release" value: base64 (the Secret's) of
// base64 (Helm's) of gzipped JSON. Old releases may lack the gzip.
func decodeHelmRelease(data string) (helmRelease, error) {
	outer, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return helmRelease{}, fmt.Errorf("Helm release: %w", err)
	}

	inner, err := base64.StdEncoding.DecodeString(string(outer))
	if err != nil {
		return helmRelease{}, fmt.Errorf("Helm release: %w", err)
	}

	if bytes.HasPrefix(inner, []byte{0x1f, 0x8b}) {
		r, err := gzip.NewReader(bytes.NewReader(inner))
		if err != nil {
			return helmRelease{}, fmt.Errorf("Helm release: %w", err)
		}

		if inner, err = io.ReadAll(io.LimitReader(r, helmReleaseMax+1)); err != nil {
			return helmRelease{}, fmt.Errorf("Helm release: %w", err)
		}

		if len(inner) > helmReleaseMax {
			return helmRelease{}, errors.New("Helm release is too large")
		}
	}

	var rel helmRelease
	if err := json.Unmarshal(inner, &rel); err != nil {
		return helmRelease{}, fmt.Errorf("Helm release: %w", err)
	}

	return rel, nil
}

func summarizeRelease(rel helmRelease, ref helmSecretRef) helmReleaseSummary {
	s := helmReleaseSummary{
		Name: cmp.Or(rel.Name, ref.name), Namespace: cmp.Or(rel.Namespace, ref.namespace),
		Revision: cmp.Or(rel.Version, ref.revision), Status: cmp.Or(rel.Info.Status, ref.status),
		Chart: rel.Chart.Metadata.Name, ChartVersion: rel.Chart.Metadata.Version, AppVersion: rel.Chart.Metadata.AppVersion,
		Updated: ref.modified,
	}

	if !rel.Info.LastDeployed.IsZero() {
		s.Updated = rel.Info.LastDeployed.Unix()
	}

	return s
}

func demoHelmReleases() helmReleaseList {
	updated := time.Now().Add(-6 * 24 * time.Hour).Unix()

	return helmReleaseList{Releases: []helmReleaseSummary{
		{Name: "cert-manager", Namespace: "cert-manager", Revision: 4, Status: "deployed", Chart: "cert-manager", ChartVersion: "v1.18.2", AppVersion: "v1.18.2", Updated: updated},
		{Name: "ingress-nginx", Namespace: "ingress-nginx", Revision: 7, Status: "deployed", Chart: "ingress-nginx", ChartVersion: "4.13.0", AppVersion: "1.13.0", Updated: updated},
	}}
}

func demoHelmRelease(namespace, name string) helmReleaseDetail {
	for _, r := range demoHelmReleases().Releases {
		if r.Namespace == namespace && r.Name == name {
			return helmReleaseDetail{
				helmReleaseSummary: r, Description: "Upgrade complete", Values: "replicaCount: 2\n",
				Manifest: "# Demo cluster: no real manifest.\n",
				History:  []helmRevision{{Revision: r.Revision, Status: r.Status, Updated: r.Updated}},
			}
		}
	}

	return helmReleaseDetail{helmReleaseSummary: helmReleaseSummary{Name: name, Namespace: namespace}, History: []helmRevision{}}
}
