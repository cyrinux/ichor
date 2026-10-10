package ichorgo

import (
	"context"
	"slices"
	"sync"
	"time"
)

// The sections of a checkup, in the order the apps show them.
const (
	checkWorkloads     = "workloads"
	checkEvents        = "events"
	checkStorage       = "storage"
	checkUpgrade       = "upgrade"
	checkWebhooks      = "webhooks"
	checkCapacity      = "capacity"
	checkNodes         = "nodes"
	checkLoadBalancers = "loadbalancers"
	checkTerminating   = "terminating"
	checkCertificates  = "certificates"
	checkSecrets       = "secrets"
	checkHelm          = "helm"
	checkMonitoring    = "monitoring"
)

// A section's status beyond the health values: the cluster does not run what it checks, or
// it could not be read at all.
const (
	checkAbsent  = "absent"
	checkUnknown = "unknown"
)

const (
	// checkupTimeout bounds a whole checkup: a dozen lists, and one kubelet call a node.
	checkupTimeout = 60 * time.Second
	// checkupMaxFindings caps a section's findings; Truncated counts the rest.
	checkupMaxFindings = 40
	// How long something may stay in a transient state before it is a finding.
	pendingGrace     = 5 * time.Minute
	startingGrace    = 10 * time.Minute
	terminatingGrace = 10 * time.Minute
)

// checkupReport is what the cluster hides behind green nodes: one section per kind of
// trouble, each with its findings, and the lists the apps draw (nodes, volumes, releases).
type checkupReport struct {
	Status      string           `json:"status"` // critical, warning or ok
	KubeVersion string           `json:"kubeVersion,omitempty"`
	Sections    []checkupSection `json:"sections"`
	Nodes       []checkupNode    `json:"nodes"`
	Volumes     []checkupVolume  `json:"volumes"`
	Releases    []checkupRelease `json:"releases"`
}

// checkupSection is one check. Status is the worst finding's severity (critical, warning,
// else ok), absent when the cluster does not run what it checks, unknown when it could not
// be read (Error says why). Checked counts the objects looked at.
type checkupSection struct {
	ID        string           `json:"id"`
	Status    string           `json:"status"`
	Error     string           `json:"error,omitempty"`
	Checked   int              `json:"checked"`
	Findings  []checkupFinding `json:"findings"`
	Truncated int              `json:"truncated,omitempty"`
}

// checkupFinding is one problem; the apps word each kind with its fix. Reason and Message
// are Kubernetes' own words, shown as they are. What the other fields hold depends on the
// kind, see the constants next to each check.
type checkupFinding struct {
	Kind      string  `json:"kind"`
	Severity  string  `json:"severity"`
	Namespace string  `json:"namespace,omitempty"`
	Name      string  `json:"name,omitempty"`
	Node      string  `json:"node,omitempty"`
	Reason    string  `json:"reason,omitempty"`
	Message   string  `json:"message,omitempty"`
	Extra     string  `json:"extra,omitempty"`
	Value     float64 `json:"value,omitempty"`
	Limit     float64 `json:"limit,omitempty"`
	Count     int     `json:"count,omitempty"`
	Since     int64   `json:"since,omitempty"` // unix ms
}

// KubeCheckup looks for the trouble no other screen shows, through the Kubernetes API
// (os:admin): failing pods and Jobs, Warning events, volumes filling up, deprecated APIs
// still in use, admission webhooks without a backend, nodes out of room, load balancers
// without an address, objects stuck in Terminating, pending certificate requests, external
// secrets that do not sync, Helm releases that failed, and the scrape targets down and rules
// failing in the first Prometheus discovered, as JSON (see checkupReport).
// It only reads. kubeServer: see KubePods.
func KubeCheckup(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return toJSON(demoCheckup(time.Now()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkupTimeout)
	defer cancel()

	report, err := withKubeContext(ctx, kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) (checkupReport, error) {
		return readCheckup(ctx, k, time.Now())
	})
	if err != nil {
		return "", err
	}

	return toJSON(report)
}

// checkupInput is what several sections share, read once.
type checkupInput struct {
	now      time.Time
	groups   map[string]string
	pods     []checkPod
	podsErr  error
	nodes    []checkNodeObject
	nodesErr error
	pvcs     []pvcObject
	pvcsErr  error
}

// readCheckup reads the shared lists, then every section in parallel. It fails only when
// the API server answers nothing (its API groups); a section that fails carries its error.
func readCheckup(ctx context.Context, k *kubeClient, now time.Time) (checkupReport, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return checkupReport{}, err
	}

	in := checkupInput{now: now, groups: groups}

	var (
		wg      sync.WaitGroup
		version string
	)

	wg.Go(func() { in.pods, in.podsErr = listCheckPods(ctx, k) })
	wg.Go(func() { in.nodes, in.nodesErr = listObjects[checkNodeObject](ctx, k, "/api/v1/nodes") })
	wg.Go(func() { in.pvcs, in.pvcsErr = listObjects[pvcObject](ctx, k, "/api/v1/persistentvolumeclaims") })
	wg.Go(func() { version = readAPIVersion(ctx, k) })
	wg.Wait()

	report := checkupReport{KubeVersion: version}
	sections := make([]checkupSection, 13)

	wg.Go(func() { sections[0] = checkupWorkloads(ctx, k, in) })
	wg.Go(func() { sections[1] = checkupEvents(ctx, k, in) })
	wg.Go(func() { sections[2], report.Volumes = checkupStorage(ctx, k, in) })
	wg.Go(func() { sections[3] = checkupUpgrade(ctx, k, version) })
	wg.Go(func() { sections[4] = checkupWebhooks(ctx, k) })
	wg.Go(func() { sections[5], sections[6], report.Nodes = checkupCapacity(ctx, k, in, version) })
	wg.Go(func() { sections[7] = checkupLoadBalancers(ctx, k, in) })
	wg.Go(func() { sections[8] = checkupTerminating(ctx, k, in) })
	wg.Go(func() { sections[9] = checkupCSRs(ctx, k, in) })
	wg.Go(func() { sections[10] = checkupExternalSecrets(ctx, k, in) })
	wg.Go(func() { sections[11], report.Releases = checkupHelm(ctx, k, in) })
	wg.Go(func() { sections[12] = checkupMonitoring(ctx, k, in) })
	wg.Wait()

	report.Sections = sections
	report.Status = checkupVerdict(sections)
	report.Nodes, report.Volumes, report.Releases = nonNil(report.Nodes), nonNil(report.Volumes), nonNil(report.Releases)

	return report, nil
}

// listObjects reads every item of the list at path.
func listObjects[T any](ctx context.Context, k *kubeClient, path string) ([]T, error) {
	var list kubeList[T]

	err := getList(ctx, k, path, &list)

	return list.Items, err
}

// newSection closes a section: findings worst first (in the order given otherwise), capped,
// with the status they make. errs are why a part (or all) of it could not be read.
func newSection(id string, checked int, findings []checkupFinding, errs ...error) checkupSection {
	slices.SortStableFunc(findings, func(a, b checkupFinding) int {
		return severityRank[a.Severity] - severityRank[b.Severity]
	})

	s := checkupSection{ID: id, Checked: checked, Error: sectionError(errs...), Status: healthOK}

	for _, f := range findings {
		switch {
		case f.Severity == sevCritical:
			s.Status = healthCritical
		case f.Severity == sevWarning && s.Status == healthOK:
			s.Status = healthWarning
		}
	}

	if s.Error != "" && s.Status == healthOK {
		s.Status = checkUnknown
	}

	if len(findings) > checkupMaxFindings {
		s.Truncated = len(findings) - checkupMaxFindings
		findings = findings[:checkupMaxFindings]
	}

	s.Findings = nonNil(findings)

	return s
}

// absentSection is the section of something the cluster does not run.
func absentSection(id string) checkupSection {
	return checkupSection{ID: id, Status: checkAbsent, Findings: []checkupFinding{}}
}

// checkupVerdict is the worst status of the sections that were read.
func checkupVerdict(sections []checkupSection) string {
	verdict := healthOK

	for _, s := range sections {
		switch s.Status {
		case healthCritical:
			return healthCritical
		case healthWarning:
			verdict = healthWarning
		}
	}

	return verdict
}

// olderThan tells whether t is set and more than d before now.
func olderThan(t, now time.Time, d time.Duration) bool {
	return !t.IsZero() && now.Sub(t) > d
}

// milli is t in unix ms, 0 when unset.
func milli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}

	return t.UnixMilli()
}
