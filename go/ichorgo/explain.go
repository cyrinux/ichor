package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// "Explain with AI" from a sheet: a short report about one pod, one alert or one checkup finding,
// asked like the health check helper (one answer, no tools). It is a *Diagnosis, so masking,
// asking and revealing work as for the full report, and its Kubernetes names are hidden the way a
// kubeconfig cluster's report hides them.

const (
	explainPod     = "pod"
	explainAlert   = "alert"
	explainFinding = "finding"

	// explainTimeout bounds every read of the report together: the operator is waiting on a sheet.
	explainTimeout = 10 * time.Second
	// explainMaxReport keeps the report small enough for the model on the phone.
	explainMaxReport = 16 << 10
	explainLogLines  = 40
	explainEvents    = 15
)

// explainSubject is what the apps send: {namespace, name} for a pod, {source, fingerprint} for an
// alert (source as AlertmanagerAlerts takes it, a JSON text or the object itself), and for a
// finding {finding, text, fix}: the finding as KubeCheckup gave it, with the title and the fix the
// app shows for it, in its language.
type explainSubject struct {
	Namespace   string          `json:"namespace"`
	Name        string          `json:"name"`
	Source      json.RawMessage `json:"source"`
	Fingerprint string          `json:"fingerprint"`
	Finding     *checkupFinding `json:"finding"`
	Text        string          `json:"text"`
	Fix         string          `json:"fix"`
}

// explainData is what the report says. A *Note says why a part is missing, so that an empty list
// always means nothing was found.
type explainData struct {
	At   time.Time
	Kind string

	Pod     *explainPodState
	Alert   *amAlert
	Finding *explainSubject

	Events     []kubeEvent
	EventsNote string

	Log     []string
	LogFrom string // "container cart, previous run"
	LogNote string
}

// explainPodState is the pod as KubePod maps it, with each container's state.
type explainPodState struct {
	kubePod

	Phase      string
	ContainerStates []string // "cart: waiting CrashLoopBackOff (…), 23 restarts, last ended OOMKilled (exit 137)"
}

// CollectExplanation builds the report "Explain with AI" sends about one subject: kind is pod,
// alert or finding, subjectJSON says which one (see explainSubject). It only reads (get pods,
// get pods/log, list events, the Alertmanager the Alerts screen uses), within 10 seconds in all;
// a read that is refused leaves a line in the report instead. Nothing leaves the phone here, and
// masking works as in CollectDiagnosis.
func CollectExplanation(configYAML, contextName, kubeServer, kind, subjectJSON string, anonymize bool) (d *Diagnosis, err error) {
	defer maskErr(&err)

	subject, err := parseExplainSubject(kind, subjectJSON)
	if err != nil {
		return nil, err
	}

	contextName = unmaskContext(configYAML, contextName)

	d = &Diagnosis{kind: diagnosisExplain}
	d.screenshotMode, d.screenshotResets = privacy.state()

	var data explainData

	if isDemoContext(configYAML, contextName) {
		data, err = demoExplanation(kind, subject, time.Now())
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), explainTimeout)
		defer cancel()

		data, err = collectExplanation(ctx, kubeTarget{configYAML, contextName, kubeServer}, kind, subject)
	}

	if err != nil {
		return nil, err
	}

	d.report = renderExplanation(data)
	kube := data.kubeNames()
	d.applyMask(configYAML, anonymize, diagnosisData{At: data.At, Kube: &kube, KubeOnly: true})

	return d, nil
}

// parseExplainSubject reads subjectJSON for kind, with the names the screen showed (masked in
// screenshot mode) turned back into the real ones.
func parseExplainSubject(kind, subjectJSON string) (explainSubject, error) {
	var s explainSubject

	if err := json.Unmarshal([]byte(subjectJSON), &s); err != nil {
		return s, fmt.Errorf("explain subject: %w", err)
	}

	switch kind {
	case explainPod:
		s.Namespace, s.Name = privacy.revealNamespace(strings.TrimSpace(s.Namespace)), privacy.revealName(strings.TrimSpace(s.Name))

		return s, validateKubeName("pod", s.Namespace, s.Name)
	case explainAlert:
		if strings.TrimSpace(s.Fingerprint) == "" {
			return s, errors.New("explain subject: the alert has no fingerprint")
		}

		_, err := parsePromSource(s.sourceJSON())

		return s, err
	case explainFinding:
		if s.Finding == nil || s.Finding.Kind == "" {
			return s, errors.New("explain subject: no finding")
		}

		s.Finding.Namespace = privacy.revealNamespace(s.Finding.Namespace)
		s.Finding.Name = privacy.reveal(s.Finding.Name)
		s.Finding.Node = privacy.reveal(s.Finding.Node)

		return s, nil
	default:
		return s, fmt.Errorf("explain: unknown kind %q (pod, alert or finding)", kind)
	}
}

// sourceJSON is the Alertmanager source as parsePromSource reads it: the apps may send its JSON
// text as a string or the object itself.
func (s explainSubject) sourceJSON() string {
	var text string
	if bytes.HasPrefix(bytes.TrimSpace(s.Source), []byte(`"`)) && json.Unmarshal(s.Source, &text) == nil {
		return text
	}

	return string(s.Source)
}

// findingObject is the kind and name of the object a finding is about: its Ref when it has one,
// else its Name, which may be "Kind/name" ("" kind: the name and what it owns).
func findingObject(f *checkupFinding) (namespace, kind, name string) {
	if f.Ref != nil && f.Ref.Name != "" {
		return f.Ref.Namespace, f.Ref.Kind, f.Ref.Name
	}

	if k, n, ok := strings.Cut(f.Name, "/"); ok {
		return f.Namespace, k, n
	}

	return f.Namespace, "", f.Name
}

// kubeNames is the report's subject in the shape the kubeconfig report's mask learns from: the pod
// and its containers, the events, the finding, and the objects an alert's labels name.
func (d explainData) kubeNames() kubeDiagnosisData {
	k := kubeDiagnosisData{At: d.At, Events: d.Events}

	if d.Pod != nil {
		k.Pods = append(k.Pods, kubeTroubledPod{kubePod: d.Pod.kubePod})
	}

	if d.Alert != nil {
		labels := d.Alert.Labels
		ns := labels["namespace"]

		if pod := labels["pod"]; ns != "" && pod != "" {
			k.Pods = append(k.Pods, kubeTroubledPod{kubePod: kubePod{
				Namespace: ns, Name: pod, Node: labels["node"], ContainerNames: nonEmpty([]string{labels["container"]}),
			}})
		}

		for _, label := range explainWorkloadLabels {
			if name := labels[label]; name != "" {
				k.Findings = append(k.Findings, checkupFinding{Namespace: ns, Name: name})
			}
		}

		if ns != "" {
			k.Findings = append(k.Findings, checkupFinding{Namespace: ns})
		}
	}

	if d.Finding != nil {
		k.Findings = append(k.Findings, *d.Finding.Finding)
	}

	return k
}
