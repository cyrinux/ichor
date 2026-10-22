package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cyrinux/ichor/go/gitforge"
)

// GitForgePropose commits one file of repoURL on a new branch from baseRef and opens a pull
// (GitHub, Gitea) or merge (GitLab) request, with hostJSON's token. optionsJSON is
// {"repoURL","baseRef","path","content","fileSha","message","branch","title","body","draft"};
// fileSha is GitForgeFile's sha ("" for a new file), branch defaults to ichor/<title>-<hex>.
// For several files, "files":[{"path","content","fileSha","delete"}] (1 to 20) replaces
// path, content and fileSha. The answer is {"url","number","branch","commit","commits","forge"}. configYAML and contextName name
// the app's cluster, for the audit entry and the demo check only.
func GitForgePropose(configYAML, contextName, hostJSON, optionsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	var (
		res    gitforge.ProposeResult
		target string
	)

	// recordedRun builds the entry once the request is open, with its URL; never the token
	// nor the content.
	err = recordedRun(configYAML, contextName, func() auditAction {
		return auditAction{Action: "gitops-propose", Object: target, Params: res.URL}
	}, func() error {
		var err error
		res, err = gitForgePropose(configYAML, contextName, hostJSON, optionsJSON, &target)

		return err
	})
	if err != nil {
		return "", err
	}

	return gitForgeJSON(res)
}

// gitForgePropose is GitForgePropose once its context is unmasked; target gets the
// repository and path once they are known.
func gitForgePropose(configYAML, contextName, hostJSON, optionsJSON string, target *string) (gitforge.ProposeResult, error) {
	h, err := gitforge.ParseHost(hostJSON)
	if err != nil {
		return gitforge.ProposeResult{}, err
	}

	if h.Kind == gitforge.KindDemo || configYAML != "" && isDemoContext(configYAML, contextName) {
		return gitforge.ProposeResult{}, errDemoUnavailable
	}

	var p gitforge.Proposal
	if err := json.Unmarshal([]byte(optionsJSON), &p); err != nil {
		return gitforge.ProposeResult{}, fmt.Errorf("invalid proposal: %w", err)
	}

	if privacy.isEnabled() {
		return gitforge.ProposeResult{}, errGitProposePrivacy
	}

	h, repo, p, err := gitforge.CheckProposal(hostJSON, p)
	if err != nil {
		return gitforge.ProposeResult{}, err
	}

	*target = repo.Full() + ":" + gitforge.ProposalPaths(p)

	ctx, cancel := context.WithTimeout(context.Background(), gitforge.ProposeTimeout)
	defer cancel()

	res, err := gitforge.Propose(ctx, gitforge.New(h), repo, p)
	res.Forge = h.Kind

	return res, err
}

// errGitProposePrivacy refuses a proposal in screenshot mode: the file was read masked, and
// masking cannot be undone exactly, so the commit could rewrite lines nobody edited.
var errGitProposePrivacy = errors.New("turn off screenshot mode to propose a change: the file was read with masked names")
