package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// An image scan looks for known vulnerabilities in the images an app runs, with Trivy in a
// short-lived pod of the cluster, like a network test: the cluster downloads the
// vulnerability database and the image layers, not the phone. The images are scanned as they
// run, by the digest the kubelet pulled (status.containerStatuses[].imageID), with the pods'
// own pull secrets. When the Trivy Operator runs in the cluster, its reports are read instead
// of scanning (ImageScanOperatorReports). Either report exports to SARIF, CycloneDX, HTML,
// CSV or JSON (ImageScanExport).

const (
	// imageScanTimeout bounds a whole scan: database download, every image, cleanup.
	imageScanTimeout = 45 * time.Minute
	// imageScanMaxImages bounds the images of one scan (an app has a handful).
	imageScanMaxImages = 20
)

// Image scan phases reported through ImageScanListener.OnProgress.
const (
	imageScanPhasePreparing = "preparing" // reading the pods, their images and pull secrets
	imageScanPhaseStarting  = "starting"  // starting the Trivy pod, pulling its image
	imageScanPhaseDatabase  = "database"  // downloading the vulnerability database
	imageScanPhaseScanning  = "scanning"  // one image (step of steps)
	imageScanPhaseCleaning  = "cleaning"  // deleting the namespace
)

// Report sources.
const (
	imageScanSourceScan     = "scan"     // a Trivy pod run by the app
	imageScanSourceOperator = "operator" // the Trivy Operator's VulnerabilityReports
)

var (
	errImageScanStopped  = errors.New("image scan stopped")
	errImageScanTimedOut = errors.New("image scan timed out")
)

// ImageScanListener follows an image scan (implemented in Kotlin/Swift).
type ImageScanListener interface {
	// OnProgress gets {"phase","step","steps","image","message","at"} on every step.
	OnProgress(json string)
	// OnDone is called exactly once with the report (see imageScanReport; the images
	// scanned so far on failure) and errMessage, empty on success.
	OnDone(reportJSON string, errMessage string)
}

// ImageScanRun is a handle on a running image scan.
type ImageScanRun struct {
	cancel context.CancelFunc
}

// Cancel stops the scan; its namespace is still deleted and OnDone follows.
func (r *ImageScanRun) Cancel() { r.cancel() }

type imageScanProgress struct {
	Phase   string `json:"phase"`
	Step    int    `json:"step,omitempty"`
	Steps   int    `json:"steps"`
	Image   string `json:"image,omitempty"`
	Message string `json:"message,omitempty"`
	At      int64  `json:"at"`
}

// imageScanOptions are the scan's settings, from StartImageScan's options JSON.
type imageScanOptions struct {
	// DBRepository and JavaDBRepository replace Trivy's database locations (OCI
	// repositories), for a cluster that cannot reach mirror.gcr.io and ghcr.io.
	DBRepository     string `json:"dbRepository,omitempty"`
	JavaDBRepository string `json:"javaDBRepository,omitempty"`
}

func decodeImageScanOptions(s string) (imageScanOptions, error) {
	var o imageScanOptions

	if strings.TrimSpace(s) == "" {
		return o, nil
	}

	if err := json.Unmarshal([]byte(s), &o); err != nil {
		return o, fmt.Errorf("invalid scan options: %w", err)
	}

	for _, repo := range []string{o.DBRepository, o.JavaDBRepository} {
		if strings.ContainsAny(repo, " \t\n\"'`$;&|<>") {
			return o, fmt.Errorf("invalid database repository %q", repo)
		}
	}

	return o, nil
}

// StartImageScan scans the images of the given pods for known vulnerabilities with Trivy
// (os:admin): pods [{namespace,pod}] as KubeAppWorkloads, options {"dbRepository",
// "javaDBRepository"} or "". It reads the pods' images by digest and their pull secrets,
// creates a namespace, runs one Trivy pod there that downloads the database then scans each
// image in turn, and deletes the namespace at the end, also on failure or Cancel. The pull
// secrets are copied into that namespace for the run. kubeServer: see KubePods.
func StartImageScan(configYAML, contextName, kubeServer, pods, options string, listener ImageScanListener) *ImageScanRun {
	contextName = unmaskContext(configYAML, contextName)

	listener = maskedImageScanListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), imageScanTimeout)
	emit := func(p imageScanProgress) {
		p.At = time.Now().UnixMilli()
		emitJSON(p, listener.OnProgress)
	}

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", msg) })

		report, err := startImageScan(ctx, kubeTarget{configYAML, contextName, kubeServer}, pods, options, emit)

		listener.OnDone(imageScanDone(ctx, report, err))
	}()

	return &ImageScanRun{cancel: cancel}
}

func startImageScan(ctx context.Context, target kubeTarget, pods, options string, emit func(imageScanProgress)) (imageScanReport, error) {
	report := newImageScanReport(imageScanSourceScan, "Trivy "+trivyVersion)

	refs, err := decodeRoutePods(pods)
	if err != nil {
		return report, err
	}

	opts, err := decodeImageScanOptions(options)
	if err != nil {
		return report, err
	}

	if isDemoContext(target.config, target.context) {
		return runDemoImageScan(ctx, emit)
	}

	return runImageScanWith(ctx, target, refs, opts, emit)
}

// runImageScanWith runs a scan with target's client. A refusal is returned as is, outside
// withKubeContext, which would take it for a failed call.
func runImageScanWith(ctx context.Context, target kubeTarget, refs []routePod, opts imageScanOptions, emit func(imageScanProgress)) (imageScanReport, error) {
	var (
		report  imageScanReport
		refusal error
	)

	_, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (struct{}, error) {
		var runErr error
		report, runErr = runImageScan(ctx, k, refs, opts, emit)

		var r *netPerfRefusal
		if errors.As(runErr, &r) {
			refusal, runErr = runErr, nil
		}

		return struct{}{}, runErr
	})
	if refusal != nil {
		err = refusal
	}

	return report, err
}

// imageScanDone is what OnDone gets for a scan that returned report and err.
func imageScanDone(ctx context.Context, report imageScanReport, err error) (string, string) {
	errMessage := ""

	switch {
	case err == nil:
	case errors.Is(ctx.Err(), context.Canceled):
		errMessage = errImageScanStopped.Error()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		errMessage = errImageScanTimedOut.Error()
	default:
		errMessage = err.Error()
	}

	if report.Finished == 0 {
		report.Finished = time.Now().UnixMilli()
	}

	if report.Images == nil {
		report.Images = []scannedImage{}
	}

	js, jsErr := toJSON(report)
	if jsErr != nil && errMessage == "" {
		errMessage = jsErr.Error()
	}

	return js, errMessage
}

// ImageScanOperatorReports reads the Trivy Operator's VulnerabilityReports for the images of
// the given pods (os:admin; pods as StartImageScan): {"available","report"}, available false
// when the operator's CRD is not installed; report as StartImageScan's, an image without a
// report left out. kubeServer: see KubePods.
func ImageScanOperatorReports(configYAML, contextName, kubeServer, pods string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	refs, err := decodeRoutePods(pods)
	if err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoOperatorReports,
		func(ctx context.Context, k *kubeClient) (operatorReports, error) {
			return readOperatorReports(ctx, k, refs)
		})
}

// ImageScanExport writes report (StartImageScan's or ImageScanOperatorReports' report) in
// format: "sarif" (SARIF 2.1.0), "cyclonedx" (CycloneDX 1.6 JSON), "html" (a self-contained
// page, printable to PDF), "csv" or "json".
func ImageScanExport(reportJSON, format string) (out string, err error) {
	defer maskResult(&out, &err)

	var report imageScanReport
	if err := json.Unmarshal([]byte(reportJSON), &report); err != nil {
		return "", fmt.Errorf("invalid report: %w", err)
	}

	return exportImageScan(report, format, time.Now())
}

func exportImageScan(report imageScanReport, format string, now time.Time) (string, error) {
	switch format {
	case "sarif":
		return toJSON(sarifReport(report))
	case "cyclonedx":
		return toJSON(cycloneDXReport(report, now))
	case "html":
		return htmlReport(report)
	case "csv":
		return csvReport(report)
	case "json":
		return toJSON(report)
	default:
		return "", fmt.Errorf("unknown report format %q", format)
	}
}
