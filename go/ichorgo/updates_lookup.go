package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/internal/upstream"
)

const (
	updatesMaxTargets = 500
	updatesTimeout    = 2 * time.Minute
)

// updatesClient is shared by every call so its 6 h cache lasts the app's life.
var updatesClient = upstream.New(upstream.Options{})

type updateCandidate struct {
	Target upstream.Target `json:"target"`
	upstream.Candidate
}

type updateCandidates struct {
	Candidates []updateCandidate `json:"candidates"`
}

// UpdateCandidates says which newer versions of images and charts exist.
// targetsJSON: [{"kind":"image","ref":"ghcr.io/example-org/app:1.2.3"},
// {"kind":"chart","repoURL":"https://charts.example.com" or "oci://…","name","version"}].
// authJSON (may be empty): a Docker config.json {"auths":{host:{"auth"}}} plus an optional
// "helm":{"username","password"} for https chart repositories.
// → {"candidates":[{"target","current","patch","minor","major","prerelease","shape","reason",
// "source"}]} in the input order; a target that cannot be looked up says why in "reason".
func UpdateCandidates(targetsJSON, authJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var targets []upstream.Target
	if err := json.Unmarshal([]byte(targetsJSON), &targets); err != nil {
		return "", fmt.Errorf("targets: %w", err)
	}

	if len(targets) > updatesMaxTargets {
		return "", fmt.Errorf("%d targets, at most %d", len(targets), updatesMaxTargets)
	}

	var auth upstream.Auth
	if strings.TrimSpace(authJSON) != "" {
		if err := json.Unmarshal([]byte(authJSON), &auth); err != nil {
			return "", fmt.Errorf("credentials: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), updatesTimeout)
	defer cancel()

	result := updateCandidates{Candidates: make([]updateCandidate, len(targets))}

	for i, r := range updatesClient.LookupAll(ctx, targets, auth) {
		c := r.Candidate
		if r.Err != nil {
			c.Reason = friendlyError(r.Err)
		}

		result.Candidates[i] = updateCandidate{Target: targets[i], Candidate: c}
	}

	// Tags and registries are not names the privacy mask learned: masking leaves them alone.
	return toJSON(result)
}
