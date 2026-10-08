package ichorgo

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	trivyVersion = "0.75.0"
	// trivyImage is Trivy's official image, pinned by digest (multi-arch index): its tags
	// were overwritten with a malicious build once (CVE-2026-33634).
	trivyImage = "docker.io/aquasec/trivy:" + trivyVersion + "@sha256:af6acf9a6b85dfe389a1941505c0ce9efef52a4719635e1a962f022a3d855daa"
	// imageScanName labels the scan namespaces, and prefixes their names.
	imageScanName      = "ichor-imagescan"
	imageScanContainer = "trivy"
	// imageScanStartTimeout bounds the start of the Trivy pod, its image pull included.
	imageScanStartTimeout = 5 * time.Minute
	// imageScanPodDeadline bounds the Trivy pod itself.
	imageScanPodDeadline = 40 * time.Minute
	// imageScanImageTimeout bounds one image's scan, imageScanDBTimeout the database
	// download (Trivy's --timeout, 5m by default: short for ~80 MB on a slow uplink).
	imageScanImageTimeout = "15m"
	imageScanDBTimeout    = "15m"
	// imageScanFollows is how many times a log follow cut short is resumed.
	imageScanFollows = 10
)

// imageScanScript downloads the database once, then scans each image ("$@") into a file
// printed between markers ($1 is a nonce no image content can guess): the log says where
// the scan is, and carries each image's report or error.
const imageScanScript = `n=$1; shift
mkdir -p /work/cache /work/tmp || exit 2
echo "@@$n db"
if ! trivy image --download-db-only --quiet --timeout ` + imageScanDBTimeout + ` 2>/work/err; then
  echo "@@$n dbfail"; tail -c 4000 /work/err; echo; echo "@@$n end db"; exit 3
fi
i=0
for img in "$@"; do
  echo "@@$n scan $i"
  if trivy image --quiet --skip-db-update --scanners vuln --image-src remote --timeout ` + imageScanImageTimeout + ` --format json --output /work/r.json "$img" 2>/work/err; then
    echo "@@$n result $i"; cat /work/r.json; echo
  else
    echo "@@$n fail $i"; tail -c 4000 /work/err; echo
  fi
  rm -f /work/r.json
  echo "@@$n end $i"
  i=$((i+1))
done
`

// runImageScan scans targets' images with one Trivy Job (see imagescan_job.go).
func runImageScan(ctx context.Context, k *kubeClient, refs []routePod, opts imageScanOptions, emit func(imageScanProgress)) (imageScanReport, error) {
	report := newImageScanReport(imageScanSourceScan, "Trivy "+trivyVersion)

	emit(imageScanProgress{Phase: imageScanPhasePreparing})

	targets, err := readScanTargets(ctx, k, refs)
	if err != nil {
		return report, err
	}

	if len(targets) > imageScanMaxImages {
		return report, netPerfRefused("these pods run %d images, more than the %d one scan takes", len(targets), imageScanMaxImages)
	}

	report.Images = pendingImages(targets)

	auth, _, err := readPullCredentials(ctx, k, targetSecrets(targets), targetRegistries(targets))
	if err != nil {
		return report, err
	}

	if err := ensureScanNamespace(ctx, k); err != nil {
		return report, err
	}

	nonce, err := scanNonce()
	if err != nil {
		return report, err
	}

	job, secret := "scan-"+nonce[:10], ""
	if auth != nil {
		secret = job + "-auth"
	}

	var created createdJob
	if err := k.post(ctx, imageScanJobsPath, imageScanJob(job, imageScanPodSpec(nonce, targets, opts, secret)), &created); err != nil {
		return report, fmt.Errorf("create the scan Job: %w", kubeError(err))
	}

	defer func() {
		emit(imageScanProgress{Phase: imageScanPhaseCleaning, Steps: len(targets)})
		deleteScanJob(ctx, k, job)
	}()

	// The pod waits for its credentials to mount: they are created once the Job is, to be
	// owned by it.
	if auth != nil {
		if err := k.post(ctx, netPerfNamespacePath(imageScanNamespace)+"/secrets", pullSecretBody(secret, created, auth), nil); err != nil {
			return report, fmt.Errorf("copy the pull credentials: %w", kubeError(err))
		}
	}

	err = followImageScan(ctx, k, job, nonce, targets, &report, emit)
	report.Finished = time.Now().UnixMilli()

	return report, err
}

// pendingImages are the targets before their scan: each says so until its report arrives.
func pendingImages(targets []scanTarget) []scannedImage {
	out := make([]scannedImage, len(targets))

	for i, t := range targets {
		out[i] = scannedImage{
			Image: t.image, Ref: t.ref, Digest: t.digest, Pods: t.pods,
			Error: "not scanned", Vulnerabilities: []imageVuln{},
		}
	}

	return out
}

func targetSecrets(targets []scanTarget) []string {
	var out []string

	for _, t := range targets {
		for _, s := range t.secrets {
			out = appendNew(out, s)
		}
	}

	return out
}

func scanNonce() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

// imageScanPodSpec is the Trivy pod: restricted like the network test's, its database and
// layers in a /work emptyDir, the pull credentials (the secret named, if any) at
// /docker/config.json.
func imageScanPodSpec(nonce string, targets []scanTarget, opts imageScanOptions, secret string) map[string]any {
	img := runPodImage{
		app: imageScanName, image: trivyImage, container: imageScanContainer,
		env: []map[string]string{
			{"name": "HOME", "value": "/work"},
			{"name": "TMPDIR", "value": "/work/tmp"},
			{"name": "TRIVY_CACHE_DIR", "value": "/work/cache"},
			{"name": "TRIVY_NO_PROGRESS", "value": "true"},
			{"name": "TRIVY_DISABLE_TELEMETRY", "value": "true"},
			{"name": "TRIVY_SKIP_VERSION_CHECK", "value": "true"},
		},
		mounts:  []map[string]any{{"name": "work", "mountPath": "/work"}},
		volumes: []map[string]any{{"name": "work", "emptyDir": map[string]string{"sizeLimit": "8Gi"}}},
		resources: map[string]any{
			"requests": map[string]string{"cpu": "250m", "memory": "512Mi"},
			"limits":   map[string]string{"memory": "4Gi"},
		},
	}

	if opts.DBRepository != "" {
		img.env = append(img.env, map[string]string{"name": "TRIVY_DB_REPOSITORY", "value": opts.DBRepository})
	}

	if opts.JavaDBRepository != "" {
		img.env = append(img.env, map[string]string{"name": "TRIVY_JAVA_DB_REPOSITORY", "value": opts.JavaDBRepository})
	}

	if secret != "" {
		img.env = append(img.env, map[string]string{"name": "DOCKER_CONFIG", "value": "/docker"})
		img.mounts = append(img.mounts, map[string]any{"name": "docker", "mountPath": "/docker", "readOnly": true})
		img.volumes = append(img.volumes, map[string]any{"name": "docker", "secret": map[string]any{
			"secretName": secret, "defaultMode": 0o440,
		}})
	}

	command := []string{"sh", "-c", imageScanScript, "sh", nonce}
	for _, t := range targets {
		command = append(command, t.ref)
	}

	return runPodSpec(img, "", "", false, imageScanPodDeadline, command...)
}

// followImageScan waits for the Trivy pod to start, then follows its log until it ends,
// reading the progress and reports into report. A follow cut short (the API server
// restarted, the phone's network changed) starts over from the top: the reports read again
// replace the same images. Once the pod ended, a follow that did not read to the end is
// replaced by one plain read of the whole log.
func followImageScan(ctx context.Context, k *kubeClient, job, nonce string, targets []scanTarget, report *imageScanReport, emit func(imageScanProgress)) error {
	emit(imageScanProgress{Phase: imageScanPhaseStarting, Steps: len(targets)})

	podName, err := findScanPod(ctx, k, job, imageScanStartTimeout)
	if err != nil {
		return err
	}

	onWait := func(reason string) {
		emit(imageScanProgress{Phase: imageScanPhaseStarting, Steps: len(targets), Message: reason})
	}

	_, err = waitRunPod(ctx, k, "Trivy", imageScanNamespace, podName, "its node", imageScanStartTimeout, onWait, func(pod netPerfPod) bool {
		return pod.Status.Phase != "Pending" && pod.Status.Phase != ""
	})
	if err != nil {
		return err
	}

	podPath := netPerfNamespacePath(imageScanNamespace) + "/pods/" + url.PathEscape(podName)
	logPath := func(follow bool) string {
		return podPath + "/log?" + url.Values{"container": {imageScanContainer}, "follow": {strconv.FormatBool(follow)}}.Encode()
	}

	var lastErr error

	for range imageScanFollows {
		parser := newScanLogParser(nonce, targets, report, emit)
		streamErr := k.stream(ctx, logPath(true), "text/plain, */*", parser.read)

		if ctx.Err() != nil {
			return ctx.Err()
		}

		var pod netPerfPod
		if lastErr = k.get(ctx, podPath, &pod); lastErr != nil {
			sleepCtx(ctx, netPerfPoll)

			continue
		}

		if pod.Status.Phase != "Succeeded" && pod.Status.Phase != "Failed" {
			lastErr = streamErr

			continue
		}

		if streamErr != nil {
			parser = newScanLogParser(nonce, targets, report, func(imageScanProgress) {})
			if err := k.stream(ctx, logPath(false), "text/plain, */*", parser.read); err != nil {
				return fmt.Errorf("read the Trivy pod's log: %w", kubeError(err))
			}
		}

		return parser.outcome(pod)
	}

	if lastErr != nil {
		return fmt.Errorf("follow the Trivy pod's log: %w", kubeError(lastErr))
	}

	return netPerfRefused("lost the Trivy pod's log")
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// trivyError turns the end of Trivy's error output into a sentence: its last line without
// the timestamp and level, after a plain-words reason for the common cases.
func trivyError(text string) string {
	last := ""

	for line := range strings.SplitSeq(strings.TrimSpace(text), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			last = line
		}
	}

	if fields := strings.Split(last, "\t"); len(fields) > 1 {
		last = fields[len(fields)-1]
	}

	if len(last) > 400 {
		last = last[:400] + "…"
	}

	lower := strings.ToLower(text)

	var reason string

	switch {
	case strings.Contains(lower, "toomanyrequests"):
		reason = "the registry rate-limited the pull (Docker Hub limits anonymous pulls)"
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "denied"):
		reason = "the registry refused the pull: a private image the pods' imagePullSecrets do not cover, or an image that does not exist"
	case strings.Contains(lower, "manifest_unknown") || strings.Contains(lower, "not found"):
		reason = "the image is no longer in its registry"
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		reason = "the scan timed out"
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "connection refused"):
		reason = "the cluster cannot reach the registry"
	}

	switch {
	case reason == "":
		return cmp.Or(last, "Trivy failed without a message")
	case last == "":
		return reason
	default:
		return reason + ": " + last
	}
}
