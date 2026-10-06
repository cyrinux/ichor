package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// The findings of the workloads section. Count is the pod's restarts, Reason what
// `kubectl get pods` shows or why the container last stopped, Message Kubernetes' detail.
const (
	findPodCrashLoop     = "podCrashLoop"     // restarts again and again
	findPodImagePull     = "podImagePull"     // its image cannot be pulled (Extra: the image)
	findPodUnschedulable = "podUnschedulable" // no node takes it (Message: the scheduler's)
	findPodStuckStarting = "podStuckStarting" // scheduled, but its containers do not start
	findPodNotReady      = "podNotReady"      // runs, not ready (Count ready of Limit containers)
	findPodFailed        = "podFailed"        // any other unhealthy status (Reason)
	findPodOOMKilled     = "podOOMKilled"     // runs again after being killed for its memory
	findJobFailed        = "jobFailed"        // a Job gave up (Reason, Message)
)

// jobFailureRecent is how long a failed Job stays a warning; older ones are only listed.
const jobFailureRecent = 24 * time.Hour

// checkPod is a pod as the checkup reads it: what the pod lists show, plus what only the
// checkup needs.
type checkPod struct {
	kubePod

	Phase      string
	Deleted    time.Time // when its deletion is due, zero when not deleted
	Finalizers []string
	CPU        float64 // requests, in cores
	Memory     float64 // requests, in bytes
	Scheduled  kubeCondition
	Waiting    string   // the message of a waiting container
	Claims     []string // the PersistentVolumeClaims it mounts
}

// podExtra holds the fields of a pod that podObject leaves out.
type podExtra struct {
	Metadata struct {
		DeletionTimestamp *time.Time `json:"deletionTimestamp"`
		Finalizers        []string   `json:"finalizers"`
	} `json:"metadata"`
	Spec struct {
		Containers []struct {
			Resources struct {
				Requests map[string]string `json:"requests"`
			} `json:"resources"`
		} `json:"containers"`
		Volumes []struct {
			PersistentVolumeClaim *struct {
				ClaimName string `json:"claimName"`
			} `json:"persistentVolumeClaim"`
		} `json:"volumes"`
	} `json:"spec"`
	Status struct {
		Conditions            []kubeCondition `json:"conditions"`
		InitContainerStatuses []waitingStatus `json:"initContainerStatuses"`
		ContainerStatuses     []waitingStatus `json:"containerStatuses"`
	} `json:"status"`
}

type waitingStatus struct {
	State struct {
		Waiting *struct {
			Message string `json:"message"`
		} `json:"waiting"`
	} `json:"state"`
}

// listCheckPods reads every pod of the cluster, page by page.
func listCheckPods(ctx context.Context, k *kubeClient) ([]checkPod, error) {
	pods := []checkPod{}

	err := k.listAll(ctx, "/api/v1/pods", pageQuery{}, func() { pods = pods[:0] }, func(page kubePage) error {
		for _, raw := range page.items {
			p, err := decodeCheckPod(raw)
			if err != nil {
				return err
			}

			pods = append(pods, p)
		}

		return nil
	})

	return pods, err
}

func decodeCheckPod(raw json.RawMessage) (checkPod, error) {
	var (
		obj   podObject
		extra podExtra
	)

	if err := json.Unmarshal(raw, &obj); err != nil {
		return checkPod{}, err
	}

	if err := json.Unmarshal(raw, &extra); err != nil {
		return checkPod{}, err
	}

	p := checkPod{
		kubePod:    mapPod(obj),
		Phase:      obj.Status.Phase,
		Finalizers: extra.Metadata.Finalizers,
		Scheduled:  kubeConditions(extra.Status.Conditions).get("PodScheduled"),
	}

	if extra.Metadata.DeletionTimestamp != nil {
		p.Deleted = *extra.Metadata.DeletionTimestamp
	}

	for _, c := range extra.Spec.Containers {
		p.CPU += parseQuantity(c.Resources.Requests["cpu"])
		p.Memory += parseQuantity(c.Resources.Requests["memory"])
	}

	for _, v := range extra.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			p.Claims = append(p.Claims, v.PersistentVolumeClaim.ClaimName)
		}
	}

	for _, cs := range append(extra.Status.InitContainerStatuses, extra.Status.ContainerStatuses...) {
		if cs.State.Waiting != nil && cs.State.Waiting.Message != "" {
			p.Waiting = cs.State.Waiting.Message

			break
		}
	}

	return p, nil
}

// running tells whether the pod still holds its node's resources.
func (p checkPod) running() bool { return p.Phase != "Succeeded" && p.Phase != "Failed" }

type checkJob struct {
	Metadata kubeObjectMeta `json:"metadata"`
	Status   struct {
		Conditions []struct {
			Type               string    `json:"type"`
			Status             string    `json:"status"`
			Reason             string    `json:"reason"`
			Message            string    `json:"message"`
			LastTransitionTime time.Time `json:"lastTransitionTime"`
		} `json:"conditions"`
	} `json:"status"`
}

// finished is the Job's end: whether it failed, when, and Kubernetes' reason for it.
func (j checkJob) finished() (failed, done bool, at time.Time, reason, message string) {
	for _, c := range j.Status.Conditions {
		if c.Status != "True" {
			continue
		}

		switch c.Type {
		case "Failed":
			return true, true, c.LastTransitionTime, c.Reason, c.Message
		case "Complete":
			return false, true, c.LastTransitionTime, "", ""
		}
	}

	return false, false, time.Time{}, "", ""
}

func (j checkJob) cronOwner() string {
	for _, o := range j.Metadata.OwnerReferences {
		if o.Kind == "CronJob" {
			return o.Name
		}
	}

	return ""
}

func checkupWorkloads(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	jobs, jobsErr := listObjects[checkJob](ctx, k, "/apis/batch/v1/jobs")

	findings := []checkupFinding{}

	for _, p := range in.pods {
		if f, ok := podFinding(p, in.now); ok {
			findings = append(findings, f)
		}
	}

	findings = append(findings, jobFindings(jobs, in.now)...)

	return newSection(checkWorkloads, len(in.pods)+len(jobs), findings, in.podsErr, jobsErr)
}

// podFinding is what is wrong with one pod, if anything. A pod being deleted is the
// terminating section's; a Job's failed pod is told by its Job.
func podFinding(p checkPod, now time.Time) (checkupFinding, bool) {
	f := checkupFinding{Namespace: p.Namespace, Name: p.Name, Node: p.Node, Count: p.Restarts, Since: p.Created, Reason: p.Status}
	age := now.Sub(time.UnixMilli(p.Created))

	if !p.Deleted.IsZero() {
		return f, false
	}

	if p.Healthy {
		if p.Restarts == 0 || !strings.HasPrefix(p.LastTermination, "OOMKilled") {
			return f, false
		}

		f.Kind, f.Severity, f.Reason = findPodOOMKilled, sevWarning, p.LastTermination

		return f, true
	}

	switch {
	case strings.Contains(p.Status, "CrashLoopBackOff"):
		f.Kind, f.Severity, f.Reason, f.Message = findPodCrashLoop, sevCritical, p.LastTermination, p.Waiting
	case strings.Contains(p.Status, "ImagePull") || strings.Contains(p.Status, "InvalidImageName"):
		f.Kind, f.Severity, f.Message = findPodImagePull, sevWarning, p.Waiting

		if age > pendingGrace {
			f.Severity = sevCritical
		}

		if len(p.Images) > 0 {
			f.Extra = p.Images[0]
		}
	case p.Phase == "Pending" && p.Scheduled.Status == "False":
		if age <= pendingGrace {
			return f, false
		}

		f.Kind, f.Severity, f.Reason, f.Message = findPodUnschedulable, sevCritical, p.Scheduled.Reason, p.Scheduled.Message
	case p.Phase == "Pending":
		if age <= startingGrace {
			return f, false
		}

		f.Kind, f.Severity, f.Message = findPodStuckStarting, sevWarning, p.Waiting
	case p.Status == "Running":
		if age <= startingGrace {
			return f, false
		}

		f.Kind, f.Severity, f.Count, f.Limit = findPodNotReady, sevWarning, p.Ready, float64(p.Containers)
	case strings.HasPrefix(p.Owner, "Job/"):
		return f, false
	case p.Phase == "Failed":
		// A dead pod nobody removed (Evicted, Error): it holds nothing, it only stays listed.
		f.Kind, f.Severity = findPodFailed, sevInfo
	default:
		f.Kind, f.Severity = findPodFailed, sevWarning
	}

	return f, true
}

// jobFindings are the Jobs that gave up. A CronJob's failed run that a later run made up
// for is not one.
func jobFindings(jobs []checkJob, now time.Time) []checkupFinding {
	lastSuccess := map[string]time.Time{}

	for _, j := range jobs {
		if failed, done, at, _, _ := j.finished(); done && !failed && j.cronOwner() != "" {
			key := j.Metadata.Namespace + "/" + j.cronOwner()
			if at.After(lastSuccess[key]) {
				lastSuccess[key] = at
			}
		}
	}

	findings := []checkupFinding{}

	for _, j := range jobs {
		failed, _, at, reason, message := j.finished()
		if !failed {
			continue
		}

		if owner := j.cronOwner(); owner != "" && lastSuccess[j.Metadata.Namespace+"/"+owner].After(at) {
			continue
		}

		severity := sevWarning
		if olderThan(at, now, jobFailureRecent) {
			severity = sevInfo
		}

		findings = append(findings, checkupFinding{
			Kind: findJobFailed, Severity: severity, Namespace: j.Metadata.Namespace, Name: j.Metadata.Name,
			Reason: reason, Message: message, Extra: j.cronOwner(), Since: milli(at),
		})
	}

	return findings
}
