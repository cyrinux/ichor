package helm

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
	"time"

	"github.com/cyrinux/ichor/go/kube"
	"go.yaml.in/yaml/v4"
)

// Helm releases, read-only, the way `helm list` and `helm get` find them: each revision is a
// Secret labelled owner=helm (the default storage driver) whose "release" holds the release
// as base64 of gzipped JSON. Decoded here rather than with the Helm SDK, which would weigh
// on the app for a few fields.

// releaseMax bounds a decoded release (charts with large CRDs reach a few MiB).
const releaseMax = 16 << 20

type ReleaseSummary struct {
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

type ReleaseList struct {
	Releases []ReleaseSummary `json:"releases"`
}

type Revision struct {
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	Updated     int64  `json:"updated"`
	Description string `json:"description,omitempty"`
}

type ReleaseDetail struct {
	ReleaseSummary

	Description string     `json:"description,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	Values      string     `json:"values"`
	Manifest    string     `json:"manifest"`
	History     []Revision `json:"history"`
}

// Release is the part of Helm's release JSON the app reads.
type Release struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	Info      struct {
		Status        string    `json:"status"`
		FirstDeployed time.Time `json:"first_deployed"`
		LastDeployed  time.Time `json:"last_deployed"`
		Description   string    `json:"description"`
		Notes         string    `json:"notes"`
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
	Hooks    []ReleaseHook  `json:"hooks"`
}

// ReleaseHook is a chart hook as Helm stores it (pkg/release/v1 Hook); every field may be
// missing from an older Secret.
type ReleaseHook struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Manifest       string   `json:"manifest"` // the rendered object, one YAML document
	Events         []string `json:"events"`
	Weight         int      `json:"weight"`
	DeletePolicies []string `json:"delete_policies"`
}

// SecretRef is a release revision as its Secret's labels tell it.
type SecretRef struct {
	Secret, Name, Namespace, Status string
	Revision                        int
	Modified                        int64
}

// Releases lists the latest revision of each release (in namespace, "" for all), sorted by
// namespace then name.
func Releases(ctx context.Context, k kube.Client, namespace string) (ReleaseList, error) {
	refs, err := SecretRefs(ctx, k, namespace, "")
	if err != nil {
		return ReleaseList{}, err
	}

	out := ReleaseList{Releases: []ReleaseSummary{}}

	for _, ref := range LatestRevisions(refs) {
		rel, err := ReadRelease(ctx, k, ref)
		if err != nil {
			return ReleaseList{}, err
		}

		out.Releases = append(out.Releases, Summarize(rel, ref))
	}

	slices.SortFunc(out.Releases, func(a, b ReleaseSummary) int {
		return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
	})

	return out, nil
}

// Detail reads one release: its latest revision (values, manifest, notes) and its history.
func Detail(ctx context.Context, k kube.Client, namespace, name string) (ReleaseDetail, error) {
	refs, err := SecretRefs(ctx, k, namespace, name)
	if err != nil {
		return ReleaseDetail{}, err
	}

	refs = historyRefs(refs, name)
	if len(refs) == 0 {
		return ReleaseDetail{}, fmt.Errorf("no Helm release %s/%s", namespace, name)
	}

	rel, err := ReadRelease(ctx, k, refs[0])
	if err != nil {
		return ReleaseDetail{}, err
	}

	values := ""

	if len(rel.Config) > 0 {
		data, err := yaml.Marshal(rel.Config)
		if err != nil {
			return ReleaseDetail{}, fmt.Errorf("encode values: %w", err)
		}

		values = string(data)
	}

	detail := ReleaseDetail{
		ReleaseSummary: Summarize(rel, refs[0]),
		Description:    rel.Info.Description, Notes: rel.Info.Notes, Values: values, Manifest: rel.Manifest,
		History: []Revision{},
	}

	for _, ref := range refs {
		detail.History = append(detail.History, Revision{Revision: ref.Revision, Status: ref.Status, Updated: ref.Modified})
	}

	return detail, nil
}

// LatestRevisions keeps the highest revision of each release.
func LatestRevisions(refs []SecretRef) []SecretRef {
	latest := map[string]SecretRef{}

	for _, ref := range refs {
		key := ref.Namespace + "/" + ref.Name
		if cur, ok := latest[key]; !ok || ref.Revision > cur.Revision {
			latest[key] = ref
		}
	}

	out := make([]SecretRef, 0, len(latest))
	for _, ref := range latest {
		out = append(out, ref)
	}

	return out
}

func ReadRelease(ctx context.Context, k kube.Client, ref SecretRef) (Release, error) {
	var secret struct {
		Data map[string]string `json:"data"`
	}

	if err := k.Get(ctx, kube.ScopedPath("/api/v1", ref.Namespace, "secrets")+"/"+url.PathEscape(ref.Secret), &secret); err != nil {
		return Release{}, err
	}

	return decodeRelease(secret.Data["release"])
}

// decodeRelease reads a release Secret's "release" value: base64 (the Secret's) of
// base64 (Helm's) of gzipped JSON. Old releases may lack the gzip.
func decodeRelease(data string) (Release, error) {
	raw, err := releaseJSON(data)
	if err != nil {
		return Release{}, err
	}

	var rel Release
	if err := json.Unmarshal(raw, &rel); err != nil {
		return Release{}, fmt.Errorf("decode Helm release: %w", err)
	}

	return rel, nil
}

// releaseJSON undoes the two base64 layers and the gzip of a Secret's "release" value.
func releaseJSON(data string) ([]byte, error) {
	outer, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("decode Helm release: %w", err)
	}

	inner, err := base64.StdEncoding.DecodeString(string(outer))
	if err != nil {
		return nil, fmt.Errorf("decode Helm release: %w", err)
	}

	if bytes.HasPrefix(inner, []byte{0x1f, 0x8b}) {
		r, err := gzip.NewReader(bytes.NewReader(inner))
		if err != nil {
			return nil, fmt.Errorf("decode Helm release: %w", err)
		}

		if inner, err = io.ReadAll(io.LimitReader(r, releaseMax+1)); err != nil {
			return nil, fmt.Errorf("decode Helm release: %w", err)
		}

		if len(inner) > releaseMax {
			return nil, errors.New("decoded Helm release is too large")
		}
	}

	return inner, nil
}

func Summarize(rel Release, ref SecretRef) ReleaseSummary {
	s := ReleaseSummary{
		Name: cmp.Or(rel.Name, ref.Name), Namespace: cmp.Or(rel.Namespace, ref.Namespace),
		Revision: cmp.Or(rel.Version, ref.Revision), Status: cmp.Or(rel.Info.Status, ref.Status),
		Chart: rel.Chart.Metadata.Name, ChartVersion: rel.Chart.Metadata.Version, AppVersion: rel.Chart.Metadata.AppVersion,
		Updated: ref.Modified,
	}

	if !rel.Info.LastDeployed.IsZero() {
		s.Updated = rel.Info.LastDeployed.Unix()
	}

	return s
}
