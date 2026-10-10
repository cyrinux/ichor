package ichorgo

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Severities as Trivy names them, most severe first.
var vulnSeverities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "UNKNOWN"}

// Package classes as Trivy names them: the image's OS packages, or libraries an app bundles.
const vulnClassOS = "os-pkgs"

// imageScanReport is the vulnerabilities of an app's images, from a scan or the operator.
type imageScanReport struct {
	Source   string         `json:"source"`  // imageScanSourceScan or imageScanSourceOperator
	Scanner  string         `json:"scanner"` // "Trivy 0.75.0"
	Started  int64          `json:"started"` // epoch ms
	Finished int64          `json:"finished"`
	Images   []scannedImage `json:"images"`
}

type scannedImage struct {
	Image     string      `json:"image"`            // as the pods name it (tag)
	Ref       string      `json:"ref"`              // what was scanned: repo@digest when known
	Digest    string      `json:"digest,omitempty"` // sha256:…
	OS        string      `json:"os,omitempty"`     // "debian 12.5"
	Pods      []string    `json:"pods"`             // namespace/pod running it
	ScannedAt int64       `json:"scannedAt,omitempty"`
	Error     string      `json:"error,omitempty"`
	Summary   vulnSummary `json:"summary"`
	// NewSummary counts the findings not in the last kept scan of the same digest, scanned at
	// PreviousScannedAt (0 when there is none: nothing is new then).
	NewSummary        vulnSummary `json:"new"`
	PreviousScannedAt int64       `json:"previousScannedAt,omitempty"`
	Vulnerabilities   []imageVuln `json:"vulnerabilities"`
}

type vulnSummary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
	Fixable  int `json:"fixable"` // with a fixed version
	OS       int `json:"os"`      // in the OS packages (a newer base image fixes them)
}

type imageVuln struct {
	ID          string  `json:"id"` // CVE-…, GHSA-…
	Package     string  `json:"package"`
	Installed   string  `json:"installed"`
	Fixed       string  `json:"fixed,omitempty"` // versions that fix it, comma-separated
	Severity    string  `json:"severity"`        // one of vulnSeverities
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	URL         string  `json:"url,omitempty"`
	Score       float64 `json:"score,omitempty"` // CVSS v3 (or v4, v2) base score
	Vector      string  `json:"vector,omitempty"`
	PURL        string  `json:"purl,omitempty"`
	Target      string  `json:"target,omitempty"` // "debian 12.5", "usr/local/bin/app"
	Class       string  `json:"class,omitempty"`  // os-pkgs, lang-pkgs
	Type        string  `json:"type,omitempty"`   // debian, gobinary, jar…
	Published   string  `json:"published,omitempty"`
	New         bool    `json:"new,omitempty"` // not in the last kept scan of the same digest
}

func newImageScanReport(source, scanner string) imageScanReport {
	return imageScanReport{Source: source, Scanner: scanner, Started: time.Now().UnixMilli(), Images: []scannedImage{}}
}

// trivyReport is the part of `trivy image --format json` (schema 2) the app reads.
type trivyReport struct {
	SchemaVersion int    `json:"SchemaVersion"`
	ArtifactName  string `json:"ArtifactName"`
	Metadata      struct {
		OS *struct {
			Family string `json:"Family"`
			Name   string `json:"Name"`
			EOSL   bool   `json:"EOSL"`
		} `json:"OS"`
		RepoDigests []string `json:"RepoDigests"`
	} `json:"Metadata"`
	Results []struct {
		Target          string      `json:"Target"`
		Class           string      `json:"Class"`
		Type            string      `json:"Type"`
		Vulnerabilities []trivyVuln `json:"Vulnerabilities"`
	} `json:"Results"`
}

type trivyVuln struct {
	VulnerabilityID string `json:"VulnerabilityID"`
	PkgName         string `json:"PkgName"`
	PkgIdentifier   struct {
		PURL string `json:"PURL"`
	} `json:"PkgIdentifier"`
	InstalledVersion string                    `json:"InstalledVersion"`
	FixedVersion     string                    `json:"FixedVersion"`
	Severity         string                    `json:"Severity"`
	Title            string                    `json:"Title"`
	Description      string                    `json:"Description"`
	PrimaryURL       string                    `json:"PrimaryURL"`
	SeveritySource   string                    `json:"SeveritySource"`
	CVSS             map[string]trivyCVSSScore `json:"CVSS"`
	PublishedDate    string                    `json:"PublishedDate"`
}

type trivyCVSSScore struct {
	V2Vector  string  `json:"V2Vector"`
	V3Vector  string  `json:"V3Vector"`
	V40Vector string  `json:"V40Vector"`
	V2Score   float64 `json:"V2Score"`
	V3Score   float64 `json:"V3Score"`
	V40Score  float64 `json:"V40Score"`
}

// parseTrivyReport reads one image's Trivy JSON into img (its Image, Ref and Pods kept).
func parseTrivyReport(data []byte, img scannedImage) (scannedImage, error) {
	var tr trivyReport
	if err := json.Unmarshal(data, &tr); err != nil {
		return img, fmt.Errorf("unreadable Trivy report: %w", err)
	}

	if tr.SchemaVersion != 2 {
		return img, fmt.Errorf("unsupported Trivy report schema %d", tr.SchemaVersion)
	}

	if os := tr.Metadata.OS; os != nil {
		img.OS = strings.TrimSpace(os.Family + " " + os.Name)
		if os.EOSL {
			img.OS += " (end of life)"
		}
	}

	if img.Digest == "" && len(tr.Metadata.RepoDigests) > 0 {
		_, img.Digest, _ = strings.Cut(tr.Metadata.RepoDigests[0], "@")
	}

	vulns := []imageVuln{}

	for _, r := range tr.Results {
		for _, v := range r.Vulnerabilities {
			vulns = append(vulns, trivyVulnOf(v, r.Target, r.Class, r.Type))
		}
	}

	return withVulns(img, vulns), nil
}

func trivyVulnOf(v trivyVuln, target, class, typ string) imageVuln {
	score, vector := cvssOf(v.CVSS, v.SeveritySource)

	return imageVuln{
		ID: v.VulnerabilityID, Package: v.PkgName, Installed: v.InstalledVersion, Fixed: v.FixedVersion,
		Severity: normalSeverity(v.Severity), Title: v.Title, Description: v.Description, URL: v.PrimaryURL,
		Score: score, Vector: vector, PURL: v.PkgIdentifier.PURL, Target: target, Class: class, Type: typ,
		Published: v.PublishedDate,
	}
}

// cvssOf picks one base score: the severity source's, else NVD's, else any; v3 before v4 and
// v2 (the scores most tools show).
func cvssOf(scores map[string]trivyCVSSScore, source string) (float64, string) {
	sources := []string{source, "nvd"}

	for name := range scores {
		sources = append(sources, name)
	}

	slices.Sort(sources[2:])

	for _, s := range sources {
		c, ok := scores[s]

		switch {
		case !ok:
		case c.V3Score > 0:
			return c.V3Score, c.V3Vector
		case c.V40Score > 0:
			return c.V40Score, c.V40Vector
		case c.V2Score > 0:
			return c.V2Score, c.V2Vector
		}
	}

	return 0, ""
}

func normalSeverity(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if slices.Contains(vulnSeverities, s) {
		return s
	}

	return "UNKNOWN"
}

func vulnSeverityRank(s string) int {
	if i := slices.Index(vulnSeverities, s); i >= 0 {
		return i
	}

	return len(vulnSeverities)
}

// withVulns sets img's vulnerabilities, deduplicated (the same one reached through two
// targets is kept once), sorted most severe first, fixable first, by score, and counted.
func withVulns(img scannedImage, vulns []imageVuln) scannedImage {
	seen := map[string]bool{}
	out := make([]imageVuln, 0, len(vulns))

	for _, v := range vulns {
		key := v.ID + "\x00" + v.Package + "\x00" + v.Installed + "\x00" + v.Target
		if !seen[key] {
			seen[key] = true
			out = append(out, v)
		}
	}

	slices.SortStableFunc(out, func(a, b imageVuln) int {
		return cmp.Or(
			cmp.Compare(vulnSeverityRank(a.Severity), vulnSeverityRank(b.Severity)),
			cmp.Compare(boolRank(a.Fixed == ""), boolRank(b.Fixed == "")),
			cmp.Compare(b.Score, a.Score),
			strings.Compare(a.Package, b.Package),
			strings.Compare(a.ID, b.ID),
		)
	})

	img.Vulnerabilities = out
	img.Summary = summarizeVulns(out)

	return img
}

func boolRank(b bool) int {
	if b {
		return 1
	}

	return 0
}

func summarizeVulns(vulns []imageVuln) vulnSummary {
	var s vulnSummary

	for _, v := range vulns {
		switch v.Severity {
		case "CRITICAL":
			s.Critical++
		case "HIGH":
			s.High++
		case "MEDIUM":
			s.Medium++
		case "LOW":
			s.Low++
		default:
			s.Unknown++
		}

		if v.Fixed != "" {
			s.Fixable++
		}

		if v.Class == vulnClassOS {
			s.OS++
		}
	}

	return s
}
