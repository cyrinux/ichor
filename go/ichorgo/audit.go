package ichorgo

import (
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// The action audit log records every change the app makes to a cluster (a reboot, a scale,
// a sync...): when, on which cluster and object, with what, and how it ended. Each mutating
// exported function records itself (audit_api_test.go checks it), whatever the outcome, so
// a refused or failed attempt shows too. Entries are kept in a file of the app's data
// directory (see SetDataDir), encrypted like the node names, for auditRetention and at most
// auditMaxEntries; without a data directory nothing is recorded. Secrets are never stored:
// the parameters are summaries built here, and both they and the errors go through
// redactSecrets.

const (
	auditFile       = "audit-log.json"
	auditRetention  = 90 * 24 * time.Hour
	auditMaxEntries = 2000

	auditOK     = "ok"
	auditFailed = "failed"
)

// auditEntry is one recorded action. At is unix ms; Object is "Kind/name".
type auditEntry struct {
	At        int64  `json:"at"`
	Cluster   string `json:"cluster"`
	Node      string `json:"node,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Object    string `json:"object,omitempty"`
	Action    string `json:"action"`
	Params    string `json:"params,omitempty"`
	Outcome   string `json:"outcome"`
	Error     string `json:"error,omitempty"`
	Demo      bool   `json:"demo,omitempty"`
}

// auditAction is what an entry point says about the action it runs.
type auditAction struct {
	Action    string
	Node      string
	Namespace string
	Object    string
	Params    string
}

var (
	auditMu  sync.Mutex
	auditNow = time.Now
)

// recordAction records the action the calling exported function just ran, with its outcome
// (use with defer, after the arguments are unmasked and after defer maskErr, so the error is
// still the real one). Like maskErr it turns a panic into an error, recorded as a failure.
func recordAction(err *error, configYAML, contextName string, a auditAction) {
	if r := recover(); r != nil {
		*err = panicError(r)
	}

	recordOutcome(configYAML, contextName, a, *err)
}

// recordOutcome records an action that ended with err (nil: it succeeded); for the runs
// that end in the background. Best effort: a log that cannot be written loses the entry.
func recordOutcome(configYAML, contextName string, a auditAction, err error) {
	e := auditEntry{
		At:        auditNow().UnixMilli(),
		Cluster:   contextName,
		Node:      a.Node,
		Namespace: a.Namespace,
		Object:    a.Object,
		Action:    a.Action,
		Params:    redactSecrets(a.Params),
		Outcome:   auditOK,
		Demo:      configYAML != "" && isDemoContext(configYAML, contextName),
	}

	if err != nil {
		e.Outcome, e.Error = auditFailed, redactSecrets(err.Error())
	}

	auditMu.Lock()
	defer auditMu.Unlock()

	if _, aead := dataStore(); aead == nil {
		return
	}

	_ = writeAuditLocked(append(readAuditLocked(), e))
}

// AuditLog lists the recorded actions, newest first, as a JSON array of {at (unix ms),
// cluster, node, namespace, object, action, params, outcome ("ok" or "failed"), error, demo}.
// cluster and action filter it when not empty.
func AuditLog(cluster, action string) (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(auditEntries(privacy.unmaskContext(strings.TrimSpace(cluster)), strings.TrimSpace(action)))
}

// AuditExport is the log of cluster ("" for every cluster) to share: format "json" (as
// AuditLog) or "markdown" (a table).
func AuditExport(cluster, format string) (out string, err error) {
	defer maskResult(&out, &err)

	entries := auditEntries(privacy.unmaskContext(strings.TrimSpace(cluster)), "")

	switch format {
	case "json":
		b, err := json.MarshalIndent(entries, "", "  ")

		return string(b), err
	case "markdown":
		return auditMarkdown(entries), nil
	default:
		return "", fmt.Errorf("unknown export format %q (json, markdown)", format)
	}
}

// AuditClear forgets the recorded actions of cluster, or all of them when cluster is "".
func AuditClear(cluster string) (err error) {
	defer maskErr(&err)

	cluster = privacy.unmaskContext(strings.TrimSpace(cluster))

	auditMu.Lock()
	defer auditMu.Unlock()

	kept := slices.DeleteFunc(readAuditLocked(), func(e auditEntry) bool {
		return cluster == "" || e.Cluster == cluster
	})

	return writeAuditLocked(kept)
}

// auditEntries is the log filtered by cluster and action ("" matches all), newest first.
func auditEntries(cluster, action string) []auditEntry {
	auditMu.Lock()
	all := readAuditLocked()
	auditMu.Unlock()

	out := make([]auditEntry, 0, len(all))

	for i := len(all) - 1; i >= 0; i-- {
		e := all[i]
		if (cluster == "" || e.Cluster == cluster) && (action == "" || e.Action == action) {
			out = append(out, e)
		}
	}

	return out
}

// readAuditLocked is the stored log, oldest first, without the expired entries. A missing
// file or one that does not decrypt (the key was reset) is an empty log.
func readAuditLocked() []auditEntry {
	dir, aead := dataStore()
	if aead == nil {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(dir, auditFile))
	if err != nil {
		return nil
	}

	plain, err := openSealed(aead, data)
	if err != nil {
		return nil
	}

	var entries []auditEntry
	if json.Unmarshal(plain, &entries) != nil {
		return nil
	}

	cutoff := auditNow().Add(-auditRetention).UnixMilli()

	return slices.DeleteFunc(entries, func(e auditEntry) bool { return e.At < cutoff })
}

// writeAuditLocked replaces the stored log with the newest auditMaxEntries of entries,
// atomically.
func writeAuditLocked(entries []auditEntry) error {
	dir, aead := dataStore()
	if aead == nil {
		return nil
	}

	if len(entries) > auditMaxEntries {
		entries = entries[len(entries)-auditMaxEntries:]
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}

	return writeSealed(filepath.Join(dir, auditFile), data, aead)
}

// writeSealed replaces path with data encrypted with aead, atomically, so a crash never
// leaves half of it.
func writeSealed(path string, data []byte, aead cipher.AEAD) error {
	sealed, err := seal(aead, data)
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

// secretPatterns match what could be a credential in a parameter summary or an error: a
// value after a secret-sounding key, a bearer token, a long base64 run.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(token|password|passwd|secret|api[-_]?key|private[-_]?key|key)(\s*[:=]\s*)\S+`),
	regexp.MustCompile(`(?i)\b(bearer|basic)\s+\S+`),
	regexp.MustCompile(`[A-Za-z0-9+/_=-]{40,}`),
}

// redactSecrets replaces anything in s that looks like a credential with "<redacted>".
func redactSecrets(s string) string {
	s = secretPatterns[0].ReplaceAllString(s, "$1$2<redacted>")
	s = secretPatterns[1].ReplaceAllString(s, "$1 <redacted>")

	return secretPatterns[2].ReplaceAllStringFunc(s, func(run string) string {
		if !looksEncoded(run) {
			return run
		}

		return "<redacted>"
	})
}

// looksEncoded tells a base64 run (a token, a key: one long mixed-case word) from a hex
// digest or Image Factory schematic ID (one case) or a path of names (short words), which
// say nothing secret.
func looksEncoded(run string) bool {
	words := strings.FieldsFunc(run, func(r rune) bool { return strings.ContainsRune("/-_.", r) })

	return slices.ContainsFunc(words, func(w string) bool {
		return len(w) >= 32 && strings.ToLower(w) != w && strings.ToUpper(w) != w
	})
}

// auditMarkdown is entries as a Markdown table, times in UTC.
func auditMarkdown(entries []auditEntry) string {
	var b strings.Builder

	b.WriteString("# Ichor activity\n\n")
	b.WriteString("| Time (UTC) | Cluster | Target | Action | Parameters | Outcome |\n")
	b.WriteString("|---|---|---|---|---|---|\n")

	for _, e := range entries {
		outcome := e.Outcome
		if e.Error != "" {
			outcome += ": " + e.Error
		}

		cluster := e.Cluster
		if e.Demo {
			cluster += " (demo)"
		}

		cells := []string{
			time.UnixMilli(e.At).UTC().Format("2006-01-02 15:04:05Z"),
			cluster, auditTarget(e), e.Action, e.Params, outcome,
		}

		for i, c := range cells {
			cells[i] = markdownCell(c)
		}

		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}

	return b.String()
}

// auditTarget is what an entry acted on: "namespace/Kind/name", the object, or the node.
func auditTarget(e auditEntry) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{e.Node, e.Namespace, e.Object} {
		if p != "" {
			parts = append(parts, p)
		}
	}

	return strings.Join(parts, "/")
}

func markdownCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)

	return strings.Join(strings.Fields(s), " ")
}

// auditVerb is yes when on, else no: the action of a toggle (cordon or uncordon...).
func auditVerb(on bool, yes, no string) string {
	if on {
		return yes
	}

	return no
}
