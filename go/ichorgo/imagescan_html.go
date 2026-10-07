package ichorgo

import (
	"bytes"
	"html/template"
	"strconv"
	"strings"
	"time"
)

// htmlReport is a self-contained page (no script, no remote asset) to mail or print to PDF:
// a summary per image, then its findings, most severe first.
func htmlReport(report imageScanReport) (string, error) {
	var buf bytes.Buffer

	if err := imageScanHTML.Execute(&buf, report); err != nil {
		return "", err
	}

	return buf.String(), nil
}

var imageScanHTML = template.Must(template.New("report").Funcs(template.FuncMap{
	"time": func(ms int64) string {
		if ms == 0 {
			return ""
		}

		return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04 UTC")
	},
	"lower": strings.ToLower,
	"score": func(f float64) string {
		if f == 0 {
			return ""
		}

		return strconv.FormatFloat(f, 'f', 1, 64)
	},
	"total": func(s vulnSummary) int { return s.Critical + s.High + s.Medium + s.Low + s.Unknown },
	// chip is a severity chip's class: its colour, or plain when there is none.
	"chip": func(class string, n int) string {
		if n == 0 {
			return "plain"
		}

		return class
	},
	"overall": overallSummary,
}).Parse(imageScanHTMLSource))

// overallSummary adds up the images' summaries, for the page's header.
func overallSummary(images []scannedImage) vulnSummary {
	var s vulnSummary

	for _, img := range images {
		s.Critical += img.Summary.Critical
		s.High += img.Summary.High
		s.Medium += img.Summary.Medium
		s.Low += img.Summary.Low
		s.Unknown += img.Summary.Unknown
		s.Fixable += img.Summary.Fixable
		s.OS += img.Summary.OS
	}

	return s
}

const imageScanHTMLSource = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Image vulnerability report</title>
<style>
:root { --fg:#1c1b1f; --muted:#5f5d66; --line:#dcd9e0; --bg:#fff;
  --critical:#b3261e; --high:#d9480f; --medium:#b58105; --low:#2f6fb3; --unknown:#6b6b6b; }
@media (prefers-color-scheme: dark) { :root { --fg:#e6e1e5; --muted:#a8a4ad; --line:#3a3840; --bg:#141218; } }
body { font: 14px/1.45 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; color:var(--fg); background:var(--bg); margin:24px; }
h1 { font-size:20px; margin:0 0 4px; } h2 { font-size:16px; margin:28px 0 4px; word-break:break-all; }
.meta { color:var(--muted); font-size:12px; }
.chips { display:flex; flex-wrap:wrap; gap:6px; margin:8px 0; }
.chip { border-radius:12px; padding:2px 10px; color:#fff; font-weight:600; font-size:12px; }
.critical { background:var(--critical); } .high { background:var(--high); } .medium { background:var(--medium); }
.low { background:var(--low); } .unknown { background:var(--unknown); }
.plain { background:none; color:var(--muted); border:1px solid var(--line); }
.error { color:var(--critical); }
table { border-collapse:collapse; width:100%; margin-top:8px; font-size:12px; }
th, td { text-align:left; padding:5px 6px; border-bottom:1px solid var(--line); vertical-align:top; }
th { color:var(--muted); font-weight:600; }
td.sev span { border-radius:4px; padding:1px 6px; color:#fff; font-size:11px; font-weight:600; }
a { color:inherit; }
.title { color:var(--muted); }
@media print { body { margin:0; } h2 { break-before:auto; } tr { break-inside:avoid; } }
</style>
</head>
<body>
<h1>Image vulnerability report</h1>
<div class="meta">{{.Scanner}} · {{time .Finished}} · {{len .Images}} image(s)</div>
{{template "chips" overall .Images}}
{{range .Images}}
<h2>{{.Image}}</h2>
<div class="meta">{{if .Digest}}{{.Digest}}{{end}}{{if .OS}} · {{.OS}}{{end}}{{if .ScannedAt}} · scanned {{time .ScannedAt}}{{end}}</div>
{{if .Pods}}<div class="meta">Pods: {{range $i, $p := .Pods}}{{if $i}}, {{end}}{{$p}}{{end}}</div>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{else}}
{{template "chips" .Summary}}
{{if .Summary.OS}}<div class="meta">{{.Summary.OS}} in the OS packages{{if .OS}} ({{.OS}}){{end}}: a newer base image fixes those it has a fix for.</div>{{end}}
{{if .Vulnerabilities}}
<table>
<thead><tr><th>Severity</th><th>Vulnerability</th><th>Package</th><th>Installed</th><th>Fixed</th><th>Score</th></tr></thead>
<tbody>
{{range .Vulnerabilities}}<tr>
<td class="sev"><span class="{{lower .Severity}}">{{.Severity}}</span></td>
<td>{{if .URL}}<a href="{{.URL}}">{{.ID}}</a>{{else}}{{.ID}}{{end}}{{if .Title}}<div class="title">{{.Title}}</div>{{end}}</td>
<td>{{.Package}}{{if .Target}}<div class="title">{{.Target}}</div>{{end}}</td>
<td>{{.Installed}}</td>
<td>{{.Fixed}}</td>
<td>{{score .Score}}</td>
</tr>
{{end}}</tbody>
</table>
{{else}}<p>No known vulnerability.</p>{{end}}
{{end}}
{{end}}
</body>
</html>
{{define "chips"}}<div class="chips">
<span class="chip {{chip "critical" .Critical}}">Critical {{.Critical}}</span>
<span class="chip {{chip "high" .High}}">High {{.High}}</span>
<span class="chip {{chip "medium" .Medium}}">Medium {{.Medium}}</span>
<span class="chip {{chip "low" .Low}}">Low {{.Low}}</span>
{{if .Unknown}}<span class="chip unknown">Unknown {{.Unknown}}</span>{{end}}
<span class="chip plain">{{.Fixable}} of {{total .}} fixable</span>
</div>{{end}}
`
