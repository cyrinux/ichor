package ichorgo

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The exports follow the formats Trivy itself writes, so the tools that import Trivy's
// SARIF (GitHub code scanning, Defect Dojo) and CycloneDX (Dependency-Track) take them.

// severityScore stands in for a missing CVSS score in SARIF's security-severity, which
// GitHub maps back to the same severity.
var severityScore = map[string]float64{"CRITICAL": 9.5, "HIGH": 8.0, "MEDIUM": 5.5, "LOW": 2.0}

const sarifSchema = "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json"

func sarifReport(report imageScanReport) map[string]any {
	var (
		rules   []map[string]any
		results []map[string]any
	)

	ruleIndex := map[string]int{}

	for _, img := range report.Images {
		for _, v := range img.Vulnerabilities {
			i, ok := ruleIndex[v.ID]
			if !ok {
				i = len(rules)
				ruleIndex[v.ID] = i
				rules = append(rules, sarifRule(v))
			}

			results = append(results, sarifResult(img, v, i))
		}
	}

	return map[string]any{
		"version": "2.1.0",
		"$schema": sarifSchema,
		"runs": []map[string]any{{
			"tool": map[string]any{"driver": map[string]any{
				"name": "Trivy", "fullName": report.Scanner, "informationUri": "https://github.com/aquasecurity/trivy",
				"version": trivyVersion, "rules": nonNil(rules),
			}},
			"results":            nonNil(results),
			"columnKind":         "utf16CodeUnits",
			"originalUriBaseIds": map[string]any{"ROOTPATH": map[string]string{"uri": "file:///"}},
			"automationDetails":  map[string]string{"id": "ichor/image-scan/" + strconv.FormatInt(report.Finished, 10)},
			"properties":         map[string]any{"source": report.Source},
			"invocations":        []map[string]any{{"executionSuccessful": true, "endTimeUtc": time.UnixMilli(report.Finished).UTC().Format(time.RFC3339)}},
		}},
	}
}

func vulnScore(v imageVuln) float64 {
	if v.Score > 0 {
		return v.Score
	}

	return severityScore[v.Severity]
}

func sarifRule(v imageVuln) map[string]any {
	short := cmp.Or(v.Title, v.ID)

	return map[string]any{
		"id":                   v.ID,
		"name":                 sarifRuleName(v),
		"shortDescription":     map[string]string{"text": short},
		"fullDescription":      map[string]string{"text": cmp.Or(v.Description, short)},
		"defaultConfiguration": map[string]string{"level": sarifLevel(v.Severity)},
		"helpUri":              v.URL,
		"help": map[string]string{
			"text":     fmt.Sprintf("Vulnerability %s\nSeverity: %s\nPackage: %s\nFixed Version: %s\nLink: %s\n%s", v.ID, v.Severity, v.Package, v.Fixed, v.URL, v.Description),
			"markdown": fmt.Sprintf("**Vulnerability %s**\n| Severity | Package | Fixed Version | Link |\n| --- | --- | --- | --- |\n|%s|%s|%s|[%s](%s)|\n\n%s", v.ID, v.Severity, v.Package, v.Fixed, v.ID, v.URL, v.Description),
		},
		"properties": map[string]any{
			"precision":         "very-high",
			"security-severity": strconv.FormatFloat(vulnScore(v), 'f', 1, 64),
			"tags":              []string{"vulnerability", "security", v.Severity},
		},
	}
}

func sarifRuleName(v imageVuln) string {
	if v.Class == vulnClassOS {
		return "OsPackageVulnerability"
	}

	return "LanguageSpecificPackageVulnerability"
}

func sarifLevel(severity string) string {
	switch severity {
	case "CRITICAL", "HIGH":
		return "error"
	case "MEDIUM":
		return "warning"
	default:
		return "note"
	}
}

// sarifResult places a finding in the image: its repository path, then the file that
// brought it for a library (Trivy's own layout).
func sarifResult(img scannedImage, v imageVuln, rule int) map[string]any {
	uri := parseImageRef(img.Image).Path
	if v.Class != vulnClassOS && v.Target != "" {
		uri += "/" + strings.TrimPrefix(v.Target, "/")
	}

	return map[string]any{
		"ruleId":    v.ID,
		"ruleIndex": rule,
		"level":     sarifLevel(v.Severity),
		"message": map[string]string{"text": fmt.Sprintf("Package: %s\nInstalled Version: %s\nVulnerability %s\nSeverity: %s\nFixed Version: %s\nLink: [%s](%s)",
			v.Package, v.Installed, v.ID, v.Severity, v.Fixed, v.ID, v.URL)},
		"locations": []map[string]any{{
			"physicalLocation": map[string]any{
				"artifactLocation": map[string]string{"uri": uri, "uriBaseId": "ROOTPATH"},
				"region":           map[string]int{"startLine": 1, "startColumn": 1, "endLine": 1, "endColumn": 1},
			},
			"message":          map[string]string{"text": img.Ref + ": " + v.Package + "@" + v.Installed},
			"logicalLocations": []map[string]string{{"fullyQualifiedName": img.Ref, "kind": "container-image"}},
		}},
	}
}

// cycloneDXReport is a CycloneDX 1.6 BOM: each image a container component with its
// vulnerable packages inside, and the vulnerabilities pointing at those packages.
func cycloneDXReport(report imageScanReport, now time.Time) map[string]any {
	var (
		components []map[string]any
		vulns      []map[string]any
	)

	vulnIndex := map[string]int{}

	for i, img := range report.Images {
		imageRef := fmt.Sprintf("image-%d", i)
		packages := []map[string]any{}
		pkgRefs := map[string]bool{}

		for _, v := range img.Vulnerabilities {
			pkgRef := imageRef + ":" + cmp.Or(v.PURL, v.Package+"@"+v.Installed)
			if !pkgRefs[pkgRef] {
				pkgRefs[pkgRef] = true
				packages = append(packages, cycloneDXPackage(pkgRef, v))
			}

			j, ok := vulnIndex[v.ID]
			if !ok {
				j = len(vulns)
				vulnIndex[v.ID] = j
				vulns = append(vulns, cycloneDXVuln(v))
			}

			vulns[j]["affects"] = append(vulns[j]["affects"].([]map[string]any), map[string]any{"ref": pkgRef})
		}

		components = append(components, cycloneDXImage(imageRef, img, packages))
	}

	return map[string]any{
		"$schema":      "http://cyclonedx.org/schema/bom-1.6.schema.json",
		"bomFormat":    "CycloneDX",
		"specVersion":  "1.6",
		"serialNumber": "urn:uuid:" + uuid4(),
		"version":      1,
		"metadata": map[string]any{
			"timestamp": now.UTC().Format(time.RFC3339),
			"tools": map[string]any{"components": []map[string]any{
				{"type": "application", "group": "aquasecurity", "name": "trivy", "version": trivyVersion},
				{"type": "application", "name": "ichor"},
			}},
			"component": map[string]any{"bom-ref": "scan", "type": "application", "name": "image scan " + report.Scanner},
		},
		"components":      nonNil(components),
		"vulnerabilities": nonNil(vulns),
	}
}

func cycloneDXImage(ref string, img scannedImage, packages []map[string]any) map[string]any {
	parsed := parseImageRef(img.Image)
	c := map[string]any{
		"bom-ref": ref, "type": "container", "name": parsed.Repo(), "version": cmp.Or(img.Digest, parsed.Tag),
		"components": packages,
	}

	if img.Digest != "" {
		c["purl"] = "pkg:oci/" + parsed.Name() + "@" + strings.Replace(img.Digest, ":", "%3A", 1) +
			"?repository_url=" + parsed.Repo()
	}

	props := []map[string]string{{"name": "ichor:image", "value": img.Image}}
	if img.OS != "" {
		props = append(props, map[string]string{"name": "ichor:os", "value": img.OS})
	}

	for _, p := range img.Pods {
		props = append(props, map[string]string{"name": "ichor:pod", "value": p})
	}

	c["properties"] = props

	return c
}

func cycloneDXPackage(ref string, v imageVuln) map[string]any {
	p := map[string]any{"bom-ref": ref, "type": "library", "name": v.Package, "version": v.Installed}
	if v.PURL != "" {
		p["purl"] = v.PURL
	}

	if v.Target != "" {
		p["properties"] = []map[string]string{{"name": "aquasecurity:trivy:Target", "value": v.Target}}
	}

	return p
}

func cycloneDXVuln(v imageVuln) map[string]any {
	rating := map[string]any{"severity": strings.ToLower(v.Severity)}
	if v.Score > 0 {
		rating["score"] = v.Score
		if m := cvssMethod(v.Vector); m != "" {
			rating["method"] = m
		}

		if v.Vector != "" {
			rating["vector"] = v.Vector
		}
	}

	out := map[string]any{
		"id":      v.ID,
		"source":  map[string]string{"name": vulnSource(v.ID), "url": v.URL},
		"ratings": []map[string]any{rating},
		"affects": []map[string]any{},
	}

	if v.Description != "" {
		out["description"] = v.Description
	}

	if v.Title != "" {
		out["detail"] = v.Title
	}

	if v.Fixed != "" {
		out["recommendation"] = "Upgrade " + v.Package + " to version " + v.Fixed
	}

	if v.URL != "" {
		out["advisories"] = []map[string]string{{"url": v.URL}}
	}

	if v.Published != "" {
		out["published"] = v.Published
	}

	return out
}

func cvssMethod(vector string) string {
	switch {
	case strings.HasPrefix(vector, "CVSS:4"):
		return "CVSSv4"
	case strings.HasPrefix(vector, "CVSS:3.1"):
		return "CVSSv31"
	case strings.HasPrefix(vector, "CVSS:3"):
		return "CVSSv3"
	case vector != "":
		return "CVSSv2"
	default:
		return ""
	}
}

func vulnSource(id string) string {
	switch {
	case strings.HasPrefix(id, "CVE-"):
		return "nvd"
	case strings.HasPrefix(id, "GHSA-"):
		return "ghsa"
	default:
		return strings.ToLower(strings.SplitN(id, "-", 2)[0])
	}
}

// uuid4 is a random RFC 4122 UUID (the BOM's serial number).
func uuid4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) //nolint:errcheck // crypto/rand.Read never fails

	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

var csvHeader = []string{"image", "digest", "os", "severity", "id", "package", "installed", "fixed", "score", "title", "target", "url"}

func csvReport(report imageScanReport) (string, error) {
	var buf bytes.Buffer

	w := csv.NewWriter(&buf)
	rows := [][]string{csvHeader}

	for _, img := range report.Images {
		for _, v := range img.Vulnerabilities {
			score := ""
			if v.Score > 0 {
				score = strconv.FormatFloat(v.Score, 'f', 1, 64)
			}

			row := []string{img.Image, img.Digest, img.OS, v.Severity, v.ID, v.Package, v.Installed, v.Fixed, score, v.Title, v.Target, v.URL}
			for i := range row {
				row[i] = csvSafe(row[i])
			}

			rows = append(rows, row)
		}
	}

	if err := w.WriteAll(rows); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// csvSafe keeps a spreadsheet from running a cell as a formula (leading blanks included).
func csvSafe(s string) string {
	if t := strings.TrimLeft(s, " \t\r\n"); t != "" && strings.ContainsRune("=+-@\t\r", rune(t[0])) {
		return "'" + s
	}

	return s
}
