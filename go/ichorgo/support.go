package ichorgo

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	supportTimeout     = 15 * time.Minute
	supportStepTimeout = 90 * time.Second
)

// SupportListener follows a support bundle (implemented in Kotlin/Swift).
type SupportListener interface {
	// OnProgress gets {"node","step","done","total"} when a step starts (node is "" for the
	// cluster-wide steps). done and total count that node's own steps; each node ends with
	// a "done" step where done == total, the cluster-wide one once everything is written.
	OnProgress(json string)
	// OnDone is called exactly once; errMessage is empty on success.
	OnDone(path string, size int64, errMessage string)
}

// SupportRun is a handle on a support bundle being collected.
type SupportRun struct {
	cancel context.CancelFunc
}

// Cancel aborts the collection; the partial file is removed and OnDone reports the error.
func (r *SupportRun) Cancel() { r.cancel() }

type supportProgress struct {
	Node  string `json:"node"`
	Step  string `json:"step"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

// StartSupportBundle collects a debugging bundle for nodes (comma-separated, "" = every node
// of the context) into the zip file destPath, in the spirit of `talosctl support`: per node
// the kernel log, service logs, kube-system container logs, every non-sensitive resource as
// YAML, the machine config with its secrets redacted, mounts and processes, plus the
// cluster's etcd status. os:reader collects most of it; the machine config needs os:admin
// and the etcd status os:operator (a step that fails leaves a NAME.error.txt in the bundle
// instead of failing it). The file is written to destPath.part and renamed when complete.
//
// The bundle is for debugging and is NOT masked by the screenshot mode: it holds real
// addresses and host names. It never contains the talosconfig, and the machine config in it
// has no secrets; logs are copied as they are.
//
// kubeServer: see CollectDiagnosis; with os:admin the bundle holds the Argo CD and Flux state
// (cluster/gitops.json, repository credentials removed).
func StartSupportBundle(configYAML, contextName, kubeServer, nodes, destPath string, listener SupportListener) *SupportRun {
	contextName, nodes = unmaskTargets(configYAML, contextName, nodes)

	listener = maskedSupportListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), supportTimeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", 0, msg) })

		size, err := runSupportBundle(ctx, configYAML, contextName, kubeServer, nodes, destPath, listener)
		if err != nil {
			listener.OnDone("", 0, err.Error())

			return
		}

		listener.OnDone(destPath, size, "")
	}()

	return &SupportRun{cancel: cancel}
}

func runSupportBundle(ctx context.Context, configYAML, contextName, kubeServer, nodes, destPath string, listener SupportListener) (int64, error) {
	s, release, err := sessions.acquire(configYAML, contextName)
	if err != nil {
		return 0, err
	}

	defer release()

	targets, err := supportTargets(targetNodes(s.context), nodes)
	if err != nil {
		return 0, err
	}

	sections := make([]bundleSection, 0, len(targets)+1)
	for _, node := range targets {
		sections = append(sections, nodeBundleSection(s, node))
	}

	sections = append(sections, clusterBundleSection(s, kubeTarget{configYAML, contextName, kubeServer}))

	progress := func(p supportProgress) { emitJSON(p, listener.OnProgress) }

	return writeBundle(ctx, destPath, sections, progress, time.Now())
}

// supportTargets resolves the comma-separated nodes ("" = all) against the context's nodes.
func supportTargets(contextNodes []string, nodes string) ([]string, error) {
	var out []string

	for _, n := range splitCSV(nodes) {
		if !slices.Contains(contextNodes, n) {
			return nil, fmt.Errorf("node %q is not part of this context", n)
		}

		out = append(out, n)
	}

	if len(out) == 0 {
		out = contextNodes
	}

	if len(out) == 0 {
		return nil, errors.New("no node to collect from")
	}

	return out, nil
}

// bundleSection is one directory of the bundle: a node, or the cluster ("" node).
type bundleSection struct {
	node string
	dir  string
	// reachable is checked first: when it fails the steps are skipped (each would only time
	// out) and the reason is written instead. nil: no check.
	reachable func(context.Context) error
	steps     []bundleStep
}

// bundleStep collects some files. An error does not stop the bundle: it is written next to
// the files the step could still produce.
type bundleStep struct {
	name string
	run  func(context.Context) ([]bundleFile, error)
}

type bundleFile struct {
	name    string // path inside the section's directory
	content []byte
}

// writeBundle runs every step and writes the zip atomically (destPath.part, then rename).
// It fails, leaving no file, only when ctx ends or the file cannot be written.
func writeBundle(ctx context.Context, destPath string, sections []bundleSection, progress func(supportProgress), now time.Time) (int64, error) {
	if !filepath.IsAbs(destPath) {
		return 0, fmt.Errorf("destination %q is not an absolute path", destPath)
	}

	part := destPath + ".part"

	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}

	fail := func(err error) (int64, error) {
		_ = f.Close()       //nolint:errcheck
		_ = os.Remove(part) //nolint:errcheck

		return 0, err
	}

	zw := zip.NewWriter(f)
	w := &bundleWriter{zip: zw, now: now}

	// The cluster-wide section's closing report waits until the archive is written.
	clusterTotal := 0

	for _, sec := range sections {
		skip := ""
		total := len(sec.steps)

		if sec.reachable != nil {
			if err := runBundleCheck(ctx, sec.reachable); err != nil {
				skip = err.Error()
				w.failures = append(w.failures, sec.dir+": skipped, "+skip)

				if err := w.add(sec.dir+"/unreachable.txt", []byte(skip+"\n")); err != nil {
					return fail(err)
				}
			}
		}

		for done, step := range sec.steps {
			if ctx.Err() != nil {
				return fail(bundleStopped(ctx))
			}

			progress(supportProgress{Node: sec.node, Step: step.name, Done: done, Total: total})

			if skip == "" {
				if err := w.runStep(ctx, sec.dir, step); err != nil {
					return fail(err)
				}
			}
		}

		if sec.node == "" {
			clusterTotal += total

			continue
		}

		progress(supportProgress{Node: sec.node, Step: "done", Done: total, Total: total})
	}

	if ctx.Err() != nil {
		return fail(bundleStopped(ctx))
	}

	if err := w.add("summary.txt", w.summary(sections)); err != nil {
		return fail(err)
	}

	if err := zw.Close(); err != nil {
		return fail(err)
	}

	if err := f.Sync(); err != nil {
		return fail(err)
	}

	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(part) //nolint:errcheck

		return 0, err
	}

	if err := os.Rename(part, destPath); err != nil {
		_ = os.Remove(part) //nolint:errcheck

		return 0, err
	}

	progress(supportProgress{Step: "done", Done: clusterTotal, Total: clusterTotal})

	return info.Size(), nil
}

func bundleStopped(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("the support bundle was not complete after %s", supportTimeout)
	}

	return errors.New("cancelled")
}

func runBundleCheck(ctx context.Context, check func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	return check(ctx)
}

type bundleWriter struct {
	zip      *zip.Writer
	now      time.Time
	files    int
	failures []string
}

func (w *bundleWriter) add(name string, content []byte) error {
	fw, err := w.zip.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: w.now})
	if err != nil {
		return err
	}

	w.files++

	_, err = fw.Write(content)

	return err
}

// runStep writes the step's files; a collection error becomes a NAME.error.txt file. The
// returned error is only a failure to write the zip.
func (w *bundleWriter) runStep(ctx context.Context, dir string, step bundleStep) error {
	stepCtx, cancel := context.WithTimeout(ctx, supportStepTimeout)
	defer cancel()

	files, err := step.run(stepCtx)

	for _, file := range files {
		if werr := w.add(dir+"/"+file.name, file.content); werr != nil {
			return werr
		}
	}

	if err == nil || ctx.Err() != nil {
		return nil
	}

	w.failures = append(w.failures, dir+"/"+step.name+": "+err.Error())

	return w.add(dir+"/"+bundleFileName(step.name)+".error.txt", []byte(err.Error()+"\n"))
}

func (w *bundleWriter) summary(sections []bundleSection) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "Ichor support bundle\ncollected: %s\nfiles: %d\n\n", w.now.UTC().Format(time.RFC3339), w.files)

	for _, sec := range sections {
		if sec.node != "" {
			fmt.Fprintf(&b, "node: %s\n", sec.node)
		}
	}

	b.WriteString("\nThe machine configs have their secrets redacted. Logs and resources are copied as they are.\n")

	if len(w.failures) == 0 {
		b.WriteString("\nEvery step succeeded.\n")
	} else {
		b.WriteString("\nSteps that failed (see the .error.txt files):\n")

		for _, failure := range w.failures {
			b.WriteString("  " + failure + "\n")
		}
	}

	return []byte(b.String())
}

// bundleFileName makes a node address, service or resource name safe as a zip path element.
func bundleFileName(name string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, name)

	if strings.Trim(out, "._") == "" {
		return "unnamed"
	}

	return out
}
