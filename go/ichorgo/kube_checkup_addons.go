package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The findings of the secrets section (External Secrets Operator).
const (
	findExternalSecretFailed = "externalSecretFailed" // Extra: its store; Reason, Message: its Ready condition's
	findSecretStoreNotReady  = "secretStoreNotReady"  // Extra: SecretStore or ClusterSecretStore
)

// The findings of the helm section. Name: the release, Reason: its status, Count: its revision.
const (
	findHelmFailed  = "helmFailed"
	findHelmPending = "helmPending" // pending-install, -upgrade or -rollback for too long: a lock nobody holds
)

const (
	groupExternalSecrets = "external-secrets.io"
	helmSelector         = "owner=helm"
	// helmPendingGrace is how long an install or upgrade may run before it is stuck.
	helmPendingGrace   = 10 * time.Minute
	checkupMaxReleases = 300
)

type externalSecretObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		SecretStoreRef struct {
			Name string `json:"name"`
		} `json:"secretStoreRef"`
	} `json:"spec"`
	Status struct {
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}

// checkupExternalSecrets finds the ExternalSecrets that do not sync and the stores that
// are not ready. Absent without the operator.
func checkupExternalSecrets(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	version, ok := in.groups[groupExternalSecrets]
	if !ok {
		return absentSection(checkSecrets)
	}

	base := "/apis/" + groupExternalSecrets + "/" + version + "/"
	secrets, err := listObjects[externalSecretObject](ctx, k, base+"externalsecrets")
	stores, storeErr := listObjects[externalSecretObject](ctx, k, base+"secretstores")
	clusterStores, clusterErr := listObjects[externalSecretObject](ctx, k, base+"clustersecretstores")

	findings := storeFindings("SecretStore", stores)
	findings = append(findings, storeFindings("ClusterSecretStore", clusterStores)...)

	for _, s := range secrets {
		if c := readyCondition(s.Status.Conditions); c.Status == "False" {
			findings = append(findings, checkupFinding{
				Kind: findExternalSecretFailed, Severity: sevWarning, Namespace: s.Metadata.Namespace, Name: s.Metadata.Name,
				Extra: s.Spec.SecretStoreRef.Name, Reason: c.Reason, Message: c.Message,
			})
		}
	}

	return newSection(checkSecrets, len(secrets)+len(stores)+len(clusterStores), findings,
		ignoreNotFound(err), ignoreNotFound(storeErr), ignoreNotFound(clusterErr))
}

// storeFindings are the stores that cannot reach their backend: every secret of theirs
// goes stale.
func storeFindings(kind string, stores []externalSecretObject) []checkupFinding {
	findings := []checkupFinding{}

	for _, s := range stores {
		if c := readyCondition(s.Status.Conditions); c.Status == "False" {
			findings = append(findings, checkupFinding{
				Kind: findSecretStoreNotReady, Severity: sevCritical, Namespace: s.Metadata.Namespace, Name: s.Metadata.Name,
				Extra: kind, Reason: c.Reason, Message: c.Message,
			})
		}
	}

	return findings
}

// checkupRelease is a Helm release at its latest revision, as `helm list -A` shows it.
type checkupRelease struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"` // deployed, failed, pending-upgrade, uninstalling...
	Revision  int    `json:"revision"`
	Updated   int64  `json:"updated"` // unix ms
}

// checkupHelm reads the Helm releases from the labels of their Secrets (never the payload)
// and finds those that failed or hang. Absent when Helm installed nothing.
func checkupHelm(ctx context.Context, k *kubeClient, in checkupInput) (checkupSection, []checkupRelease) {
	metas := []kubeRowMeta{}

	err := k.listAll(ctx, "/api/v1/secrets", pageQuery{labelSelector: helmSelector, table: true}, func() { metas = metas[:0] }, func(page kubePage) error {
		if page.table != nil {
			for _, row := range page.table.Rows {
				metas = append(metas, row.Object.Metadata)
			}

			return nil
		}

		for _, raw := range page.items {
			var obj struct {
				Metadata kubeRowMeta `json:"metadata"`
			}

			if err := json.Unmarshal(raw, &obj); err != nil {
				return err
			}

			metas = append(metas, obj.Metadata)
		}

		return nil
	})
	if err != nil {
		return newSection(checkHelm, 0, nil, err), nil
	}

	releases := helmReleases(metas)
	if len(releases) == 0 {
		return absentSection(checkHelm), nil
	}

	section := newSection(checkHelm, len(releases), helmFindings(releases, in.now))

	if len(releases) > checkupMaxReleases {
		releases = releases[:checkupMaxReleases]
	}

	return section, releases
}

// helmReleases keeps the latest revision of each release, in trouble first.
func helmReleases(metas []kubeRowMeta) []checkupRelease {
	latest := map[string]checkupRelease{}

	for _, m := range metas {
		name := m.Labels["name"]
		if name == "" {
			continue
		}

		revision, _ := strconv.Atoi(m.Labels["version"]) //nolint:errcheck // 0 when unreadable
		r := checkupRelease{Namespace: m.Namespace, Name: name, Status: m.Labels["status"], Revision: revision, Updated: milli(m.CreationTimestamp)}

		if seconds, err := strconv.ParseInt(m.Labels["modifiedAt"], 10, 64); err == nil && seconds > 0 {
			r.Updated = seconds * 1000
		}

		if key := m.Namespace + "/" + name; revision >= latest[key].Revision {
			latest[key] = r
		}
	}

	releases := make([]checkupRelease, 0, len(latest))
	for _, r := range latest {
		releases = append(releases, r)
	}

	slices.SortFunc(releases, func(a, b checkupRelease) int {
		return cmp.Or(helmRank(a.Status)-helmRank(b.Status), strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name))
	})

	return releases
}

func helmRank(status string) int {
	switch {
	case status == "failed":
		return 0
	case strings.HasPrefix(status, "pending-"):
		return 1
	case status == "deployed":
		return 3
	default:
		return 2
	}
}

func helmFindings(releases []checkupRelease, now time.Time) []checkupFinding {
	findings := []checkupFinding{}

	for _, r := range releases {
		f := checkupFinding{Namespace: r.Namespace, Name: r.Name, Reason: r.Status, Count: r.Revision, Since: r.Updated}

		switch {
		case r.Status == "failed":
			f.Kind, f.Severity = findHelmFailed, sevWarning
		case strings.HasPrefix(r.Status, "pending-") && olderThan(time.UnixMilli(r.Updated), now, helmPendingGrace):
			f.Kind, f.Severity = findHelmPending, sevWarning
		default:
			continue
		}

		findings = append(findings, f)
	}

	return findings
}
