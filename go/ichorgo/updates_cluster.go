package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/internal/upstream"
)

// The cluster's update candidates (ClusterUpdates): every image its workloads run and every
// chart its Flux, Argo CD and Helm releases install, with the newer versions step 1's lookup
// finds (updates_lookup.go), who owns each, whether something already bumps it, and the
// one-line edit that would. The cluster is read first, the registries asked after, so a
// lookup failure never hides the inventory. Nothing is written.

const updatesMaxWorkloads = 20 // workloads listed per image, the rest counted in "more"

type updateOwner struct {
	Tool      string `json:"tool"` // flux | argocd | helm | "" for a bare workload
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// updateEdit is the quick edit (gitops_quickedit.go) that applies the first candidate; the
// file it applies to is found on demand with GitOpsSourceFind.
type updateEdit struct {
	Op         string   `json:"op"` // image | path
	Kind       string   `json:"kind,omitempty"`
	Name       string   `json:"name,omitempty"`
	Container  string   `json:"container,omitempty"`
	Image      string   `json:"image,omitempty"`
	Path       string   `json:"path"`
	Value      string   `json:"value,omitempty"`
	Hint       string   `json:"hint,omitempty"`
	ValueFiles []string `json:"valueFiles,omitempty"`
}

type updateWorkloadRef struct {
	Kind       string   `json:"kind"`
	Namespace  string   `json:"namespace"`
	Name       string   `json:"name"`
	Containers []string `json:"containers"`
}

type updateImage struct {
	Repo        string              `json:"repo"`
	Tag         string              `json:"tag"`
	Digest      string              `json:"digest"`
	Workloads   []updateWorkloadRef `json:"workloads"`
	More        int                 `json:"more,omitempty"`
	Owner       *updateOwner        `json:"owner"`
	Credentials string              `json:"credentials,omitempty"` // "not readable" when its pull secrets cannot be read
	upstream.Candidate
	AutomatedBy string      `json:"automatedBy,omitempty"`
	Edit        *updateEdit `json:"edit"`

	ref        string   // as the workloads name it
	secrets    []string // namespace/name of the pull secrets of its workloads
	rendered   bool     // a chart renders its workloads: the tag is in the chart's values
	valueFiles []string // the owning chart's values files, when a chart renders it
}

type updateChart struct {
	Name       string       `json:"name"`
	Version    string       `json:"version"`
	RepoURL    string       `json:"repoURL"`
	Owner      *updateOwner `json:"owner"`
	Constraint string       `json:"constraint,omitempty"`
	upstream.Candidate
	AutomatedBy string      `json:"automatedBy,omitempty"`
	Edit        *updateEdit `json:"edit"`

	editPath   string      // the field holding the version in the owner's manifest, "" for none
	editTarget *updateEdit // the object holding editPath when it is not the owner (an OCIRepository)
}

type updateSkipped struct {
	What   string `json:"what"`
	Reason string `json:"reason"`
}

type updateLookups struct {
	Queried int `json:"queried"`
	Failed  int `json:"failed"`
}

type clusterUpdates struct {
	At      int64           `json:"at"`
	Images  []*updateImage  `json:"images"`
	Charts  []*updateChart  `json:"charts"`
	Skipped []updateSkipped `json:"skipped"`
	Lookups updateLookups   `json:"lookups"`

	auth upstream.Auth // the pull secrets read, for the lookups
}

// updateOptions is ClusterUpdates' optionsJSON.
type updateOptions struct {
	Namespaces []string `json:"namespaces"` // empty: every namespace
	// RepoURL and Ref name the cluster's saved repository: with hostJSON, a dependency bot
	// configuration found there marks the items it bumps.
	RepoURL string `json:"repoURL"`
	Ref     string `json:"ref"`
}

func (o updateOptions) wants(namespace string) bool {
	return len(o.Namespaces) == 0 || slices.Contains(o.Namespaces, namespace)
}

// ClusterUpdates lists the cluster's images and charts with their newer versions.
// optionsJSON (may be empty): {"namespaces":[],"repoURL","ref"}; hostJSON (may be empty) is
// the forge of repoURL. → {"at","images":[{repo,tag,digest,workloads,more,owner,credentials,
// current,patch,minor,major,prerelease,shape,reason,source,automatedBy,edit}],"charts":[{name,
// version,repoURL,owner,constraint,current,patch,minor,major,…,automatedBy,edit}],"skipped":
// [{what,reason}],"lookups":{queried,failed}}, outdated first (major, minor, patch), then by
// name. A kind the cluster does not serve or the role cannot list is noted in "skipped".
func ClusterUpdates(configYAML, contextName, kubeServer, hostJSON, optionsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var opts updateOptions
	if strings.TrimSpace(optionsJSON) != "" {
		if err := json.Unmarshal([]byte(optionsJSON), &opts); err != nil {
			return "", fmt.Errorf("options: %w", err)
		}
	}

	for i, ns := range opts.Namespaces {
		opts.Namespaces[i] = privacy.revealNamespace(strings.TrimSpace(ns))
	}

	contextName = unmaskContext(configYAML, contextName)
	if isDemoContext(configYAML, contextName) {
		demo := demoClusterUpdates(time.Now())
		sortUpdates(&demo)

		return toJSON(demo)
	}

	result, err := withKube(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) (clusterUpdates, error) {
		return collectUpdates(ctx, k, opts)
	})
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), updatesTimeout)
	defer cancel()

	lookupUpdates(ctx, updatesClient, &result)

	if strings.TrimSpace(hostJSON) != "" && opts.RepoURL != "" {
		markBotUpdates(&result, hostJSON, opts.RepoURL, opts.Ref)
	}

	finishUpdates(&result)

	return toJSON(result)
}

// lookupUpdates asks the registries about every image and chart that has a repository.
func lookupUpdates(ctx context.Context, c *upstream.Client, r *clusterUpdates) {
	var (
		targets []upstream.Target
		apply   []func(upstream.Candidate)
	)

	for _, img := range r.Images {
		img.Current = img.Tag
		if img.Digest != "" && img.Tag == "" {
			img.Reason = upstream.ReasonDigest

			continue
		}

		targets = append(targets, upstream.Target{Kind: "image", Ref: img.ref})
		apply = append(apply, func(c upstream.Candidate) { img.Candidate = c })
	}

	for _, ch := range r.Charts {
		ch.Current = ch.Version
		if ch.RepoURL == "" || ch.Version == "" {
			ch.Reason = cmp.Or(ch.Reason, "chart repository unknown")

			continue
		}

		targets = append(targets, upstream.Target{Kind: "chart", RepoURL: ch.RepoURL, Name: ch.Name, Version: ch.Version})
		apply = append(apply, func(c upstream.Candidate) { ch.Candidate = c })
	}

	if len(targets) > updatesMaxTargets {
		r.Skipped = append(r.Skipped, updateSkipped{What: "lookups", Reason: fmt.Sprintf("%d images and charts, the first %d looked up", len(targets), updatesMaxTargets)})
		targets, apply = targets[:updatesMaxTargets], apply[:updatesMaxTargets]
	}

	r.Lookups.Queried = len(targets)

	for i, res := range c.LookupAll(ctx, targets, r.auth) {
		cand := res.Candidate
		if res.Err != nil {
			r.Lookups.Failed++
			cand = upstream.Candidate{Current: cmp.Or(cand.Current, targetVersion(targets[i])), Reason: lookupReason(res.Err)}
		}

		apply[i](cand)
	}
}

func targetVersion(t upstream.Target) string {
	if t.Kind == "chart" {
		return t.Version
	}

	return parseImageRef(t.Ref).Tag
}

// lookupReason says why a registry gave no answer; one the phone cannot reach (a registry on
// the cluster's network only) says so rather than a raw dial error.
func lookupReason(err error) string {
	msg := err.Error()
	for _, s := range []string{"dial tcp", "no such host", "connection refused", "i/o timeout", "network is unreachable"} {
		if strings.Contains(msg, s) {
			return "registry not reachable from the phone"
		}
	}

	return friendlyError(err)
}

// finishUpdates keeps the candidates a version range leaves out, fills the edits and sorts.
func finishUpdates(r *clusterUpdates) {
	for _, ch := range r.Charts {
		if ch.Constraint != "" {
			dropInRange(&ch.Candidate, ch.Constraint)
		}

		ch.Edit = chartEdit(ch)
	}

	for _, img := range r.Images {
		img.Edit = imageEdit(img)
	}

	sortUpdates(r)
}

// sortUpdates puts the outdated items first (major, minor, patch), then by name.
func sortUpdates(r *clusterUpdates) {
	slices.SortStableFunc(r.Images, func(a, b *updateImage) int {
		return cmp.Or(cmp.Compare(updateRank(a.Candidate), updateRank(b.Candidate)), cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.Tag, b.Tag))
	})
	slices.SortStableFunc(r.Charts, func(a, b *updateChart) int {
		return cmp.Or(cmp.Compare(updateRank(a.Candidate), updateRank(b.Candidate)), cmp.Compare(a.Name, b.Name), cmp.Compare(ownerKey(a.Owner), ownerKey(b.Owner)))
	})
}

// updateRank orders major before minor before patch before nothing to do.
func updateRank(c upstream.Candidate) int {
	switch {
	case c.Major != "":
		return 0
	case c.Minor != "":
		return 1
	case c.Patch != "":
		return 2
	}

	return 3
}

// firstCandidate is the version an edit proposes: the smallest step that exists.
func firstCandidate(c upstream.Candidate) string {
	return cmp.Or(c.Patch, c.Minor, c.Major)
}

func ownerKey(o *updateOwner) string {
	if o == nil {
		return ""
	}

	return o.Namespace + "/" + o.Name
}
