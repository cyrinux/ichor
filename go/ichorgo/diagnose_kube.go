package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
)

// The diagnosis of a cluster added from a kubeconfig: what the Kubernetes API knows, since there
// is no Talos API to read. A Talos cluster gets its two sections about workloads and pods too.
// Everything is read, nothing is changed, and no read pages a large cluster to its end.

const (
	// diagnosisKubeTimeout bounds every read of the report together.
	diagnosisKubeTimeout = 20 * time.Second

	diagnosisKubeMaxWorkloads = 30
	diagnosisKubeMaxPods      = 30
	diagnosisKubeMaxEvents    = 30
	diagnosisKubeMaxFindings  = 40
	// diagnosisKubePodPages bounds the pods looked at, in pages of kubePageLimit.
	diagnosisKubePodPages = 4
)

// kubeDiagnosisData is what the report says. A *Note says why a section is left out or not
// whole, so that an empty list always means nothing was found.
type kubeDiagnosisData struct {
	At      time.Time
	Version string

	Nodes     []kubeNodeInfo
	NodesNote string

	Workloads     []kubeTroubledWorkload
	WorkloadsMore int
	WorkloadsNote string

	Pods     []kubeTroubledPod
	PodsMore int
	PodsNote string

	Events     []kubeEvent
	EventsNote string

	Findings     []checkupFinding // warnings and criticals, without the two lists below
	FindingsMore int
	Plugins      []checkupFinding // the plugins' critical findings
	Helm         []checkupRelease // the releases that failed
	CheckupNotes []string

	GitOps *gitopsState

	// err is set when the API server answered nothing at all: there is no report to show.
	err error
}

// kubeTroubledWorkload is a workload that wants more pods than are ready.
type kubeTroubledWorkload struct {
	idleWorkload

	// Event is its newest Warning event, or that of one of its pods.
	Event string
}

// kubeTroubledPod is a pod that does not run, with why.
type kubeTroubledPod struct {
	kubePod

	Reason string
}

// collectKubeDiagnosis reads the report's data. A section that cannot be read carries a note
// and hides none of the others. locale words the plugins' findings ("" is English).
func collectKubeDiagnosis(ctx context.Context, k *kubeClient, locale string) kubeDiagnosisData {
	ctx, cancel := context.WithTimeout(ctx, diagnosisKubeTimeout)
	defer cancel()

	d := kubeDiagnosisData{At: time.Now()}

	var (
		wg                         sync.WaitGroup
		nodesErr, eventsErr        error
		podsErr, workloadsErr      error
		events                     []kubeEvent
		eventsPartial, podsPartial bool
		pods                       []kubePod
		workloads                  []idleWorkload
		workloadsDenied            []string
	)

	wg.Go(func() {
		overview, err := listKubeNodes(ctx, k)
		d.Version, d.Nodes, nodesErr = overview.ServerVersion, overview.Nodes, err

		if overview.Forbidden {
			nodesErr = errNotAllowed
		}
	})
	wg.Go(func() { events, eventsPartial, eventsErr = readWarningEvents(ctx, k) })
	wg.Go(func() { pods, podsPartial, podsErr = readTroubledPods(ctx, k) })
	wg.Go(func() { workloads, workloadsDenied, workloadsErr = readTroubledWorkloads(ctx, k) })
	wg.Go(func() {
		report, err := readCheckup(ctx, k, time.Now(), locale)
		if err != nil {
			d.CheckupNotes = []string{sectionNote("the checkup", err)}

			return
		}

		d.applyCheckup(report)
	})
	wg.Go(func() {
		state, err := readGitOps(ctx, k)

		switch {
		case err != nil:
			d.GitOps = &gitopsState{Note: sectionNote("Argo CD and Flux", err)}
		case state.Argo != nil || state.Flux != nil || state.ArgoError != "" || state.FluxError != "":
			d.GitOps = &state
		}
	})
	wg.Wait()

	d.setNotes(nodesErr, eventsErr, podsErr, workloadsErr)

	if eventsPartial {
		d.EventsNote = "not complete: the cluster has more warning events than were read"
	}

	if podsPartial {
		d.PodsNote = "not complete: only the first pods of the cluster were looked at"
	}

	if len(workloadsDenied) > 0 {
		d.WorkloadsNote = "not complete: the credentials may not list " + strings.Join(workloadsDenied, ", ")
	}

	d.setPods(pods, events)
	d.setWorkloads(workloads, events)
	d.setEvents(events)

	if nothingAnswered(nodesErr, eventsErr, podsErr, workloadsErr) {
		d.err = errors.Join(nodesErr, eventsErr, podsErr, workloadsErr)
	}

	return d
}

// errNotAllowed stands for a list the API server refused where the reader only says so.
var errNotAllowed = errors.New("forbidden")

func (d *kubeDiagnosisData) setNotes(nodes, events, pods, workloads error) {
	if nodes != nil {
		d.NodesNote = sectionNote("nodes", nodes)
	}

	if events != nil {
		d.EventsNote = sectionNote("events", events)
	}

	if pods != nil {
		d.PodsNote = sectionNote("pods", pods)
	}

	if workloads != nil {
		d.WorkloadsNote = sectionNote("Deployments, StatefulSets and DaemonSets", workloads)
	}
}

// sectionNote is the one line that replaces a section that could not be read.
func sectionNote(what string, err error) string {
	if errors.Is(err, errNotAllowed) || isForbidden(err) {
		return "left out: the credentials may not list " + what
	}

	return "could not be read: " + clipUTF8(kubeError(err).Error(), diagnosisMaxLineLen)
}

// nothingAnswered tells an API server out of reach from credentials with little access: a
// refusal is an answer, and leaves a report saying what was refused.
func nothingAnswered(errs ...error) bool {
	return !slices.ContainsFunc(errs, func(err error) bool {
		return err == nil || errors.Is(err, errNotAllowed) || isForbidden(err)
	})
}

// collectKubeTrouble reads the sections a Talos report gains; nil without os:admin, as for
// the GitOps section. It leaves a note rather than failing the report.
func collectKubeTrouble(ctx context.Context, target kubeTarget, roles []string) *kubeDiagnosisData {
	if !slices.Contains(roles, adminRole) {
		return nil
	}

	if isDemoContext(target.config, target.context) {
		demo := demoDiagnosis(time.Now(), "")

		return &kubeDiagnosisData{Workloads: demo.Workloads, Pods: demo.Pods}
	}

	ctx, cancel := context.WithTimeout(ctx, diagnosisKubeTimeout)
	defer cancel()

	d, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (kubeDiagnosisData, error) {
		var (
			d                     kubeDiagnosisData
			wg                    sync.WaitGroup
			pods                  []kubePod
			workloads             []idleWorkload
			denied                []string
			partial               bool
			podsErr, workloadsErr error
		)

		wg.Go(func() { pods, partial, podsErr = readTroubledPods(ctx, k) })
		wg.Go(func() { workloads, denied, workloadsErr = readTroubledWorkloads(ctx, k) })
		wg.Wait()

		d.setNotes(nil, nil, podsErr, workloadsErr)

		if partial {
			d.PodsNote = "not complete: only the first pods of the cluster were looked at"
		}

		if len(denied) > 0 {
			d.WorkloadsNote = "not complete: the credentials may not list " + strings.Join(denied, ", ")
		}

		d.setPods(pods, nil)
		d.setWorkloads(workloads, nil)

		return d, nil
	})
	if err != nil {
		note := sectionNote("workloads and pods", err)

		return &kubeDiagnosisData{WorkloadsNote: note, PodsNote: note}
	}

	return &d
}

// readWarningEvents reads one page of the cluster's Warning events, newest first, and tells
// when the cluster has more: the API has no order to ask for, and paging is what must not happen.
func readWarningEvents(ctx context.Context, k *kubeClient) ([]kubeEvent, bool, error) {
	page, err := k.getPage(ctx, "/api/v1/events", pageQuery{fieldSelector: "type=Warning"})
	if err != nil {
		return nil, false, err
	}

	objs, err := decodeItems[eventObject](page)
	if err != nil {
		return nil, false, err
	}

	events := make([]kubeEvent, 0, len(objs))
	for _, obj := range objs {
		events = append(events, mapEvent(obj))
	}

	slices.SortStableFunc(events, func(a, b kubeEvent) int { return cmp.Compare(b.Last, a.Last) })

	return events, page.continueToken != "", nil
}

// errEnoughPods stops reading pages.
var errEnoughPods = errors.New("enough pods")

// readTroubledPods looks through the first pages of the unfinished pods. Not with the selector
// status.phase!=Running: a crash-looping pod is in phase Running.
func readTroubledPods(ctx context.Context, k *kubeClient) (pods []kubePod, partial bool, err error) {
	pages := 0

	err = k.eachPage(ctx, "/api/v1/pods", pageQuery{fieldSelector: unfinishedPods}, func(page kubePage) error {
		mapped, err := mapPodPage(page)
		if err != nil {
			return err
		}

		for _, p := range mapped {
			if podInTrouble(p) {
				pods = append(pods, p)
			}
		}

		if pages++; pages == diagnosisKubePodPages && page.continueToken != "" {
			partial = true

			return errEnoughPods
		}

		return nil
	})
	if errors.Is(err, errEnoughPods) {
		err = nil
	}

	return pods, partial, err
}

// podInTrouble leaves out the pods being replaced, and those that run but are not ready: the
// workloads section counts them, and most are starting.
func podInTrouble(p kubePod) bool {
	return !p.Healthy && p.Status != "Running" && p.Status != "Terminating" && p.Status != "Completed"
}

// podTroubleRank puts what will not get better by itself first.
func podTroubleRank(status string) int {
	status = strings.TrimPrefix(status, "Init:")

	switch status {
	case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "InvalidImageName":
		return 0
	case "Pending", "ContainerCreating", "PodInitializing":
		return 2
	default:
		return 1
	}
}

// readTroubledWorkloads lists the workloads that want more pods than are ready, with their
// reason; the kinds the credentials may not list are named. A workload scaled to zero is stopped
// on purpose, not a finding.
func readTroubledWorkloads(ctx context.Context, k *kubeClient) ([]idleWorkload, []string, error) {
	found, errs := listWorkloadsWhere(ctx, k, workloadKinds, func(w kubeWorkload) bool {
		return w.Desired > 0 && w.Ready < w.Desired
	})

	if !slices.Contains(errs, nil) {
		return nil, nil, errors.Join(errs...)
	}

	var denied []string

	for i, err := range errs {
		if err != nil {
			denied = append(denied, workloadKinds[i].resource)
		}
	}

	addIdleReasons(ctx, k, found)

	return found, denied, nil
}

// setPods keeps the worst pods, each with the kubelet's words, else its newest Warning event.
func (d *kubeDiagnosisData) setPods(pods []kubePod, events []kubeEvent) {
	sorted := slices.Clone(pods)
	slices.SortStableFunc(sorted, func(a, b kubePod) int {
		return cmp.Or(
			cmp.Compare(podTroubleRank(a.Status), podTroubleRank(b.Status)),
			strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name),
		)
	})

	if len(sorted) > diagnosisKubeMaxPods {
		d.PodsMore = len(sorted) - diagnosisKubeMaxPods
		sorted = sorted[:diagnosisKubeMaxPods]
	}

	d.Pods = make([]kubeTroubledPod, 0, len(sorted))

	for _, p := range sorted {
		reason := p.Waiting

		if reason == "" {
			at := slices.IndexFunc(events, func(e kubeEvent) bool {
				return e.Kind == "Pod" && e.Namespace == p.Namespace && e.Name == p.Name
			})
			if at >= 0 {
				reason = events[at].Message
			}
		}

		d.Pods = append(d.Pods, kubeTroubledPod{kubePod: p, Reason: reason})
	}
}

// setWorkloads sorts and caps them, each with its newest event (events are newest first).
func (d *kubeDiagnosisData) setWorkloads(workloads []idleWorkload, events []kubeEvent) {
	sorted := slices.Clone(workloads)
	slices.SortStableFunc(sorted, func(a, b idleWorkload) int {
		return cmp.Or(strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name), strings.Compare(a.Kind, b.Kind))
	})

	if len(sorted) > diagnosisKubeMaxWorkloads {
		d.WorkloadsMore = len(sorted) - diagnosisKubeMaxWorkloads
		sorted = sorted[:diagnosisKubeMaxWorkloads]
	}

	d.Workloads = make([]kubeTroubledWorkload, 0, len(sorted))

	for _, w := range sorted {
		tw := kubeTroubledWorkload{idleWorkload: w}

		at := slices.IndexFunc(events, func(e kubeEvent) bool { return eventOfWorkload(e, w) })
		if at >= 0 {
			tw.Event = events[at].Message
		}

		d.Workloads = append(d.Workloads, tw)
	}
}

// eventOfWorkload tells an event of the workload, or of what it owns by name (its ReplicaSets
// and pods), as readEvents does.
func eventOfWorkload(e kubeEvent, w idleWorkload) bool {
	if e.Namespace != w.Namespace {
		return false
	}

	return e.Kind == w.Kind && e.Name == w.Name || strings.HasPrefix(e.Name, w.Name+"-")
}

func (d *kubeDiagnosisData) setEvents(events []kubeEvent) {
	d.Events = slices.Clone(events)
	if len(d.Events) > diagnosisKubeMaxEvents {
		d.Events = d.Events[:diagnosisKubeMaxEvents]
	}
}

// applyCheckup sorts a checkup's findings into the report's lists, and says which of its
// sections could not be read.
func (d *kubeDiagnosisData) applyCheckup(report checkupReport) {
	for _, s := range report.Sections {
		if s.Error != "" {
			d.CheckupNotes = append(d.CheckupNotes, s.ID+" could not be read: "+clipUTF8(s.Error, diagnosisMaxLineLen))
		}

		for _, f := range s.Findings {
			switch {
			case f.Kind == findPluginFinding:
				if f.Severity == sevCritical {
					d.Plugins = append(d.Plugins, f)
				}
			case f.Kind == findHelmFailed:
			case f.Severity == sevCritical || f.Severity == sevWarning:
				d.Findings = append(d.Findings, f)
			}
		}
	}

	slices.SortStableFunc(d.Findings, func(a, b checkupFinding) int {
		return severityRank[a.Severity] - severityRank[b.Severity]
	})

	if len(d.Findings) > diagnosisKubeMaxFindings {
		d.FindingsMore = len(d.Findings) - diagnosisKubeMaxFindings
		d.Findings = d.Findings[:diagnosisKubeMaxFindings]
	}

	for _, r := range report.Releases {
		if r.Status == "failed" {
			d.Helm = append(d.Helm, r)
		}
	}
}

// kubeHostNames are the node names the data states. The mask replaces them like a Talos node's
// names, and the other names must not match inside them.
func kubeHostNames(d *kubeDiagnosisData) map[string]string {
	hosts := map[string]string{}

	add := func(name, role string) {
		if name != "" && hosts[name] == "" {
			hosts[name] = role
		}
	}

	for _, n := range d.Nodes {
		role := "worker"
		if slices.Contains(n.Roles, "control-plane") {
			role = "controlplane"
		}

		add(n.Name, role)
	}

	for _, p := range d.Pods {
		add(p.Node, "worker")
	}

	for _, e := range d.Events {
		if e.Kind == "Node" {
			add(e.Name, "worker")
		}
	}

	for _, f := range d.Findings {
		add(f.Node, "worker")

		if slices.Contains(nodeFindingKinds, f.Kind) {
			add(f.Name, "worker")
		}
	}

	return hosts
}

// nodeFindingKinds are the findings whose Name is a node.
var nodeFindingKinds = []string{
	findNodeRequestsHigh, findNodePodsFull, findNoRoomToDrain, findNodeNotReady, findNodePressure, findNodeCordoned, findNodeVersionSkew,
}

// collectKubeReport is CollectDiagnosis for a kubeconfig context.
func collectKubeReport(configYAML, contextName, kubeServer string, anonymize bool) (*Diagnosis, error) {
	d := &Diagnosis{kind: diagnosisKube}
	d.screenshotMode, d.screenshotResets = privacy.state()

	var (
		kube kubeDiagnosisData
		err  error
	)

	if isKubeDemoContext(configYAML, contextName) {
		kube = demoDiagnosis(time.Now(), "")
	} else {
		target := kubeTarget{configYAML, contextName, kubeServer}

		kube, err = withKubeContext(context.Background(), target, func(ctx context.Context, k *kubeClient) (kubeDiagnosisData, error) {
			out := collectKubeDiagnosis(ctx, k, "")

			return out, out.err
		})
		if err != nil {
			return nil, err
		}
	}

	d.report = renderKubeDiagnosis(kube)
	d.applyMask(configYAML, anonymize, diagnosisData{At: kube.At, Kube: &kube, KubeOnly: true})
	d.tools = &diagnosisToolTarget{kube: kubeTarget{configYAML, contextName, kubeServer}}

	return d, nil
}
