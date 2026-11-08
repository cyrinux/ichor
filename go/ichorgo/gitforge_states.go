package ichorgo

import (
	"cmp"
	"encoding/json"
	"fmt"

	"github.com/cyrinux/ichor/go/gitforge"
)

// gitRequestStatesMax: the most requests one GitForgeRequestStates call reads (one forge call each).
const gitRequestStatesMax = 20

type gitRequestRef struct {
	RepoURL string `json:"repoURL"`
	Number  int    `json:"number"`
}

type gitRequestStateItem struct {
	RepoURL  string `json:"repoURL"`
	Number   int    `json:"number"`
	State    string `json:"state"`
	MergedAt string `json:"mergedAt"`
	ClosedAt string `json:"closedAt"`
	SHA      string `json:"sha"`
	Error    string `json:"error"`
}

// GitForgeRequestStates reads the state of several pull requests on hostJSON's forge, for the
// background monitor's "merged" and "closed" alerts: requestsJSON is [{"repoURL","number"}], the
// answer [{"repoURL","number","state":"open|merged|closed","mergedAt","closedAt","sha","error"}]
// in the same order, sha the merge commit (else the head). One forge call per request (see
// GitForgeRequest), 20 at most per call; a request that cannot be read (gone, a 403 or 429) has
// its error and an empty state, and the others are still read.
func GitForgeRequestStates(hostJSON, requestsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var refs []gitRequestRef
	if err := json.Unmarshal([]byte(requestsJSON), &refs); err != nil {
		return "", fmt.Errorf("invalid requests: %w", err)
	}

	if len(refs) > gitRequestStatesMax {
		return "", fmt.Errorf("at most %d requests per call (%d given)", gitRequestStatesMax, len(refs))
	}

	items := make([]gitRequestStateItem, 0, len(refs))

	for _, ref := range refs {
		items = append(items, readRequestState(hostJSON, ref))
	}

	return toJSON(items)
}

func readRequestState(hostJSON string, ref gitRequestRef) gitRequestStateItem {
	item := gitRequestStateItem{RepoURL: ref.RepoURL, Number: ref.Number}

	raw, err := GitForgeRequest(hostJSON, ref.RepoURL, ref.Number)
	if err == nil {
		var s gitforge.RequestState
		if err = json.Unmarshal([]byte(raw), &s); err == nil {
			item.State, item.MergedAt, item.SHA = s.State, s.MergedAt, cmp.Or(s.MergeSHA, s.HeadSHA)
			if s.State == gitforge.RequestClosed {
				item.ClosedAt = s.UpdatedAt
			}
		}
	}

	if err != nil {
		item.Error = err.Error()
	}

	return item
}
