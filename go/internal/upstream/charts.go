package upstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"go.yaml.in/yaml/v4"
)

const indexLimit = 8 << 20

// lookupChart compares t.Version with the versions of chart t.Name in t.RepoURL: its
// index.yaml, or the tags of <host>/<path>/<name> for an oci:// repository.
func (c *Client) lookupChart(ctx context.Context, t Target, auth Auth) (Candidate, error) {
	if t.Name == "" || t.RepoURL == "" {
		return Candidate{}, fmt.Errorf("a chart needs its repoURL and name")
	}

	cand := classify(t.Version, nil)
	if cand.Reason != "" {
		return cand, nil
	}

	if rest, ok := strings.CutPrefix(t.RepoURL, "oci://"); ok {
		host, path, _ := strings.Cut(strings.Trim(rest, "/"), "/")
		host = normalizeHost(host)
		repo := strings.Trim(path+"/"+t.Name, "/")

		if needsSigV4(host) {
			cand.Reason = ReasonCredentials

			return cand, nil
		}

		got, err := c.cache.get(ctx, "image:"+host+"/"+repo, func() (fetched, error) {
			return c.ociTags(ctx, host, repo, auth.basic(host))
		})
		if err != nil {
			return cand, err
		}

		// OCI tags cannot hold "+": Helm stores 1.2.3+build as 1.2.3_build.
		versions := make([]string, len(got.tags))
		for i, tag := range got.tags {
			versions[i] = strings.ReplaceAll(tag, "_", "+")
		}

		out := classify(t.Version, versions)
		out.Source = got.source

		return out, nil
	}

	index := strings.TrimSuffix(t.RepoURL, "/") + "/index.yaml"

	got, err := c.cache.get(ctx, "index:"+index, func() (fetched, error) {
		return c.chartIndex(ctx, index, auth.Helm)
	})
	if err != nil {
		return cand, err
	}

	versions, ok := got.charts[t.Name]
	if !ok {
		return cand, fmt.Errorf("chart %s is not in %s", t.Name, index)
	}

	out := classify(t.Version, versions)
	out.Source = got.source

	return out, nil
}

type chartIndex struct {
	Entries map[string][]struct {
		Version    string `yaml:"version"`
		Deprecated bool   `yaml:"deprecated"`
	} `yaml:"entries"`
}

// chartIndex reads every chart's versions from an index.yaml, the deprecated ones left out.
// The basic auth is only sent over https.
func (c *Client) chartIndex(ctx context.Context, index string, basic *BasicAuth) (fetched, error) {
	u, err := url.Parse(index)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fetched{}, fmt.Errorf("unusable chart repository %q", index)
	}

	var authz *string

	if basic != nil && u.Scheme == "https" {
		h := basicHeader(basic)
		authz = &h
	}

	resp, err := c.get(ctx, index, authz, "application/yaml, text/yaml, */*")
	if err != nil {
		return fetched{}, err
	}

	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return fetched{}, fmt.Errorf("%s has no index.yaml", u.Host)
	}

	if err := statusError(u.Host, resp); err != nil {
		return fetched{}, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, indexLimit+1))
	if err != nil {
		return fetched{}, fmt.Errorf("read %s: %w", index, err)
	}

	if len(body) > indexLimit {
		return fetched{}, fmt.Errorf("%s is larger than 8 MiB", index)
	}

	var idx chartIndex
	if err := yaml.Unmarshal(body, &idx); err != nil {
		return fetched{}, fmt.Errorf("parse %s: %w", index, err)
	}

	out := fetched{source: index, charts: make(map[string][]string, len(idx.Entries))}

	for name, entries := range idx.Entries {
		versions := make([]string, 0, len(entries))

		for _, e := range entries {
			if !e.Deprecated && e.Version != "" {
				versions = append(versions, e.Version)
			}
		}

		out.charts[name] = versions
	}

	return out, nil
}
