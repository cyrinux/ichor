package ichorgo

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// A scan runs as a Job in one namespace kept for scans, so that the cluster cleans up after
// it even when the phone kills the app mid-scan: the Job ends at its deadline, then its TTL
// deletes it with its pod and the pull credentials it owns. The app deletes it itself as
// soon as it read the log.

const (
	// imageScanNamespace holds the scan Jobs; created on the first scan and kept.
	imageScanNamespace = imageScanName
	// imageScanJobTTL is how long a finished Job (and its pod, log and credentials) stays
	// when the app could not delete it: time to read the log of a follow cut short.
	imageScanJobTTL = 10 * time.Minute
	// imageScanRunLabel names a run's pod, to find it from its Job.
	imageScanRunLabel = "app.kubernetes.io/instance"
)

var imageScanJobsPath = "/apis/batch/v1/namespaces/" + imageScanNamespace + "/jobs"

// ensureScanNamespace creates the scan namespace (restricted pod security) unless it is
// there; one of the same name the app did not create is refused rather than used.
func ensureScanNamespace(ctx context.Context, k *kubeClient) error {
	var ns struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}

	err := k.get(ctx, netPerfNamespacePath(imageScanNamespace), &ns)

	switch {
	case err == nil && ns.Metadata.Labels["app.kubernetes.io/managed-by"] != "ichor":
		return netPerfRefused("the namespace %s exists and was not created by Ichor: rename or remove it to scan", imageScanNamespace)
	case err == nil:
		return nil
	case !isNotFound(err):
		return err
	}

	body := map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{"name": imageScanNamespace, "labels": map[string]string{
			"app.kubernetes.io/name":             imageScanName,
			"app.kubernetes.io/managed-by":       "ichor",
			"pod-security.kubernetes.io/enforce": "restricted",
		}},
	}

	if err := k.post(ctx, "/api/v1/namespaces", body, nil); err != nil && kubeCode(err) != http.StatusConflict {
		return fmt.Errorf("create the namespace %s: %w", imageScanNamespace, kubeError(err))
	}

	return nil
}

// imageScanJob wraps the Trivy pod in a Job that runs it once and deletes itself after its
// TTL, at the latest imageScanPodDeadline + imageScanJobTTL after it started.
func imageScanJob(name string, pod map[string]any) map[string]any {
	meta := pod["metadata"].(map[string]any)
	labels := map[string]string{imageScanRunLabel: name}

	for key, value := range meta["labels"].(map[string]string) {
		labels[key] = value
	}

	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]any{"name": name, "labels": meta["labels"]},
		"spec": map[string]any{
			"backoffLimit":            0,
			"activeDeadlineSeconds":   int(imageScanPodDeadline.Seconds()),
			"ttlSecondsAfterFinished": int(imageScanJobTTL.Seconds()),
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec":     pod["spec"],
			},
		},
	}
}

// createdJob is the part of the created Job the credentials' owner reference needs.
type createdJob struct {
	Metadata struct {
		Name string `json:"name"`
		UID  string `json:"uid"`
	} `json:"metadata"`
}

// pullSecretBody is the run's merged pull credentials, owned by its Job: deleted with it.
func pullSecretBody(name string, job createdJob, config []byte) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name": name,
			"labels": map[string]string{
				"app.kubernetes.io/name": imageScanName, "app.kubernetes.io/managed-by": "ichor",
			},
			"ownerReferences": []map[string]any{{
				"apiVersion": "batch/v1", "kind": "Job", "name": job.Metadata.Name, "uid": job.Metadata.UID,
				"blockOwnerDeletion": false,
			}},
		},
		"type": "Opaque",
		"data": map[string]string{"config.json": base64.StdEncoding.EncodeToString(config)},
	}
}

// findScanPod waits for the Job's pod to exist and returns its name. A Job whose pod cannot
// be created (pod security, a quota) only says so in its events: past timeout, it is refused.
func findScanPod(ctx context.Context, k *kubeClient, job string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	selector := url.QueryEscape(imageScanRunLabel + "=" + job)

	for {
		var list kubeList[struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}]

		if err := k.get(ctx, netPerfNamespacePath(imageScanNamespace)+"/pods?labelSelector="+selector, &list); err != nil {
			if ctx.Err() != nil {
				break
			}

			return "", err
		}

		if len(list.Items) > 0 {
			return list.Items[0].Metadata.Name, nil
		}

		select {
		case <-ctx.Done():
		case <-time.After(netPerfPoll):
			continue
		}

		break
	}

	return "", netPerfRefused("the scan Job started no pod in time: see its events in the %s namespace (pod security, quotas)", imageScanNamespace)
}

// deleteScanJob deletes the Job with its pod and credentials, also when ctx is already done.
// Left to its TTL when the call fails.
func deleteScanJob(ctx context.Context, k *kubeClient, name string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), netPerfCleanupTimeout)
	defer cancel()

	path := imageScanJobsPath + "/" + url.PathEscape(name) + "?propagationPolicy=Background"
	_ = k.do(ctx, http.MethodDelete, path, "", nil, nil) //nolint:errcheck
}
