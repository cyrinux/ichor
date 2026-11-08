package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	// listPageLimit is the page size of GetList: a page of full pods stays well below MaxBody.
	listPageLimit = 500
	// listExpiredRetries is how many times GetList starts a list again after a 410 Gone.
	listExpiredRetries = 2
)

// APIGroups lists the API groups the server serves, with their preferred versions.
func APIGroups(ctx context.Context, c Client) (map[string]string, error) {
	var list struct {
		Groups []struct {
			Name             string `json:"name"`
			PreferredVersion struct {
				Version string `json:"version"`
			} `json:"preferredVersion"`
		} `json:"groups"`
	}

	if err := c.Get(ctx, "/apis", &list); err != nil {
		return nil, err
	}

	groups := map[string]string{}
	for _, g := range list.Groups {
		groups[g.Name] = g.PreferredVersion.Version
	}

	return groups, nil
}

// GetList reads every item of the list at path into out, page by page, so that a large list
// never meets MaxBody: the drop-in for Get on a collection. path may carry a labelSelector and
// a fieldSelector query; other parameters are refused. A 410 Gone mid-way starts the list
// again. The errors are Get's: a 404 on the first page is still IsNotFound.
func GetList[T any](ctx context.Context, c Client, path string, out *List[T]) error {
	path, rawQuery, _ := strings.Cut(path, "?")

	query, err := listQuery(rawQuery)
	if err != nil {
		return err
	}

	for attempt := 0; ; attempt++ {
		items, err := listPages[T](ctx, c, path, query)
		if err == nil {
			out.Items = items

			return nil
		}

		if Code(err) != http.StatusGone || attempt == listExpiredRetries {
			return err
		}
	}
}

// listQuery reads the selectors of a list path's query.
func listQuery(rawQuery string) (url.Values, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("bad Kubernetes list query %q: %w", rawQuery, err)
	}

	query := url.Values{"limit": {strconv.Itoa(listPageLimit)}}

	for key := range values {
		if key != "labelSelector" && key != "fieldSelector" {
			return nil, fmt.Errorf("unsupported Kubernetes list parameter %q", key)
		}

		if v := values.Get(key); v != "" {
			query.Set(key, v)
		}
	}

	return query, nil
}

// listPages reads every page of the list at path.
func listPages[T any](ctx context.Context, c Client, path string, query url.Values) ([]T, error) {
	items := []T{}
	token := ""

	for {
		page := url.Values{}
		for key, v := range query {
			page[key] = v
		}

		if token != "" {
			page.Set("continue", token)
		}

		resp, data, err := c.Send(ctx, http.MethodGet, path+"?"+page.Encode(), "application/json", "", nil, nil)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, StatusError(resp.StatusCode, data)
		}

		var body struct {
			Metadata struct {
				Continue string `json:"continue"`
			} `json:"metadata"`
			Items []json.RawMessage `json:"items"`
		}

		if err := json.Unmarshal(data, &body); err != nil {
			return nil, fmt.Errorf("decode Kubernetes API answer: %w", err)
		}

		for _, raw := range body.Items {
			var obj T
			if err := json.Unmarshal(raw, &obj); err != nil {
				return nil, fmt.Errorf("decode Kubernetes object: %w", err)
			}

			items = append(items, obj)
		}

		if body.Metadata.Continue == "" {
			return items, nil
		}

		token = body.Metadata.Continue
	}
}
