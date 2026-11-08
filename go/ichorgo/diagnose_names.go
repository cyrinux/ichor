package ichorgo

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// The anonymized report also hides the Kubernetes names: a namespace can name a customer. These
// placeholders stand next to privacyMask rather than in it: its learnNamespaces and learnNames
// only remember how an already masked name maps back, and teaching the shared screenshot mask new
// terms would change what every other screen shows.
//
// Only names the report's data states can be learned, plus those the kubelet's log lines give in a
// structured form: a name somewhere else in free text goes as it is.

const (
	placeholderNS  = "ns"
	placeholderPod = "pod"
	placeholderCtr = "ctr"
	placeholderApp = "app"
	placeholderObj = "obj"

	minMaskedName = 3
	systemNS      = "kube-system"
)

var (
	// keptNamespaces identify nothing.
	keptNamespaces = []string{systemNS, "default", "kube-public", "flux-system", "argocd"}
	// Kubernetes names these static pods itself, after the node (which the mask replaces), and
	// the report is no use without telling them apart.
	controlPlanePods = []string{"kube-apiserver-", "kube-controller-manager-", "kube-scheduler-"}

	structuredLogName = regexp.MustCompile(`\b(pod|podName|namespace|podNamespace|containerName)="([^"]+)"`)
)

// diagnosisNames maps the Kubernetes names of one report to placeholders and back.
type diagnosisNames struct {
	reals  map[string]string // real -> fake
	counts map[string]int
	avoid  string   // the report: a placeholder never equals something in it
	skip   []string // never replaced: too common, or the report's own words (a service id)
	keep   []string // stay as they are, and a shorter name must not match inside them
}

// newDiagnosisNames learns the names in data. report is the text about to be masked.
func newDiagnosisNames(data diagnosisData, report string) *diagnosisNames {
	n := &diagnosisNames{
		reals: map[string]string{}, counts: map[string]int{}, avoid: report,
		skip: slices.Concat(genericNames, keptNamespaces, []string{"etcd", kubeletService}),
		keep: slices.Clone(keptNamespaces),
	}

	for _, node := range data.Nodes {
		for _, s := range node.Services {
			n.skip = append(n.skip, s.ID)
		}
	}

	for _, node := range data.Nodes {
		n.learnNode(node)
	}

	for _, ev := range data.Events {
		n.learnLogLine(ev.Message)
	}

	n.learnGitOps(data.GitOps)
	n.learnDataServices(data.DataServices)
	n.learnKube(data.Kube)
	n.sweep(kubeSectionsIn(report, data))

	return n
}

func (n *diagnosisNames) learnNode(node nodeDiagnosis) {
	for _, p := range node.StaticPods {
		ns, name := splitQualified(p.Name)
		n.learnPod(ns, name)
		n.learnLogLine(p.Waiting)
	}

	for _, idle := range node.IdlePods {
		n.learnIdlePod(idle)
	}

	for _, tail := range node.Logs {
		for _, line := range tail.Lines {
			n.learnLogLine(line)
		}
	}
}

// learnIdlePod reads a summarizePods row: "namespace/pod (container exited, other running)".
func (n *diagnosisNames) learnIdlePod(row string) {
	key, states, ok := strings.Cut(row, " (")
	if !ok {
		return // "and 4 more"
	}

	ns, pod := splitQualified(key)
	n.learnPod(ns, pod)

	for state := range strings.SplitSeq(strings.TrimSuffix(states, ")"), ", ") {
		container, _, _ := strings.Cut(state, " ")
		n.learnContainer(ns, container)
	}
}

// learnLogLine learns from what the kubelet logs as pod="namespace/name" containerName="app".
func (n *diagnosisNames) learnLogLine(line string) {
	ns := ""

	for _, m := range structuredLogName.FindAllStringSubmatch(line, -1) {
		switch value := m[2]; m[1] {
		case "namespace", "podNamespace":
			ns = value
			n.learn(value, placeholderNS)
		case "containerName":
			n.learnContainer(ns, value)
		default:
			podNS, pod := splitQualified(value)
			if podNS != "" {
				ns = podNS
			}

			n.learnPod(ns, pod)
		}
	}
}

func (n *diagnosisNames) learnGitOps(g *gitopsState) {
	if g == nil {
		return
	}

	if g.Argo != nil {
		for _, app := range g.Argo.Apps {
			n.learnQualified(app.Namespace, app.Name, placeholderApp)
			n.learn(app.Destination.Namespace, placeholderNS)
			n.learnPods(app.UnhealthyPods)

			if app.Operation != nil {
				for _, f := range app.Operation.Failed {
					n.learnQualified(f.Namespace, f.Name, placeholderObj)
				}
			}
		}
	}

	if g.Flux != nil {
		for _, app := range g.Flux.Apps {
			n.learnQualified(app.Namespace, app.Name, placeholderApp)
			n.learn(app.TargetNamespace, placeholderNS)
			n.learnPods(app.UnhealthyPods)
		}
	}
}

// learnDataServices reads the keys of the report's items: "db/pg", "backup db/nightly",
// "pvc-1234", "backup target nas". "node x" is a host, which the mask handles.
func (n *diagnosisNames) learnDataServices(s *dataServicesState) {
	if s == nil {
		return
	}

	for _, sec := range slices.Concat(dataServiceSections(s.dataServices), pluginReportSections(s.Plugins)) {
		for _, issue := range sec.issues {
			fields := strings.Fields(issue.key)
			if len(fields) == 0 || fields[0] == "node" {
				continue
			}

			ns, name := splitQualified(fields[len(fields)-1])
			n.learnQualified(ns, name, placeholderObj)
		}
	}
}

func (n *diagnosisNames) learnPods(pods []kubePod) {
	for _, p := range pods {
		n.learnPod(p.Namespace, p.Name)
	}
}

func (n *diagnosisNames) learnPod(ns, name string) {
	n.learn(ns, placeholderNS)

	if ns == systemNS && slices.ContainsFunc(controlPlanePods, func(p string) bool { return strings.HasPrefix(name, p) }) {
		n.keep = append(n.keep, name)

		return
	}

	n.learn(name, placeholderPod)
}

// learnContainer hides a container's name, usually its app's, unless its namespace says it is
// part of the cluster: "coredns exited" is what the model needs to read about a kube-system pod.
func (n *diagnosisNames) learnContainer(ns, name string) {
	if !slices.Contains(keptNamespaces, ns) {
		n.learn(name, placeholderCtr)
	}
}

func (n *diagnosisNames) learnQualified(ns, name, kind string) {
	n.learn(ns, placeholderNS)
	n.learn(name, kind)
}

func (n *diagnosisNames) learn(name, kind string) {
	if _, known := n.reals[name]; known || len(name) < minMaskedName || strings.ContainsAny(name, " \t") ||
		slices.Contains(n.skip, name) || slices.Contains(n.keep, name) {
		return
	}

	var fake string

	for fake == "" || strings.Contains(n.avoid, fake) {
		n.counts[kind]++
		fake = fmt.Sprintf("%s-%d", kind, n.counts[kind])
	}

	n.reals[name] = fake
}

// clone is a copy that learns on its own; nil stays nil.
func (n *diagnosisNames) clone() *diagnosisNames {
	if n == nil {
		return nil
	}

	return &diagnosisNames{
		reals: maps.Clone(n.reals), counts: maps.Clone(n.counts), avoid: n.avoid,
		skip: slices.Clone(n.skip), keep: slices.Clone(n.keep),
	}
}

// mask replaces the learned names in text.
func (n *diagnosisNames) mask(text string) string {
	terms := make([]maskTerm, 0, len(n.reals)+len(n.keep))
	for real, fake := range n.reals {
		terms = append(terms, maskTerm{match: real, repl: fake, exactCase: true})
	}

	for _, name := range n.keep {
		terms = append(terms, maskTerm{match: name, repl: name, exactCase: true})
	}

	sortTerms(terms)

	return replaceTerms(text, terms)
}

// reveal puts the real names back in text written about the placeholders.
func (n *diagnosisNames) reveal(text string) string {
	terms := make([]maskTerm, 0, len(n.reals))
	for real, fake := range n.reals {
		terms = append(terms, maskTerm{match: fake, repl: real, exactCase: true})
	}

	sortTerms(terms)

	return replaceTerms(text, terms)
}

// splitQualified splits "namespace/name"; a name alone has no namespace.
func splitQualified(key string) (ns, name string) {
	if before, after, ok := strings.Cut(key, "/"); ok {
		return before, after
	}

	return "", key
}
