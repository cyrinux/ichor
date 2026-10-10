package main

import (
	"encoding/json"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// imageScanRun scans image refs with no pod behind them; Ctrl-C cancels it, the scan Job is
// still deleted. The progress goes to stderr like a network test's. With -data-dir the
// report is kept, as app "probe" (scan-history).
func imageScanRun(cfg, contextName, kubeServer string, refs []string) string {
	options, err := json.Marshal(map[string]any{"images": refs, "app": "probe"})
	if err != nil {
		return err.Error()
	}

	p := &netPerfProbe{done: make(chan string, 1)}
	run := ichorgo.StartImageScan(cfg, contextName, kubeServer, "[]", string(options), p)

	return p.wait(run.Cancel)
}
