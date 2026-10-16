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
	checkPolicies      = "policies"
	checkCapacity      = "capacity"
	checkNodes         = "nodes"
	checkLoadBalancers = "loadbalancers"
	checkTerminating   = "terminating"
	checkCertificates  = "certificates"
	checkSecrets       = "secrets"
	checkHelm          = "helm"
	checkMonitoring    = "monitoring"
	checkCrossplane    = "crossplane"
	checkTailscale     = "tailscale"
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
	// checkupProgressOp is the ReadProgress op of a checkup; checkupDemoStep paces its demo.
	checkupProgressOp = "checkup"
	checkupDemoStep   = 100 * time.Millisecond
	// How long something may stay in a transient state before it is a finding.
	pendingGrace     = 5 * time.Minute
	startingGrace    = 10 * time.Minute
	terminatingGrace = 10 * time.Minute
)

// The steps of a checkup's progress: the shared reads, then the section readers (one reader
// fills the capacity and nodes sections, under "capacity").
const (
	checkupStepAPI     = "api"
	checkupStepPods    = "pods"
	checkupStepNodes   = "nodes"
	checkupStepPVCs    = "pvcs"
	checkupStepVersion = "version"
)

var checkupSteps = []string{
	checkupStepAPI, checkupStepPods, checkupStepNodes, checkupStepPVCs, checkupStepVersion,
	checkWorkloads, checkEvents, checkStorage, checkUpgrade, checkWebhooks, checkPolicies,
	checkCapacity, checkLoadBalancers, checkTerminating, checkCertificates, checkSecrets, checkHelm, checkMonitoring,
	checkCrossplane, checkTailscale,
}

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
// secrets that do not sync, sealed secrets that do not unseal, Helm releases that failed,
// the scrape targets down and rules failing in the first Prometheus discovered, and
// Crossplane resources that cannot sync, and a Tailscale operator, Connector or ProxyGroup
// that is not ready, as JSON (see checkupReport).
// It only reads. kubeServer: see KubePods.
func KubeCheckup(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		playDemoCheckupProgress(contextName)

		return toJSON(demoCheckup(time.Now()))
	}

	progress := startReadProgress(checkupProgressOp, contextName, checkupSteps...)
	defer progress.finish()

	ctx, cancel := context.WithTimeout(withReadProgress(context.Background(), progress), checkupTimeout)
	defer cancel()

	report, err := withKubeContext(ctx, kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) (checkupReport, error) {
		return readCheckup(ctx, k, time.Now())
	})
	if err != nil {
		return "", err
	}

	return toJSON(report)
}

// playDemoCheckupProgress walks a few steps so the demo shows the progress the apps draw.
func playDemoCheckupProgress(contextName string) {
	steps := []string{checkupStepAPI, checkupStepPods, checkStorage}
	progress := startReadProgress(checkupProgressOp, contextName, steps...)
	defer progress.finish()

	for _, step := range steps {
		progress.begin(step)
		time.Sleep(checkupDemoStep)
		progress.done(step)
	}
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
	progress := readProgressFrom(ctx)

	endAPI := progress.track(checkupStepAPI)

	groups, err := readAPIGroups(ctx, k)
	endAPI(err != nil)

	if err != nil {
		return checkupReport{}, err
	}

	in := checkupInput{now: now, groups: groups}

	var (
		wg      sync.WaitGroup
		version string
	)

	wg.Go(func() {
		end := progress.track(checkupStepPods)
		in.pods, in.podsErr = listCheckPods(ctx, k)
		end(in.podsErr != nil)
	})
	wg.Go(func() {
		end := progress.track(checkupStepNodes)
		in.nodes, in.nodesErr = listObjects[checkNodeObject](ctx, k, "/api/v1/nodes")
		end(in.nodesErr != nil)
	})
	wg.Go(func() {
		end := progress.track(checkupStepPVCs)
		in.pvcs, in.pvcsErr = listObjects[pvcObject](ctx, k, "/api/v1/persistentvolumeclaims")
		end(in.pvcsErr != nil)
	})
	wg.Go(func() {
		end := progress.track(checkupStepVersion)
		version = readAPIVersion(ctx, k)
		end(false)
	})
	wg.Wait()

	report := checkupReport{KubeVersion: version}
	sections := make([]checkupSection, 16)

	// section runs one section reader as its progress step; the step fails when a section it
	// fills could not be read at all.
	section := func(step string, read func(), filled ...int) {
		wg.Go(func() {
			end := progress.track(step)
			read()
			end(slices.ContainsFunc(filled, func(i int) bool { return sections[i].Status == checkUnknown }))
		})
	}

	section(checkWorkloads, func() { sections[0] = checkupWorkloads(ctx, k, in) }, 0)
	section(checkEvents, func() { sections[1] = checkupEvents(ctx, k, in) }, 1)
	section(checkStorage, func() { sections[2], report.Volumes = checkupStorage(ctx, k, in) }, 2)
	section(checkUpgrade, func() { sections[3] = checkupUpgrade(ctx, k, version) }, 3)
	section(checkWebhooks, func() { sections[4] = checkupWebhooks(ctx, k) }, 4)
	section(checkPolicies, func() { sections[5] = checkupPolicies(ctx, k, in) }, 5)
	section(checkCapacity, func() { sections[6], sections[7], report.Nodes = checkupCapacity(ctx, k, in, version) }, 6, 7)
	section(checkLoadBalancers, func() { sections[8] = checkupLoadBalancers(ctx, k, in) }, 8)
	section(checkTerminating, func() { sections[9] = checkupTerminating(ctx, k, in) }, 9)
	section(checkCertificates, func() { sections[10] = checkupCSRs(ctx, k, in) }, 10)
	section(checkSecrets, func() { sections[11] = checkupSecrets(ctx, k, in) }, 11)
	section(checkHelm, func() { sections[12], report.Releases = checkupHelm(ctx, k, in) }, 12)
	section(checkMonitoring, func() { sections[13] = checkupMonitoring(ctx, k, in) }, 13)
	section(checkCrossplane, func() { sections[14] = checkupCrossplane(ctx, k, in) }, 14)
	section(checkTailscale, func() { sections[15] = checkupTailscale(ctx, k, in) }, 15)
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

// checkupPart is what one reader contributes to a section several fill (see checkupSecrets):
// served says the cluster runs what it checks, and the rest is what newSection needs. A part
// that is not served contributes nothing, so a section is absent only when none of its
// readers is.
type checkupPart struct {
	served   bool
	checked  int
	findings []checkupFinding
	errs     []error
}

// sectionFromParts closes a section from the readers that fill it, absent when none of them
// is served.
func sectionFromParts(id string, parts ...checkupPart) checkupSection {
	served := false
	checked := 0
	findings := []checkupFinding{}
	errs := []error{}

	for _, p := range parts {
		if !p.served {
			continue
		}

		served = true
		checked += p.checked
		findings = append(findings, p.findings...)
		errs = append(errs, p.errs...)
	}

	if !served {
		return absentSection(id)
	}

	return newSection(id, checked, findings, errs...)
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
