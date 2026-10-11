package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Grype is the second scanner a scan may run instead of Trivy (options {"scanner":"grype"}):
// the same Job, script markers, report model and exports; only the pod image, the script and
// the JSON it prints differ.

const (
	grypeVersion = "0.120.1"
	// grypeImage is Anchore's debug image (the plain one has no shell for the script), pinned
	// by digest (multi-arch index) like trivyImage.
	grypeImage = "docker.io/anchore/grype:v" + grypeVersion + "-debug@sha256:b920958a23c3ee77c63ecb99a85d6f9beaa90b633c1104267ce7b7efa830098b"
)

// Scanner names in the scan options.
const (
	scannerTrivy = "trivy"
	scannerGrype = "grype"
)

// grypeScanScript is imageScanScript for Grype: the same markers, the database downloaded
// once, each image pulled from its registry ("registry:" never reads as a flag).
const grypeScanScript = `n=$1; shift
mkdir -p /work/cache /work/tmp || exit 2
echo "@@$n db"
if ! /grype db update >/dev/null 2>/work/err; then
  echo "@@$n dbfail"; tail -c 4000 /work/err; echo; echo "@@$n end db"; exit 3
fi
i=0
for img in "$@"; do
  echo "@@$n scan $i"
  if /grype "registry:$img" -o json --file /work/r.json 2>/work/err; then
    echo "@@$n result $i"; cat /work/r.json; echo
  else
    echo "@@$n fail $i"; tail -c 4000 /work/err; echo
  fi
  rm -f /work/r.json
  echo "@@$n end $i"
  i=$((i+1))
done
`

// scannerSpec is what differs between the scanners a scan may run.
type scannerSpec struct {
	id      string // the container's name, the options' scanner
	name    string // "Trivy", in the report and the errors
	version string
	image   string
	shell   string
	script  string
	env     func(imageScanOptions) []map[string]string
	parse   func([]byte, scannedImage) (scannedImage, error)
	errorOf func(string) string
}

// label is the report's scanner: "Trivy 0.75.0".
func (s scannerSpec) label() string { return s.name + " " + s.version }

var trivyScanner = scannerSpec{
	id: scannerTrivy, name: "Trivy", version: trivyVersion, image: trivyImage, shell: "sh", script: imageScanScript,
	env: func(opts imageScanOptions) []map[string]string {
		env := []map[string]string{
			{"name": "TRIVY_CACHE_DIR", "value": "/work/cache"},
			{"name": "TRIVY_NO_PROGRESS", "value": "true"},
			{"name": "TRIVY_DISABLE_TELEMETRY", "value": "true"},
			{"name": "TRIVY_SKIP_VERSION_CHECK", "value": "true"},
		}

		if opts.DBRepository != "" {
			env = append(env, map[string]string{"name": "TRIVY_DB_REPOSITORY", "value": opts.DBRepository})
		}

		if opts.JavaDBRepository != "" {
			env = append(env, map[string]string{"name": "TRIVY_JAVA_DB_REPOSITORY", "value": opts.JavaDBRepository})
		}

		return env
	},
	parse:   parseTrivyReport,
	errorOf: trivyError,
}

var grypeScanner = scannerSpec{
	id: scannerGrype, name: "Grype", version: grypeVersion, image: grypeImage, shell: "/busybox/sh", script: grypeScanScript,
	env: func(opts imageScanOptions) []map[string]string {
		env := []map[string]string{
			{"name": "GRYPE_DB_CACHE_DIR", "value": "/work/cache"},
			{"name": "GRYPE_DB_AUTO_UPDATE", "value": "false"},
			{"name": "GRYPE_CHECK_FOR_APP_UPDATE", "value": "false"},
			{"name": "GRYPE_DB_UPDATE_DOWNLOAD_TIMEOUT", "value": imageScanDBTimeout},
			{"name": "GRYPE_DB_UPDATE_AVAILABLE_TIMEOUT", "value": "2m"},
		}

		if opts.GrypeDBUpdateURL != "" {
			env = append(env, map[string]string{"name": "GRYPE_DB_UPDATE_URL", "value": opts.GrypeDBUpdateURL})
		}

		return env
	},
	parse:   parseGrypeReport,
	errorOf: func(text string) string { return scannerError("Grype", text) },
}

// scannerOf is the scanner opts chose (decodeImageScanOptions refused any other).
func scannerOf(opts imageScanOptions) scannerSpec {
	if opts.Scanner == scannerGrype {
		return grypeScanner
	}

	return trivyScanner
}

// checkScanner refuses a scanner other than Trivy or Grype, and a database mirror that is not
// a plain http(s) URL.
func checkScanner(o imageScanOptions) error {
	if o.Scanner != "" && o.Scanner != scannerTrivy && o.Scanner != scannerGrype {
		return fmt.Errorf("unknown scanner %q (trivy or grype)", o.Scanner)
	}

	if o.GrypeDBUpdateURL == "" {
		return nil
	}

	u, err := url.Parse(o.GrypeDBUpdateURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
		strings.ContainsAny(o.GrypeDBUpdateURL, " \t\n\"'`$;&|<>") {
		return fmt.Errorf("invalid Grype database URL %q", o.GrypeDBUpdateURL)
	}

	return nil
}

// grypeReport is the part of `grype -o json` the app reads.
type grypeReport struct {
	Matches []grypeMatch `json:"matches"`
	Source  *struct {
		Type   string `json:"type"`
		Target struct {
			RepoDigests []string `json:"repoDigests"`
		} `json:"target"`
	} `json:"source"`
	Distro struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"distro"`
}

type grypeMatch struct {
	Vulnerability        grypeVuln   `json:"vulnerability"`
	RelatedVulnerability []grypeVuln `json:"relatedVulnerabilities"`
	Artifact             struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Type      string `json:"type"`
		PURL      string `json:"purl"`
		Locations []struct {
			Path string `json:"path"`
		} `json:"locations"`
	} `json:"artifact"`
}

type grypeVuln struct {
	ID          string      `json:"id"`
	DataSource  string      `json:"dataSource"`
	Severity    string      `json:"severity"`
	Description string      `json:"description"`
	URLs        []string    `json:"urls"`
	CVSS        []grypeCVSS `json:"cvss"`
	Fix         struct {
		Versions []string `json:"versions"`
	} `json:"fix"`
}

type grypeCVSS struct {
	Version string `json:"version"`
	Vector  string `json:"vector"`
	Metrics struct {
		BaseScore float64 `json:"baseScore"`
	} `json:"metrics"`
}

// grypeLogPrefix is the time and level Grype starts a log line with: "[0002] ERROR ".
var grypeLogPrefix = regexp.MustCompile(`^\[[0-9]+\]\s+[A-Z]+\s+`)

// Grype's package types that are the image's OS packages.
var grypeOSTypes = []string{"apk", "deb", "rpm"}

// parseGrypeReport reads one image's Grype JSON into img (its Image, Ref and Pods kept).
func parseGrypeReport(data []byte, img scannedImage) (scannedImage, error) {
	var gr grypeReport
	if err := json.Unmarshal(data, &gr); err != nil {
		return img, fmt.Errorf("unreadable Grype report: %w", err)
	}

	if gr.Source == nil {
		return img, fmt.Errorf("unreadable Grype report: no source")
	}

	distro := strings.TrimSpace(gr.Distro.Name + " " + gr.Distro.Version)
	img.OS = distro

	if img.Digest == "" && len(gr.Source.Target.RepoDigests) > 0 {
		_, img.Digest, _ = strings.Cut(gr.Source.Target.RepoDigests[0], "@")
	}

	vulns := make([]imageVuln, 0, len(gr.Matches))
	for _, m := range gr.Matches {
		vulns = append(vulns, grypeVulnOf(m, distro))
	}

	return withVulns(img, vulns), nil
}

func grypeVulnOf(m grypeMatch, distro string) imageVuln {
	v, a := m.Vulnerability, m.Artifact

	class, target := "lang-pkgs", ""
	if slices.Contains(grypeOSTypes, a.Type) {
		class, target = vulnClassOS, distro
	} else if len(a.Locations) > 0 {
		target = strings.TrimPrefix(a.Locations[0].Path, "/")
	}

	// A distro's advisory often has no description nor score: NVD's related record has them.
	related := append([]grypeVuln{v}, m.RelatedVulnerability...)
	description := ""

	for _, r := range related {
		if description = strings.TrimSpace(r.Description); description != "" {
			break
		}
	}

	score, vector := grypeCVSSOf(related)

	return imageVuln{
		ID: v.ID, Package: a.Name, Installed: a.Version, Fixed: strings.Join(v.Fix.Versions, ", "),
		Severity: grypeSeverity(v.Severity), Title: grypeTitle(description), Description: description,
		URL: cmpOr(v.DataSource, firstOf(v.URLs)), Score: score, Vector: vector, PURL: a.PURL,
		Target: target, Class: class, Type: a.Type,
	}
}

// grypeCVSSOf picks one base score: the first record's with one, v3 before v4 and v2.
func grypeCVSSOf(records []grypeVuln) (float64, string) {
	for _, prefix := range []string{"3", "4", "2"} {
		for _, r := range records {
			for _, c := range r.CVSS {
				if strings.HasPrefix(c.Version, prefix) && c.Metrics.BaseScore > 0 {
					return c.Metrics.BaseScore, c.Vector
				}
			}
		}
	}

	return 0, ""
}

// grypeSeverity maps Grype's severities ("Critical", "Negligible") onto vulnSeverities.
func grypeSeverity(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), "negligible") {
		return "LOW"
	}

	return normalSeverity(s)
}

// grypeTitle is a description's first sentence, short: Grype has no title of its own.
func grypeTitle(description string) string {
	line, _, _ := strings.Cut(description, "\n")
	if i := strings.Index(line, ". "); i > 0 {
		line = line[:i+1]
	}

	if r := []rune(line); len(r) > 120 {
		line = string(r[:120]) + "…"
	}

	return line
}

func firstOf(s []string) string {
	if len(s) == 0 {
		return ""
	}

	return s[0]
}
