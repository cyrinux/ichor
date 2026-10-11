package main

import (
	"encoding/json"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// imageScanRun scans image refs with no pod behind them; Ctrl-C cancels it, the scan Job is
// still deleted. The progress goes to stderr like a network test's. With -data-dir the
// report is kept, as app "probe" (scan-history). "-scanner grype" among the args (or
// PROBE_SCANNER=grype) runs Grype instead of Trivy.
func imageScanRun(cfg, contextName, kubeServer string, args []string) string {
	refs, scanner := scannerArg(args, os.Getenv("PROBE_SCANNER"))

	options, err := json.Marshal(map[string]any{"images": refs, "app": "probe", "scanner": scanner})
	if err != nil {
		return err.Error()
	}

	p := &netPerfProbe{done: make(chan string, 1)}
	run := ichorgo.StartImageScan(cfg, contextName, kubeServer, "[]", string(options), p)

	return p.wait(run.Cancel)
}

// scannerArg takes "-scanner NAME" (or "--scanner NAME") out of args, anywhere among the refs;
// fallback when there is none.
func scannerArg(args []string, fallback string) (refs []string, scanner string) {
	scanner = fallback

	for i := 0; i < len(args); i++ {
		if (args[i] == "-scanner" || args[i] == "--scanner") && i+1 < len(args) {
			scanner = args[i+1]
			i++

			continue
		}

		refs = append(refs, args[i])
	}

	return refs, scanner
}
