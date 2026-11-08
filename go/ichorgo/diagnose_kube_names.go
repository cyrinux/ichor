package ichorgo

import (
	"regexp"
	"slices"
	"strings"
)

// What hides the names of the Kubernetes sections in an anonymized report. Their structured
// fields are learned first. Their free text (an event, a checkup message, a kubelet's words)
// holds more: a registry, an image, a Secret or a quota. Those sections are then swept, on the
// text as rendered, since a line clipped in the middle of a name no longer matches it: whatever
// is shaped like a name and is not known to be harmless becomes a placeholder too.
//
// What stays is a single plain word in free text that no structured field states and nothing
// around it marks as a name. docs/ai.md says so.

const placeholderImg = "img"

var (
	// sweepToken is a run of text with no space: it may hold separators, a name usually does. The
	// ellipsis of a clipped line is part of it: to the mask's word boundaries it is a letter.
	sweepToken = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._:/@+=~%?&#-]*[A-Za-z0-9]…?`)
	// sweepQuoted is what follows an opening quote; a clipped line has no closing one.
	sweepQuoted = regexp.MustCompile("(?:^|[\\s(=:,\\[])\\\\?[\"'`]([^\\s\"'`\\\\]+)")
	// sweepNamed is the word after the name of something a message is about.
	sweepNamed = regexp.MustCompile(`(?i)\b(?:namespace|quota|secret|configmap|serviceaccount)s?:?\s+([a-z0-9][a-z0-9._-]*)`)

	sweepSeparators = "/:@=_?&#%+~"

	placeholderPart = regexp.MustCompile(`^(?:ns|pod|ctr|app|obj|img)-\d+$`)
	numberPart      = regexp.MustCompile(`^\d+(?:[.,]\d+)*(?:[a-zA-Z]{1,3})?$`)
	durationPart    = regexp.MustCompile(`^(?:\d+[hms])+$`)
	versionPart     = regexp.MustCompile(`^v?\d+(?:\.\d+)+(?:[-+.][a-z0-9]+)*$|^v\d+(?:(?:alpha|beta)\d+)?$`)
	datePart        = regexp.MustCompile(`^\d[\d-]*T?\d*$`)
	// A Kubernetes name is lowercase: "CrashLoopBackOff" and "MountVolume.SetUp" are Kubernetes' words.
	capitalizedPart = regexp.MustCompile(`^[A-Z][A-Za-z]*(?:\.[A-Z][A-Za-z]*)*$`)
	plainWord       = regexp.MustCompile(`^[A-Za-z]+$`)
	apiDomain       = regexp.MustCompile(`(?:^|[.])(?:k8s|kubernetes)\.io$`)

	// sweepWords are the name-shaped words of Kubernetes messages and of the report itself.
	sweepWords = []string{
		"back-off", "backoff", "read-only", "non-zero", "in-use", "on-demand", "secrets-like", "control-plane",
		"kube-apiserver", "kube-controller-manager", "kube-scheduler", "kube-proxy", "metrics-server", "cert-manager",
		"k8s", "x509", "sha256", "ipv4", "ipv6", "http2", "tcp", "udp", "http", "https", "dial", "io", "exit",
	}
	// sweepNotNames follow the words sweepNamed looks for without being a name.
	sweepNotNames = []string{
		"a", "already", "an", "and", "are", "as", "at", "be", "been", "by", "cannot", "could", "data", "did", "does", "exceeded",
		"failed", "for", "from", "has", "had", "have", "in", "is", "key", "keys", "may", "must", "name", "not", "of", "on", "or",
		"status", "that", "the", "to", "was", "were", "which", "will", "with", "without", "would",
	}
)

// learnKube learns the names the Kubernetes sections state.
func (n *diagnosisNames) learnKube(d *kubeDiagnosisData) {
	if d == nil {
		return
	}

	for host := range kubeHostNames(d) {
		n.keep = append(n.keep, host)
	}

	for _, w := range d.Workloads {
		n.learnQualified(w.Namespace, w.Name, placeholderObj)
		n.learnImages(w.images)
	}

	for _, p := range d.Pods {
		n.learnPod(p.Namespace, p.Name)
		n.learnImages(p.Images)

		for _, c := range p.ContainerNames {
			n.learnContainer(p.Namespace, c)
		}

		if _, owner, ok := strings.Cut(p.Owner, "/"); ok {
			n.learn(owner, placeholderObj)
		}
	}

	for _, e := range d.Events {
		n.learn(e.Namespace, placeholderNS)

		switch e.Kind {
		case "Pod":
			n.learnPod(e.Namespace, e.Name)
		case "Node":
		default:
			n.learn(e.Name, placeholderObj)
		}
	}

	for _, f := range slices.Concat(d.Findings, d.Plugins) {
		n.learnFinding(f)
	}

	for _, r := range d.Helm {
		n.learnQualified(r.Namespace, r.Name, placeholderApp)
	}

	n.learnGitOps(d.GitOps)
}

func (n *diagnosisNames) learnImages(images []string) {
	for _, image := range images {
		n.learn(image, placeholderImg)
	}
}

// learnFinding learns what a finding names. Its Name may be "Kind/name", its Extra a Secret
// store or an image, except for the kinds where it is one of Kubernetes' or the app's words.
func (n *diagnosisNames) learnFinding(f checkupFinding) {
	n.learn(f.Namespace, placeholderNS)

	if f.Name != "" {
		_, name := splitQualified(f.Name)
		n.learn(name, placeholderObj)
	}

	if f.Ref != nil {
		n.learnQualified(f.Ref.Namespace, f.Ref.Name, placeholderObj)
	}

	if slices.Contains(findingsWithPlainExtra, f.Kind) {
		return
	}

	for field := range strings.FieldsSeq(f.Extra) {
		if !n.sweepSafe(field, true) {
			n.learn(field, placeholderObj)
		}
	}
}

// findingsWithPlainExtra hold a resource, a condition, a version or a state in Extra.
var findingsWithPlainExtra = []string{
	findNodeRequestsHigh, findQuotaNearLimit, findNodePressure, findNodeVersionSkew, findCapiClusterProvisioning,
	findTailscaleConnectorNotReady, findTailscaleProxyGroupDown, findTailscaleOperatorDown, findDeprecatedAPI,
}

// sweep learns what is left in text once the known names are masked. What it finds may hold a
// placeholder ("vault.ns-2.example"): the name learned is the real one, the one in the report.
func (n *diagnosisNames) sweep(text string) {
	if text == "" {
		return
	}

	text = n.mask(text)

	var found []string

	for _, m := range sweepQuoted.FindAllStringSubmatch(text, -1) {
		if token := strings.TrimRight(m[1], ".,;:)]}"); !n.sweepSafe(token, true) {
			found = append(found, token)
		}
	}

	for _, m := range sweepNamed.FindAllStringSubmatch(text, -1) {
		token := strings.TrimRight(m[1], ".-_")
		if !slices.Contains(sweepNotNames, strings.ToLower(token)) && !n.sweepSafe(token, true) {
			found = append(found, token)
		}
	}

	for _, token := range sweepToken.FindAllString(text, -1) {
		if !n.sweepSafe(token, false) {
			found = append(found, token)
		}
	}

	// All revealed before any is learned: reveal knows only the placeholders of the names above.
	reals := make([]string, len(found))
	for i, token := range found {
		reals[i] = n.reveal(token)
	}

	for _, name := range reals {
		n.learn(name, placeholderObj)
	}
}

// sweepSafe tells a token that names nothing. strict is for one that is known to be a name of
// some kind (it was quoted, or follows "secret"): a plain word is then no proof of anything.
func (n *diagnosisNames) sweepSafe(token string, strict bool) bool {
	token = strings.TrimSuffix(token, "…")
	parts := strings.FieldsFunc(token, func(r rune) bool { return strings.ContainsRune(sweepSeparators, r) })
	strict = strict || len(parts) > 1

	if len(parts) == 0 {
		return true
	}

	for _, part := range parts {
		if !n.sweepPartSafe(part, strict) {
			return false
		}
	}

	return true
}

func (n *diagnosisNames) sweepPartSafe(part string, strict bool) bool {
	switch {
	case placeholderPart.MatchString(part), numberPart.MatchString(part), durationPart.MatchString(part),
		versionPart.MatchString(part), datePart.MatchString(part), capitalizedPart.MatchString(part),
		apiDomain.MatchString(part), slices.Contains(n.keep, part), slices.Contains(n.skip, part),
		slices.Contains(sweepWords, strings.ToLower(part)):
		return true
	}

	return !strict && plainWord.MatchString(part)
}

// kubeSectionsIn is the part of report the Kubernetes sections hold: the whole report of a
// kubeconfig cluster, else from their title up to the events. A title quoted in a log line above
// widens what is swept, and the last events title is the real one: neither narrows it.
func kubeSectionsIn(report string, data diagnosisData) string {
	switch {
	case data.Kube == nil:
		return ""
	case data.KubeOnly:
		return report
	}

	start := strings.Index(report, kubeWorkloadsTitle)
	if start < 0 {
		return ""
	}

	rest := report[start:]
	if end := strings.LastIndex(rest, talosEventsTitle); end >= 0 {
		rest = rest[:end]
	}

	return rest
}
