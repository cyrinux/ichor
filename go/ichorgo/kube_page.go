package ichorgo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// kubePageLimit is the page size of a paged list when the app does not ask for one: a
	// page of full pods stays well below kubeMaxBody.
	kubePageLimit = 500
	// kubePageMaxLimit bounds the page size the app may ask for.
	kubePageMaxLimit = 5000
	// kubeExpiredRetries is how many times listAll starts a list again after a 410 Gone.
	kubeExpiredRetries = 2
	// kubeTableAccept asks for the server-side table `kubectl get` prints (metav1.Table),
	// falling back to the objects themselves on a server or resource without it.
	kubeTableAccept = "application/json;as=Table;v=v1;g=meta.k8s.io, application/json"
)

// pageQuery is one page of a Kubernetes list.
type pageQuery struct {
	limit         int
	continueToken string // the API server's own token, "" for the first page
	labelSelector string
	fieldSelector string
	// table asks for a Table (rows of server-side columns plus each object's metadata)
	// instead of full objects: 10-20 times smaller for pods.
	table bool
}

// kubePage is one page of a list: table when the server answered with a Table, else items.
type kubePage struct {
	table *kubeTable
	items []json.RawMessage
	// continueToken asks for the next page, "" on the last one.
	continueToken string
	// remaining is how many items the next pages hold, -1 when the server does not say
	// (it never does with a selector).
	remaining int64
}

// kubeTable is the part of a metav1.Table the app reads.
type kubeTable struct {
	Columns []struct {
		Name string `json:"name"`
	} `json:"columnDefinitions"`
	Rows []kubeTableRow `json:"rows"`
}

type kubeTableRow struct {
	Cells  []json.RawMessage `json:"cells"`
	Object struct {
		Metadata kubeRowMeta `json:"metadata"`
	} `json:"object"`
}

// kubeRowMeta is the metadata of a row's object (includeObject=Metadata).
type kubeRowMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Labels            map[string]string `json:"labels"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
	DeletionTimestamp *time.Time        `json:"deletionTimestamp"`
	OwnerReferences   []struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"ownerReferences"`
}

// column is the index of the column named name (case-insensitive), -1 when absent.
func (t *kubeTable) column(name string) int {
	for i, c := range t.Columns {
		if strings.EqualFold(c.Name, name) {
			return i
		}
	}

	return -1
}

// text is the cell at column i as text: a string as is, a number or boolean as JSON.
func (r kubeTableRow) text(i int) string {
	if i < 0 || i >= len(r.Cells) {
		return ""
	}

	var s string
	if json.Unmarshal(r.Cells[i], &s) == nil {
		return s
	}

	raw := strings.TrimSpace(string(r.Cells[i]))
	if raw == "null" {
		return ""
	}

	return raw
}

// leadingInt is the number s starts with ("3 (5m ago)" is 3), 0 when none.
func leadingInt(s string) int {
	s = strings.TrimSpace(s)

	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}

	n, _ := strconv.Atoi(s[:end]) //nolint:errcheck // no digits: 0

	return n
}

// readyCount reads a READY cell ("1/2").
func readyCount(s string) (ready, total int) {
	a, b, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok {
		return 0, 0
	}

	return leadingInt(a), leadingInt(b)
}

// getPage reads one page of the list at path (a collection: /api/v1/pods,
// /api/v1/namespaces/NS/pods...). A 410 Gone (expired continue token) is a kubeAPIError
// with Code 410; see isKubeExpired.
func (k *kubeClient) getPage(ctx context.Context, path string, q pageQuery) (kubePage, error) {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(clampPageLimit(q.limit)))

	if q.continueToken != "" {
		query.Set("continue", q.continueToken)
	}

	if q.labelSelector != "" {
		query.Set("labelSelector", q.labelSelector)
	}

	if q.fieldSelector != "" {
		query.Set("fieldSelector", q.fieldSelector)
	}

	accept := "application/json"
	if q.table {
		accept = kubeTableAccept

		query.Set("includeObject", "Metadata")
	}

	resp, data, err := k.send(ctx, http.MethodGet, path+"?"+query.Encode(), accept, "", nil, nil)
	if err != nil {
		return kubePage{}, err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return kubePage{}, kubeStatusError(resp.StatusCode, data)
	}

	return decodePage(data)
}

// decodePage reads a Table or a plain list, whichever the server sent.
func decodePage(data []byte) (kubePage, error) {
	var body struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Continue           string `json:"continue"`
			RemainingItemCount *int64 `json:"remainingItemCount"`
		} `json:"metadata"`
		Items []json.RawMessage `json:"items"`
	}

	if err := json.Unmarshal(data, &body); err != nil {
		return kubePage{}, fmt.Errorf("decode Kubernetes API answer: %w", err)
	}

	page := kubePage{continueToken: body.Metadata.Continue, remaining: -1, items: body.Items}
	if body.Metadata.RemainingItemCount != nil {
		page.remaining = *body.Metadata.RemainingItemCount
	}

	if body.Kind == "Table" {
		var table kubeTable
		if err := json.Unmarshal(data, &table); err != nil {
			return kubePage{}, fmt.Errorf("decode Kubernetes API table: %w", err)
		}

		page.table, page.items = &table, nil
	}

	return page, nil
}

func clampPageLimit(limit int) int {
	switch {
	case limit <= 0:
		return kubePageLimit
	case limit > kubePageMaxLimit:
		return kubePageMaxLimit
	default:
		return limit
	}
}

// isKubeExpired tells a 410 Gone: the list must start again from its first page.
func isKubeExpired(err error) bool {
	var apiErr *kubeAPIError

	return errors.As(err, &apiErr) && apiErr.Code == http.StatusGone
}

// listAll reads every page of path into fn. When the API expires the list mid-way it calls
// restart, so the caller drops what it collected, and starts again (kubeExpiredRetries times).
func (k *kubeClient) listAll(ctx context.Context, path string, q pageQuery, restart func(), fn func(kubePage) error) error {
	for attempt := 0; ; attempt++ {
		err := k.eachPage(ctx, path, q, fn)
		if err == nil || !isKubeExpired(err) || attempt == kubeExpiredRetries {
			return err
		}

		restart()
	}
}

func (k *kubeClient) eachPage(ctx context.Context, path string, q pageQuery, fn func(kubePage) error) error {
	q.continueToken = ""

	for {
		page, err := k.getPage(ctx, path, q)
		if err != nil {
			return err
		}

		if err := fn(page); err != nil {
			return err
		}

		if page.continueToken == "" {
			return nil
		}

		q.continueToken = page.continueToken
	}
}

// decodeItems decodes the objects of a JSON page.
func decodeItems[T any](page kubePage) ([]T, error) {
	out := make([]T, 0, len(page.items))

	for _, raw := range page.items {
		var obj T
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf("decode Kubernetes object: %w", err)
		}

		out = append(out, obj)
	}

	return out, nil
}

// pageCursor is what a page function returns besides its rows. Continue is the API's token
// hex-encoded: the result goes through the privacy mask, which must not rewrite it (a word or
// an address inside base64), and hex has no word boundary nor dot to match.
type pageCursor struct {
	Continue  string `json:"continue"`  // "" once complete
	Remaining int64  `json:"remaining"` // items left after this page, -1 unknown
	Complete  bool   `json:"complete"`
}

func cursorOf(page kubePage) pageCursor {
	return pageCursor{
		Continue:  hex.EncodeToString([]byte(page.continueToken)),
		Remaining: page.remaining,
		Complete:  page.continueToken == "",
	}
}

// completeCursor is the cursor of a list read in one go (the demo inventory).
var completeCursor = pageCursor{Remaining: 0, Complete: true}

// decodeContinue reads a token a page function returned.
func decodeContinue(token string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return "", errors.New("invalid continue token")
	}

	return string(raw), nil
}

// validateNamespace checks a namespace scope: "" (every namespace) or a Kubernetes name.
func validateNamespace(namespace string) error {
	if namespace == "" {
		return nil
	}

	if !kubeNamePattern.MatchString(namespace) || strings.Contains(namespace, "..") {
		return fmt.Errorf("invalid Kubernetes namespace %q", namespace)
	}

	return nil
}

// scopedPath is the collection of resource in namespace ("" for every namespace) under
// group ("/api/v1", "/apis/apps/v1").
func scopedPath(group, namespace, resource string) string {
	if namespace == "" {
		return group + "/" + resource
	}

	return group + "/namespaces/" + url.PathEscape(namespace) + "/" + resource
}

// pageArgs are the arguments every page function takes from the app, checked.
type pageArgs struct {
	namespace     string
	continueToken string
	limit         int
}

func newPageArgs(namespace, continueToken string, limit int) (pageArgs, error) {
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))
	if err := validateNamespace(namespace); err != nil {
		return pageArgs{}, err
	}

	token, err := decodeContinue(continueToken)
	if err != nil {
		return pageArgs{}, err
	}

	return pageArgs{namespace: namespace, continueToken: token, limit: limit}, nil
}

// inNamespace keeps the demo rows of namespace ("" keeps all).
func inNamespace[T any](rows []T, namespace string, of func(T) string) []T {
	if namespace == "" {
		return rows
	}

	out := []T{}

	for _, r := range rows {
		if of(r) == namespace {
			out = append(out, r)
		}
	}

	return out
}
