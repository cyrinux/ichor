package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Data services (Longhorn, Garage, CloudNativePG, Velero…) in the AI diagnosis and the support
// bundle: what the Data services screen reads, through the Kubernetes API with the os:admin
// kubeconfig. Best effort, like the GitOps section: a role without os:admin, or an API server
// out of reach, leaves a note instead of failing the report.

const (
	// dataServicesCollectTimeout bounds the reads (Garage runs its CLI in a pod), well inside
	// the diagnosis' own minute.
	dataServicesCollectTimeout = 25 * time.Second
	// dataServicesMaxItems caps the items listed per section in the report.
	dataServicesMaxItems = 15
)

// dataServicesState is what the data services said, or why they could not be read. The
// sections keep the JSON shape of KubeDataServices.
type dataServicesState struct {
	dataServices

	// Note says why nothing could be read ("" when it was).
	Note string `json:"note,omitempty"`
}

// collectDataServices reads the data services when the roles allow the Kubernetes API; nil
// without os:admin or when the cluster runs none.
func collectDataServices(ctx context.Context, target kubeTarget, roles []string) *dataServicesState {
	if !slices.Contains(roles, adminRole) {
		return nil
	}

	if isDemoContext(target.config, target.context) {
		return &dataServicesState{dataServices: demoDataServices(time.Now())}
	}

	ctx, cancel := context.WithTimeout(ctx, dataServicesCollectTimeout)
	defer cancel()

	ds, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (dataServices, error) {
		return readDataServices(ctx, k, k.exec, parseHints(""), time.Now())
	})
	if err != nil {
		return &dataServicesState{Note: kubeError(err).Error()}
	}

	if ds == (dataServices{}) {
		return nil
	}

	return &dataServicesState{dataServices: ds}
}

// renderDataServices writes the DATA SERVICES section of the diagnosis: a count per service,
// then the items that are not fine, worst first, with the nodes involved.
func renderDataServices(b *strings.Builder, s *dataServicesState) {
	if s == nil {
		return
	}

	b.WriteString("DATA SERVICES\n")

	if s.Note != "" {
		fmt.Fprintf(b, "  could not be read: %s\n\n", s.Note)

		return
	}

	for _, sec := range dataServiceSections(s.dataServices) {
		renderDataServiceSection(b, sec)
	}

	b.WriteString("\n")
}

func renderDataServiceSection(b *strings.Builder, sec dsSection) {
	if sec.summary != "" {
		fmt.Fprintf(b, "  %s: %s\n", sec.name, sec.summary)
	}

	bad := slices.DeleteFunc(slices.Clone(sec.issues), func(i dsIssue) bool {
		return i.health != healthCritical && i.health != healthWarning
	})
	slices.SortStableFunc(bad, func(x, y dsIssue) int { return healthRank(x.health) - healthRank(y.health) })

	for i, issue := range bad {
		if i == dataServicesMaxItems {
			fmt.Fprintf(b, "    … and %d more\n", len(bad)-i)

			break
		}

		head, nodes := issue.key+" ["+issue.health+"]", ""
		if len(issue.nodes) > 0 {
			nodes = "; nodes " + strings.Join(issue.nodes, ", ")
		}

		// A long detail (Ceph's checks) is cut before the nodes: they are the likely cause.
		line := head
		if issue.detail != "" {
			line += ": " + clipUTF8(issue.detail, max(diagnosisMaxLineLen/3, diagnosisMaxLineLen-len(head)-len(nodes)-len(": …")))
		}

		fmt.Fprintf(b, "    %s\n", clipUTF8(line+nodes, diagnosisMaxLineLen))
	}

	if sec.err != "" {
		fmt.Fprintf(b, "  %s could not be read: %s\n", sec.name, clipUTF8(sec.err, diagnosisMaxLineLen))
	}
}

// urlPassword is the "user:password@" of a URL, read as text: url.Parse refuses some that
// still hold one (a bad escape, a port that is not a number).
var urlPassword = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*://)[^/@?#]*:[^@\s]*@`)

// stripURLPassword removes "user:password@" from a URL. A user without a password stays:
// Longhorn writes the bucket there ("s3://bucket@region/").
func stripURLPassword(raw string) string {
	return urlPassword.ReplaceAllString(raw, "$1")
}

// scrubDataServices drops what could hold a credential before the state goes into a support
// bundle: the user and password part of Longhorn's backup target URLs. Bucket names, hosts and
// IDs stay: the bundle is for the operator.
func scrubDataServices(s dataServicesState) dataServicesState {
	if s.Longhorn != nil {
		l := *s.Longhorn
		l.BackupTargets = slices.Clone(l.BackupTargets)

		for i := range l.BackupTargets {
			l.BackupTargets[i].URL = stripURLPassword(l.BackupTargets[i].URL)
		}

		s.Longhorn = &l
	}

	return s
}

// collectDataServicesFile writes the data services' state, without credentials. Nothing when
// the role has no os:admin or the cluster runs none.
func collectDataServicesFile(ctx context.Context, s *session, kube kubeTarget) ([]bundleFile, error) {
	summary, err := summarizeContext("", s.context)
	if err != nil {
		return nil, err
	}

	return dataServicesFile(ctx, kube, summary.Roles)
}

func dataServicesFile(ctx context.Context, kube kubeTarget, roles []string) ([]bundleFile, error) {
	state := collectDataServices(ctx, kube, roles)
	if state == nil {
		return nil, nil
	}

	if state.Note != "" {
		return nil, errors.New(state.Note)
	}

	data, err := json.MarshalIndent(scrubDataServices(*state), "", "  ")
	if err != nil {
		return nil, err
	}

	return []bundleFile{{name: "dataservices.json", content: data}}, nil
}
