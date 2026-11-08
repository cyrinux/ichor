package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Sanitize, like k9s's `z` on its pods view: delete the pods of a namespace whose status
// says they are done or stuck, after a storm left hundreds of them. The app first asks
// KubeSanitizePreview which pods would go, then passes back the ones the user kept checked,
// so the sweep deletes exactly what the user saw and nothing that turned dead since.

// sanitizeStatuses are the statuses k9s's sanitize deletes (toastPhases in its dao/pod.go).
var sanitizeStatuses = map[string]bool{
	"Completed": true, "Pending": true, "CrashLoopBackOff": true, "Error": true,
	"ImagePullBackOff": true, "ContainerStatusUnknown": true, "Evicted": true, "OOMKilled": true,
}

// sanitizePod is a pod the sweep would delete; Owner* is who recreates it ("" when none).
type sanitizePod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	OwnerKind string `json:"ownerKind"`
	OwnerName string `json:"ownerName"`
	AgeSec    int64  `json:"ageSec"`
}

// sanitizePreview: Truncated means the namespace has more pods than one page, unread.
type sanitizePreview struct {
	Pods      []sanitizePod `json:"pods"`
	Truncated bool          `json:"truncated"`
}

type sanitizeFailure struct {
	Name  string `json:"name"` // "namespace/name"
	Error string `json:"error"`
}

// sanitizeResult: Stopped means the sweep ended (Cancel, or its time ran out) before every
// pod was tried; Deleted and Failed are then the pods tried so far.
type sanitizeResult struct {
	Deleted int               `json:"deleted"`
	Failed  []sanitizeFailure `json:"failed"`
	Stopped bool              `json:"stopped"`
}

// sanitizeNow is the clock pod ages are read with.
var sanitizeNow = time.Now

// KubeSanitizePreview lists the pods of namespace ("" for every namespace, one page of
// kubePageLimit pods) that k9s's sanitize would delete: Completed, Pending,
// CrashLoopBackOff, Error, ImagePullBackOff, ContainerStatusUnknown, Evicted, OOMKilled.
// {"pods":[{namespace,name,status,ownerKind,ownerName,ageSec}],"truncated"}. Read-only
// (list on pods). kubeServer: see KubePods.
func KubeSanitizePreview(configYAML, contextName, kubeServer, namespace string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))
	if err := validateNamespace(namespace); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() sanitizePreview { return demoSanitizePreview(namespace) },
		func(ctx context.Context, k *kubeClient) (sanitizePreview, error) {
			return readSanitizePreview(ctx, k, namespace)
		})
}

func readSanitizePreview(ctx context.Context, k *kubeClient, namespace string) (sanitizePreview, error) {
	page, err := k.getPage(ctx, scopedPath("/api/v1", namespace, "pods"), pageQuery{limit: kubePageLimit})
	if err != nil {
		return sanitizePreview{}, err
	}

	objs, err := decodeItems[podObject](page)
	if err != nil {
		return sanitizePreview{}, err
	}

	now := sanitizeNow()
	out := sanitizePreview{Pods: []sanitizePod{}, Truncated: page.continueToken != ""}

	for _, obj := range objs {
		if p, ok := sanitizeCandidate(obj, now); ok {
			out.Pods = append(out.Pods, p)
		}
	}

	privacy.learnNamespaces(namespacesOf(out.Pods, func(p sanitizePod) string { return p.Namespace }))
	privacy.learnNames(namespacesOf(out.Pods, func(p sanitizePod) string { return p.Name }))

	return out, nil
}

// sanitizeCandidate is obj as a preview row when its status is one sanitize deletes.
func sanitizeCandidate(obj podObject, now time.Time) (sanitizePod, bool) {
	status := podStatus(obj)
	if !sanitizeStatuses[status] {
		return sanitizePod{}, false
	}

	p := sanitizePod{Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name, Status: status}

	if owners := obj.Metadata.OwnerReferences; len(owners) > 0 {
		p.OwnerKind, p.OwnerName = owners[0].Kind, owners[0].Name
	}

	if created := obj.Metadata.CreationTimestamp; !created.IsZero() && now.After(created) {
		p.AgeSec = int64(now.Sub(created) / time.Second)
	}

	return p, true
}

// KubeSanitize deletes the pods named in names, newline-separated "namespace/name" lines
// from KubeSanitizePreview, each like `kubectl delete pod` with its own grace period. With
// a namespace, every pod must be in it; a malformed or out-of-scope line refuses the whole
// sweep before anything is deleted. A pod that cannot be deleted does not stop the others:
// {"deleted":n,"failed":[{name,error}],"stopped":false}. One audit entry for the sweep: ok
// when every pod went, partial ("k of n pods not deleted") when some did not, failed when
// the sweep was refused or could not run; a partial sweep still returns its result with a
// nil error. StartKubeSanitize is the same sweep in the background, as the apps run it.
// Refused in the demo. Needs delete on pods. kubeServer: see KubePods.
func KubeSanitize(configYAML, contextName, kubeServer, namespace, names string) (out string, err error) {
	defer maskResult(&out, &err)
	defer clearPartial(&err)

	contextName = unmaskContext(configYAML, contextName)

	res, err := sweepPods(context.Background(), kubeSweep{configYAML, contextName, kubeServer, namespace, names}, func(sanitizeProgress) {})
	if _, partial := err.(partialOutcome); err != nil && !partial {
		return "", err
	}

	out, encErr := toJSON(res)
	if encErr != nil {
		return "", encErr
	}

	return out, err
}

// podRef is a pod's namespace and name.
type podRef struct{ namespace, name string }

// sanitizeTargets parses the "namespace/name" lines, unmasked, without duplicates; at most
// one preview's worth of pods, all in namespace unless it is "".
func sanitizeTargets(namespace, names string) ([]podRef, error) {
	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}

	var pods []podRef

	seen := map[podRef]bool{}

	for line := range strings.SplitSeq(names, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		ns, name, ok := strings.Cut(line, "/")
		if !ok {
			return nil, fmt.Errorf("%q is not namespace/name", line)
		}

		ref := podRef{privacy.revealNamespace(strings.TrimSpace(ns)), privacy.reveal(strings.TrimSpace(name))}
		if err := validateKubeName("pod", ref.namespace, ref.name); err != nil {
			return nil, err
		}

		if namespace != "" && ref.namespace != namespace {
			return nil, fmt.Errorf("pod %s/%s is not in namespace %s", ref.namespace, ref.name, namespace)
		}

		if !seen[ref] {
			seen[ref] = true
			pods = append(pods, ref)
		}
	}

	if len(pods) == 0 {
		return nil, errors.New("no pods given")
	}

	if len(pods) > kubePageLimit {
		return nil, fmt.Errorf("%d pods given, at most %d", len(pods), kubePageLimit)
	}

	return pods, nil
}

// deletePods deletes each pod in turn, carrying on past the ones that fail, and reports
// after every pod. It stops between two pods once ctx is done, never in the middle of one.
func deletePods(ctx context.Context, k *kubeClient, pods []podRef, emit func(sanitizeProgress)) sanitizeResult {
	res := sanitizeResult{Failed: []sanitizeFailure{}}

	for i, p := range pods {
		if ctx.Err() != nil {
			res.Stopped = true

			break
		}

		if err := deletePod(ctx, k, p); err != nil {
			res.Failed = append(res.Failed, sanitizeFailure{Name: p.namespace + "/" + p.name, Error: err.Error()})
		} else {
			res.Deleted++
		}

		emit(sanitizeProgress{Done: i + 1, Total: len(pods), Name: p.namespace + "/" + p.name, Failed: len(res.Failed)})
	}

	return res
}

// deletePod deletes p within its own call budget: a Stop arriving meanwhile lets it finish.
func deletePod(ctx context.Context, k *kubeClient, p podRef) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callTimeout)
	defer cancel()

	err := k.do(ctx, http.MethodDelete, podPath(p.namespace, p.name), "", nil, nil)
	if err == nil {
		return nil
	}

	if kubeCode(err) == http.StatusNotFound {
		return errors.New("the pod no longer exists")
	}

	return kubeMutationError(err)
}

// demoSanitizePreview is what the demo inventory's sanitize would delete in namespace.
func demoSanitizePreview(namespace string) sanitizePreview {
	pod := func(ns, name, status, ownerKind, ownerName string, age time.Duration) sanitizePod {
		return sanitizePod{Namespace: ns, Name: name, Status: status, OwnerKind: ownerKind, OwnerName: ownerName, AgeSec: int64(age / time.Second)}
	}

	pods := []sanitizePod{
		pod("demo", "report-28812345-abcde", "Completed", "Job", "report-28812345", 3*time.Hour),
		pod("demo", "reports-5b7c9-xkq2p", "ImagePullBackOff", "ReplicaSet", "reports-5b7c9", 2*time.Hour),
		pod("demo", "worker-6f4b8-uvwxy", "CrashLoopBackOff", "ReplicaSet", "worker-6f4b8", 72*time.Hour),
		pod("demo", "worker-6f4b8-zz9k1", "Evicted", "ReplicaSet", "worker-6f4b8", 26*time.Hour),
		pod("demo", "hello-ichor-7d9c5-qrstu", "Pending", "ReplicaSet", "hello-ichor-7d9c5", 45*time.Second),
	}

	return sanitizePreview{Pods: inNamespace(pods, namespace, func(p sanitizePod) string { return p.Namespace })}
}
