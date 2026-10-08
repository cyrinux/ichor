package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

// operatorReports is ImageScanOperatorReports' answer.
type operatorReports struct {
	Available bool            `json:"available"` // the Trivy Operator's CRD is installed
	Report    imageScanReport `json:"report"`
}

// vulnerabilityReport is the part of an aquasecurity.github.io/v1alpha1 VulnerabilityReport
// the app reads: one per workload container, refreshed by the operator.
type vulnerabilityReport struct {
	Report struct {
		UpdateTimestamp time.Time `json:"updateTimestamp"`
		Scanner         struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"scanner"`
		Registry struct {
			Server string `json:"server"`
		} `json:"registry"`
		Artifact struct {
			Repository string `json:"repository"`
			Tag        string `json:"tag"`
			Digest     string `json:"digest"`
		} `json:"artifact"`
		OS struct {
			Family string `json:"family"`
			Name   string `json:"name"`
			EOSL   bool   `json:"eosl"`
		} `json:"os"`
		Vulnerabilities []operatorVuln `json:"vulnerabilities"`
	} `json:"report"`
}

type operatorVuln struct {
	VulnerabilityID  string   `json:"vulnerabilityID"`
	Resource         string   `json:"resource"`
	InstalledVersion string   `json:"installedVersion"`
	FixedVersion     string   `json:"fixedVersion"`
	Severity         string   `json:"severity"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	PrimaryLink      string   `json:"primaryLink"`
	Score            *float64 `json:"score"`
	Target           string   `json:"target"`
	Class            string   `json:"class"`
	PackageType      string   `json:"packageType"`
	PackagePURL      string   `json:"packagePURL"`
	PublishedDate    string   `json:"publishedDate"`
}

const vulnerabilityReportsPath = "/apis/aquasecurity.github.io/v1alpha1/namespaces/"

// readOperatorReports finds the newest VulnerabilityReport of each of the pods' images.
func readOperatorReports(ctx context.Context, k *kubeClient, refs []routePod) (operatorReports, error) {
	out := operatorReports{Report: newImageScanReport(imageScanSourceOperator, "Trivy Operator")}

	targets, err := readScanTargets(ctx, k, refs)

	var refusal *netPerfRefusal
	if errors.As(err, &refusal) {
		return out, nil
	}

	if err != nil {
		return out, err
	}

	byOwner := map[string][]vulnerabilityReport{}

	for _, t := range targets {
		for _, owner := range t.owners {
			if _, read := byOwner[owner]; read {
				continue
			}

			reports, err := ownerReports(ctx, k, owner)
			if isNotFound(err) {
				return out, nil // the CRD is not installed
			}

			if err != nil {
				return out, err
			}

			byOwner[owner] = reports
		}
	}

	out.Available = true

	for _, t := range targets {
		if r, ok := newestReport(t, byOwner); ok {
			out.Report.Images = append(out.Report.Images, operatorImage(t, r))
			out.Report.Scanner = strings.TrimSpace("Trivy Operator (" + r.Report.Scanner.Name + " " + r.Report.Scanner.Version + ")")
		}
	}

	out.Report.Finished = time.Now().UnixMilli()

	return out, nil
}

// ownerReports lists the reports on one controller's containers (namespace/kind/name), by
// the labels the operator sets: never the namespace's whole list, whose reports carry every
// finding's description.
func ownerReports(ctx context.Context, k *kubeClient, owner string) ([]vulnerabilityReport, error) {
	ns, rest, _ := strings.Cut(owner, "/")
	kind, name, _ := strings.Cut(rest, "/")
	selector := url.QueryEscape("trivy-operator.resource.kind=" + kind + ",trivy-operator.resource.name=" + name)

	var list kubeList[vulnerabilityReport]

	err := getList(ctx, k, vulnerabilityReportsPath+url.PathEscape(ns)+"/vulnerabilityreports?labelSelector="+selector, &list)

	return list.Items, err
}

// newestReport is the latest report on target's image among its controllers' reports: by
// digest; by repository and tag only when the digest is unknown, since a tag may have moved.
func newestReport(t scanTarget, byOwner map[string][]vulnerabilityReport) (vulnerabilityReport, bool) {
	var (
		best  vulnerabilityReport
		found bool
	)

	want := parseImageRef(t.image)

	for _, owner := range t.owners {
		for _, r := range byOwner[owner] {
			a := r.Report.Artifact
			got := parseImageRef(operatorRegistry(r.Report.Registry.Server) + "/" + a.Repository + ":" + a.Tag)

			match := a.Digest == t.digest
			if t.digest == "" {
				match = got.Repo() == want.Repo() && a.Tag != "" && a.Tag == want.Tag
			}

			if match && (!found || r.Report.UpdateTimestamp.After(best.Report.UpdateTimestamp)) {
				best, found = r, true
			}
		}
	}

	return best, found
}

// operatorRegistry is the registry as image references name it (the operator records
// Docker Hub as index.docker.io).
func operatorRegistry(server string) string {
	if server == "" || server == "index.docker.io" || server == "registry-1.docker.io" {
		return defaultRegistry
	}

	return server
}

func operatorImage(t scanTarget, r vulnerabilityReport) scannedImage {
	img := scannedImage{
		Image: t.image, Ref: t.ref, Digest: cmp.Or(t.digest, r.Report.Artifact.Digest), Pods: t.pods,
		OS: strings.TrimSpace(r.Report.OS.Family + " " + r.Report.OS.Name), ScannedAt: r.Report.UpdateTimestamp.UnixMilli(),
	}

	if r.Report.OS.EOSL {
		img.OS += " (end of life)"
	}

	vulns := make([]imageVuln, 0, len(r.Report.Vulnerabilities))

	for _, v := range r.Report.Vulnerabilities {
		iv := imageVuln{
			ID: v.VulnerabilityID, Package: v.Resource, Installed: v.InstalledVersion, Fixed: v.FixedVersion,
			Severity: normalSeverity(v.Severity), Title: v.Title, Description: v.Description, URL: v.PrimaryLink,
			PURL: v.PackagePURL, Target: v.Target, Class: v.Class, Type: v.PackageType, Published: v.PublishedDate,
		}
		if v.Score != nil {
			iv.Score = *v.Score
		}

		vulns = append(vulns, iv)
	}

	return withVulns(img, vulns)
}
