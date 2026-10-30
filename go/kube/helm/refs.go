package helm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cyrinux/ichor/go/kube"
)

const (
	// refsPageLimit is the page size of the list of release Secrets.
	refsPageLimit = 500
	// refsExpiredRetries is how many times the list starts again after a 410 Gone.
	refsExpiredRetries = 2
	// tableAccept asks for the server-side table `kubectl get` prints (metav1.Table), falling
	// back to the objects themselves on a server or resource without it.
	tableAccept = "application/json;as=Table;v=v1;g=meta.k8s.io, application/json"
)

// secretRow is a row of the Table of release Secrets: their metadata only.
type secretRow struct {
	Object struct {
		Metadata struct {
			Name              string            `json:"name"`
			Namespace         string            `json:"namespace"`
			Labels            map[string]string `json:"labels"`
			CreationTimestamp time.Time         `json:"creationTimestamp"`
		} `json:"metadata"`
	} `json:"object"`
}

// SecretRefs lists the release Secrets by their labels only (a Table with metadata): their
// payload is read for the revisions shown, not for the whole history. name "" is every
// release. A list the API expires mid-way starts again.
func SecretRefs(ctx context.Context, k kube.Client, namespace, name string) ([]SecretRef, error) {
	selector := "owner=helm"
	if name != "" {
		selector += ",name=" + name
	}

	for attempt := 0; ; attempt++ {
		refs, err := secretRefPages(ctx, k, kube.ScopedPath("/api/v1", namespace, "secrets"), selector)
		if err == nil || kube.Code(err) != http.StatusGone || attempt == refsExpiredRetries {
			return refs, err
		}
	}
}

// secretRefPages reads every page of the list at path. What it read stays with the error, as
// a caller only uses it without one.
func secretRefPages(ctx context.Context, k kube.Client, path, selector string) ([]SecretRef, error) {
	refs := []SecretRef{}
	token := ""

	for {
		rows, next, err := secretRefPage(ctx, k, path, selector, token)
		if err != nil {
			return refs, err
		}

		refs = appendSecretRefs(refs, rows)
		if next == "" {
			return refs, nil
		}

		token = next
	}
}

// secretRefPage reads one page: its rows and the token of the next one.
func secretRefPage(ctx context.Context, k kube.Client, path, selector, token string) ([]secretRow, string, error) {
	query := url.Values{"limit": {strconv.Itoa(refsPageLimit)}, "labelSelector": {selector}, "includeObject": {"Metadata"}}
	if token != "" {
		query.Set("continue", token)
	}

	resp, data, err := k.Send(ctx, http.MethodGet, path+"?"+query.Encode(), tableAccept, "", nil, nil)
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", kube.StatusError(resp.StatusCode, data)
	}

	var body struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Continue string `json:"continue"`
		} `json:"metadata"`
	}

	if err := json.Unmarshal(data, &body); err != nil {
		return nil, "", fmt.Errorf("decode Kubernetes API answer: %w", err)
	}

	if body.Kind != "Table" {
		return nil, "", errors.New("the API server did not answer with a table")
	}

	var table struct {
		Rows []secretRow `json:"rows"`
	}

	if err := json.Unmarshal(data, &table); err != nil {
		return nil, "", fmt.Errorf("decode Kubernetes API table: %w", err)
	}

	return table.Rows, body.Metadata.Continue, nil
}

// appendSecretRefs adds the rows that are revisions of a release.
func appendSecretRefs(refs []SecretRef, rows []secretRow) []SecretRef {
	for _, row := range rows {
		meta := row.Object.Metadata

		revision, err := strconv.Atoi(meta.Labels["version"])
		if err != nil || meta.Labels["name"] == "" {
			continue
		}

		modified, _ := strconv.ParseInt(meta.Labels["modifiedAt"], 10, 64)
		if modified == 0 {
			modified = meta.CreationTimestamp.Unix()
		}

		refs = append(refs, SecretRef{
			Secret: meta.Name, Name: meta.Labels["name"], Namespace: meta.Namespace,
			Status: meta.Labels["status"], Revision: revision, Modified: modified,
		})
	}

	return refs
}
