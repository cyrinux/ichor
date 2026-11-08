package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Whether an Argo CD or Flux app runs a commit (a merged proposal's): its revision is that
// commit, or Argo CD's history holds it. A later commit on the branch is told apart only as
// "another revision" (no walk of the commit graph): the apps word it as a newer commit.

const (
	revisionToolArgo = "argocd"
	revisionToolFlux = "flux"
)

var errRevisionSHA = errors.New("no commit given")

type revisionRuns struct {
	Runs bool `json:"runs"`
	// Revision is the commit the app runs ("" when it has none yet).
	Revision string `json:"revision"`
	// Synced: the app is synced (Argo CD) or ready on its last attempt (Flux).
	Synced bool `json:"synced"`
}

// GitOpsRevisionRuns tells whether tool's ("argocd" or "flux") app kind namespace/name runs
// the commit sha: {"runs","revision","synced"}. Argo CD: status.sync.revision(s) or a revision
// of status.history; Flux: status.lastAppliedRevision ("main@sha1:<sha>"). kubeServer: see
// KubePods.
func GitOpsRevisionRuns(configYAML, contextName, kubeServer, tool, kind, namespace, name, sha string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	sha = strings.ToLower(strings.TrimSpace(sha))
	if sha == "" {
		return "", errRevisionSHA
	}

	t := kubeTarget{configYAML, contextName, kubeServer}

	switch tool {
	case revisionToolArgo:
		return kubeReadJSON(t, func() revisionRuns { return demoRevisionRuns(tool, namespace, name, sha) }, func(ctx context.Context, k *kubeClient) (revisionRuns, error) {
			return argoRevisionRuns(ctx, k, namespace, name, sha)
		})
	case revisionToolFlux:
		return kubeReadJSON(t, func() revisionRuns { return demoRevisionRuns(tool, namespace, name, sha) }, func(ctx context.Context, k *kubeClient) (revisionRuns, error) {
			return fluxRevisionRuns(ctx, k, kind, namespace, name, sha)
		})
	}

	return "", fmt.Errorf("unknown GitOps tool %q", tool)
}

func argoRevisionRuns(ctx context.Context, k *kubeClient, namespace, name, sha string) (revisionRuns, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return revisionRuns{}, err
	}

	version, ok := groups[groupArgo]
	if !ok {
		return revisionRuns{}, &kubeAPIError{Code: 404, Reason: "NotFound", Message: "Argo CD is not installed"}
	}

	var o struct {
		Status struct {
			Sync struct {
				Status    string   `json:"status"`
				Revision  string   `json:"revision"`
				Revisions []string `json:"revisions"`
			} `json:"sync"`
			History []struct {
				Revision  string   `json:"revision"`
				Revisions []string `json:"revisions"`
			} `json:"history"`
		} `json:"status"`
	}

	path := "/apis/" + groupArgo + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/applications/" + url.PathEscape(name)
	if err := k.get(ctx, path, &o); err != nil {
		return revisionRuns{}, err
	}

	current := append([]string{o.Status.Sync.Revision}, o.Status.Sync.Revisions...)
	past := []string{}

	for _, h := range o.Status.History {
		past = append(past, h.Revision)
		past = append(past, h.Revisions...)
	}

	return runsOf(current, past, sha, o.Status.Sync.Status == "Synced"), nil
}

func fluxRevisionRuns(ctx context.Context, k *kubeClient, kind, namespace, name, sha string) (revisionRuns, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return revisionRuns{}, err
	}

	path, err := fluxPath(groups, kind, namespace, name)
	if err != nil {
		return revisionRuns{}, err
	}

	var o struct {
		Status struct {
			LastAppliedRevision   string         `json:"lastAppliedRevision"`
			LastAttemptedRevision string         `json:"lastAttemptedRevision"`
			Conditions            kubeConditions `json:"conditions"`
		} `json:"status"`
	}

	if err := k.get(ctx, path, &o); err != nil {
		return revisionRuns{}, err
	}

	st := o.Status
	synced := st.Conditions.get("Ready").Status == "True" && st.LastAttemptedRevision == st.LastAppliedRevision

	return runsOf([]string{st.LastAppliedRevision}, nil, sha, synced), nil
}

// runsOf tells whether sha is among the current revisions (or, for Argo CD, the past ones the
// app synced to and has not left: a later revision would be current then); revision is the
// first current one, as a commit.
func runsOf(current, past []string, sha string, synced bool) revisionRuns {
	res := revisionRuns{Synced: synced}

	for _, r := range current {
		if c := revisionCommit(r); c != "" {
			if res.Revision == "" {
				res.Revision = c
			}

			if sameCommit(c, sha) {
				res.Runs, res.Revision = true, c
			}
		}
	}

	if !res.Runs && res.Revision == "" {
		res.Runs = slices.ContainsFunc(past, func(r string) bool { return sameCommit(revisionCommit(r), sha) })
	}

	return res
}

// revisionCommit is the commit of a revision: "main@sha1:<sha>" (Flux), a bare sha (Argo CD);
// "" for a chart version or a tag.
func revisionCommit(r string) string {
	r = strings.ToLower(strings.TrimSpace(r))
	if i := strings.LastIndex(r, ":"); i >= 0 {
		r = r[i+1:]
	}

	if !gitCommitSHA.MatchString(r) {
		return ""
	}

	return r
}

// sameCommit compares two commits, one of them possibly abbreviated (7 characters at least).
func sameCommit(a, b string) bool {
	if len(a) < 7 || len(b) < 7 {
		return false
	}

	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// demoRevisionRuns answers from the demo's apps.
func demoRevisionRuns(tool, namespace, name, sha string) revisionRuns {
	now := time.Now()

	if tool == revisionToolArgo {
		for _, a := range demoArgoCD(now).Apps {
			if a.Namespace == namespace && a.Name == name {
				return runsOf([]string{a.Revision}, nil, sha, a.Sync == "Synced")
			}
		}

		return revisionRuns{}
	}

	for _, a := range demoFlux(now).Apps {
		if a.Namespace == namespace && a.Name == name {
			return runsOf([]string{a.Revision}, nil, sha, a.Ready == "True")
		}
	}

	return revisionRuns{}
}
