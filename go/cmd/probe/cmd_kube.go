package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// Kubernetes workloads and nodes.
var kubeCommands = []command{
	{name: "kubeconfig", args: "", run: func(e env) (out string, err error) {
		// Never print the credential itself.
		var kc string
		if kc, err = ichorgo.Kubeconfig(e.cfg, e.context, e.kubeServer); err == nil {
			out = fmt.Sprintf("kubeconfig: %d bytes, starts with %q", len(kc), firstLine(kc))
		}

		return out, err
	}},
	{name: "search", args: "QUERY", run: probeSearch},
	{name: "renew-signins", args: "", run: func(e env) (string, error) {
		// renew-signins: renews every signed-in context like the apps on foreground and before a
		// background check. The probe's sign-ins live for one run: a kubeconfig reports the
		// contexts it signs in itself, a talosconfig its Omni contexts.
		if ichorgo.IsKubeconfig(e.cfg) {
			return ichorgo.KubeRenewSignIns(e.cfg, "")
		}

		return ichorgo.KubeRenewSignIns("", e.cfg)
	}},
	{name: "discover", args: "PROVIDER SECRETS_JSON", noConfig: true, run: probeDiscover},
	{name: "workloads", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeWorkloads(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "storage", args: "[NAMESPACE]", run: func(e env) (out string, err error) {
		// storage [NAMESPACE]: every namespace when omitted.
		out, err = ichorgo.KubeStorage(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "kube-services", args: "[NAMESPACE]", run: func(e env) (out string, err error) {
		// kube-services [NAMESPACE]: every namespace when omitted.
		out, err = ichorgo.KubeServices(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "kube-loadbalancing", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeLoadBalancing(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "app-routes", args: "PODS_JSON", run: func(e env) (out string, err error) {
		// app-routes '[{"namespace":"shop","pod":"web-1"}]': the URLs those pods are served at.
		out, err = ichorgo.KubeAppRoutes(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "jobs", args: "[NAMESPACE]", run: func(e env) (out string, err error) {
		// jobs [NAMESPACE]: every namespace when omitted.
		out, err = ichorgo.KubeJobs(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "top-nodes", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeTopNodes(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "capacity", args: "", run: func(e env) (out string, err error) {
		// capacity: free room per node, workloads by priority with their QoS and OOM risk, N+1.
		out, err = ichorgo.KubeCapacity(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "node-disk", args: "", run: func(e env) (out string, err error) {
		// node-disk: each ready node's nodefs/imagefs fill and DiskPressure, as the monitor reads it.
		out, err = ichorgo.KubeNodeDisk(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "node-loss", args: "[NODE[,NODE...]]", run: func(e env) (out string, err error) {
		// node-loss [NODE[,NODE...]]: what losing the nodes costs; the worst node when omitted.
		out, err = ichorgo.KubeCapacityNodeLoss(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "top-pods", args: "[NAMESPACE [SELECTOR]]", run: func(e env) (out string, err error) {
		// top-pods [NAMESPACE [SELECTOR]]: every namespace and pod when omitted.
		out, err = ichorgo.KubeTopPods(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "top-pod", args: "NAMESPACE POD", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeTopPod(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "rollout-restart", args: "KIND NAMESPACE NAME", run: func(e env) (out string, err error) {
		// rollout-restart KIND NAMESPACE NAME
		if err = ichorgo.KubeRolloutRestart(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3)); err == nil {
			out = "restarted"
		}

		return out, err
	}},
	{name: "rollout-status", args: "KIND NAMESPACE NAME", run: func(e env) (out string, err error) {
		// rollout-status KIND NAMESPACE NAME
		out, err = ichorgo.KubeRolloutStatus(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))

		return out, err
	}},
	{name: "cronjobs", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeCronJobs(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "cronjob-run", args: "NAMESPACE NAME", run: func(e env) (out string, err error) {
		// cronjob-run NAMESPACE NAME: creates a Job from the CronJob's template.
		out, err = ichorgo.KubeTriggerCronJob(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "pods", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubePods(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "pods-page", args: "NAMESPACE|all LIMIT [TOKEN]", run: func(e env) (out string, err error) {
		// pods-page NAMESPACE|all LIMIT [TOKEN]: one page as a Table, as the apps load it.
		out, err = podsPage(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))

		return out, err
	}},
	{name: "node-pods", args: "KUBENODE [PHASE]", run: func(e env) (out string, err error) {
		// node-pods KUBENODE [PHASE|!PHASE]: the first page of the pods on a node, as a Table.
		out, err = ichorgo.KubeNodePodsPage(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), "", 0, true)

		return out, err
	}},
	{name: "kube-node-name", args: "NODE", run: func(e env) (out string, err error) {
		// kube-node-name NODE: the Kubernetes name of a Talos node, what node-pods takes.
		out, err = ichorgo.KubeNodeName(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "workload-pods", args: "KIND NAMESPACE NAME [PHASE]", run: func(e env) (out string, err error) {
		// workload-pods KIND NAMESPACE NAME [PHASE|!PHASE]: the first page of its pods, by its selector.
		out, err = ichorgo.KubeWorkloadPodsPage(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), "", 0, true)

		return out, err
	}},
	{name: "namespaces", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeNamespaces(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "apihealth", args: "", run: func(e env) (out string, err error) {
		// apihealth: the API server's health and load, with its progress on stderr (the
		// wait between the two scrapes counts its seconds).
		stop := watchReadProgress(e, "apihealth")
		out, err = ichorgo.KubeAPIHealth(e.cfg, e.context, e.kubeServer)
		stop()

		return out, err
	}},
	{name: "checkup", args: "[LOCALE]", run: func(e env) (out string, err error) {
		// checkup [LOCALE]: what no other screen shows (failing pods, full volumes, dead webhooks...),
		// the plugins' findings worded in LOCALE, with its progress on stderr every 500 ms while it reads.
		stop := watchReadProgress(e, "checkup")
		out, err = ichorgo.KubeCheckup(e.cfg, e.context, e.kubeServer, flag.Arg(1))
		stop()

		return out, err
	}},
	{name: "kube-events", args: "NAMESPACE [KIND] [NAME]", run: func(e env) (out string, err error) {
		// kube-events NAMESPACE [KIND] NAME: an object's events; without KIND, NAME's and what it owns.
		kind, name := flag.Arg(2), flag.Arg(3)
		if name == "" {
			kind, name = "", kind
		}

		out, err = ichorgo.KubeEvents(e.cfg, e.context, e.kubeServer, flag.Arg(1), kind, name)

		return out, err
	}},
	{name: "helm-rollback-plan", args: "NAMESPACE NAME [REVISION] [OPTIONS_JSON]", run: func(e env) (out string, err error) {
		// helm-rollback-plan NAMESPACE NAME [REVISION] [OPTIONS_JSON]: what a rollback would
		// change and the chart's rollback hooks it would run ("hooks"), dry-run only, e.g.
		// `web site 1 '{"force":true,"historyMax":2}'`.
		revision, err := helmRevisionArg(flag.Arg(3))
		if err == nil {
			out, err = ichorgo.KubeHelmRollbackPlan(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), revision, flag.Arg(4))
		}

		return out, err
	}},
	{name: "helm-rollback", args: "NAMESPACE NAME [REVISION] [OPTIONS_JSON]", run: func(e env) (out string, err error) {
		// helm-rollback NAMESPACE NAME [REVISION] [OPTIONS_JSON]: rolls the release back (0 or
		// none: the previous revision), with {"force","historyMax"}, running the chart's pre-
		// and post-rollback hooks around the changes.
		revision, err := helmRevisionArg(flag.Arg(3))
		if err != nil {
			return "", err
		}

		if err = ichorgo.KubeHelmRollback(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), revision, flag.Arg(4)); err == nil {
			out = "rolled back"
		}

		return out, err
	}},
	{name: "helm-rollback-follow", args: "NAMESPACE NAME", run: func(e env) (string, error) {
		// helm-rollback-follow NAMESPACE NAME, after helm-rollback: prints the workloads'
		// progress to stderr until they are ready, one failed, or 5 minutes passed.
		return helmRollbackFollowRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2)), nil
	}},
	{name: "audit", args: "[MINUTES]", run: func(e env) (out string, err error) {
		// audit [MINUTES]: reads the control planes' audit logs (os:admin), 15 minutes by default.
		minutes, _ := strconv.Atoi(flag.Arg(1)) //nolint:errcheck
		out, err = ichorgo.KubeAuditAnalysis(e.cfg, e.context, minutes)

		return out, err
	}},
	{name: "netperf-nodes", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.NetPerfNodes(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "netperf", args: "SERVER CLIENT [pod|host] [SECONDS]", run: func(e env) (out string, err error) {
		// netperf SERVER CLIENT [pod|host] [SECONDS]: creates pods in a temporary namespace.
		out = netPerfRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))

		return out, err
	}},
	{name: "image-scan", args: "REF... [-scanner trivy|grype]", run: func(e env) (out string, err error) {
		// image-scan REF... [-scanner grype]: scans image refs with no pod behind them (Trivy,
		// or Grype, in a Job; PROBE_SCANNER sets the scanner too).
		out = imageScanRun(e.cfg, e.context, e.kubeServer, flag.Args()[1:])

		return out, err
	}},
	{name: "scan-history", args: "[CLUSTER]", run: func(e env) (out string, err error) {
		// scan-history [CLUSTER]: the kept scan reports (needs -data-dir), every cluster when omitted.
		out, err = ichorgo.ImageScanHistory(flag.Arg(1))

		return out, err
	}},
	{name: "scan-saved", args: "ID", run: func(e env) (out string, err error) {
		out, err = ichorgo.ImageScanSaved(flag.Arg(1))

		return out, err
	}},
	{name: "netperf-cleanup", args: "NAMESPACE", run: func(e env) (out string, err error) {
		// netperf-cleanup NAMESPACE: deletes an ichor-netperf-* namespace a test left behind.
		if err = ichorgo.NetPerfDeleteNamespace(e.cfg, e.context, e.kubeServer, flag.Arg(1)); err == nil {
			out = "deleted"
		}

		return out, err
	}},
	{name: "node-debug-pod", args: "KUBENODE [NAMESPACE] [IMAGE]", run: func(e env) (out string, err error) {
		// node-debug-pod KUBENODE [NAMESPACE] [IMAGE]: a privileged pod on the node, runs hostname, deleted.
		out = nodeDebugRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))

		return out, err
	}},
	{name: "change-watch", args: "NAMESPACE|all KINDS", run: func(e env) (out string, err error) {
		// change-watch NAMESPACE|all GROUP/VERSION/RESOURCE,...: prints each change signal for 30 s.
		out = changeWatchRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "node-pods-watch", args: "KUBENODE [PHASE]", run: func(e env) (out string, err error) {
		// node-pods-watch KUBENODE [PHASE]: prints the node's pod events for 30 s.
		out = nodePodsWatchRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "resource-watch", args: "GROUP VERSION RESOURCE [NAMESPACE]", run: func(e env) (out string, err error) {
		// resource-watch GROUP VERSION RESOURCE [NAMESPACE]: prints the Table rows' events for
		// 30 s ("" GROUP for the core API).
		out = resourceWatchRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))

		return out, err
	}},
	{name: "delete-pod", args: "NAMESPACE NAME", run: func(e env) (out string, err error) {
		// delete-pod NAMESPACE NAME
		if err = ichorgo.KubeDeletePod(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2)); err == nil {
			out = "deleted"
		}

		return out, err
	}},
	{name: "sanitize-preview", args: "[NAMESPACE]", run: func(e env) (out string, err error) {
		// sanitize-preview [NAMESPACE]: the pods k9s's sanitize would delete, every namespace when omitted.
		out, err = ichorgo.KubeSanitizePreview(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "sanitize", args: "NAMESPACE", run: probeSanitize},
	{name: "scale", args: "KIND NAMESPACE NAME REPLICAS", run: func(e env) (out string, err error) {
		// scale KIND NAMESPACE NAME REPLICAS
		replicas, convErr := strconv.Atoi(flag.Arg(4))
		if convErr != nil {
			fmt.Fprintf(os.Stderr, "scale: replicas %q is not a number\n", flag.Arg(4)) // never scale to 0 by mistake
			os.Exit(2)
		}

		out, err = ichorgo.KubeScale(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), replicas)

		return out, err
	}},
	{name: "suspend-cronjob", args: "NAMESPACE NAME", run: runSuspendCronjob},
	{name: "resume-cronjob", args: "NAMESPACE NAME", run: runSuspendCronjob},
	{name: "revisions", args: "KIND NAMESPACE NAME", run: func(e env) (out string, err error) {
		// revisions Deployment|StatefulSet|DaemonSet NAMESPACE NAME
		out, err = ichorgo.KubeWorkloadRevisions(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))

		return out, err
	}},
	{name: "revision-diff", args: "KIND NAMESPACE NAME REVISION", run: func(e env) (out string, err error) {
		// revision-diff KIND NAMESPACE NAME REVISION: what a rollback to REVISION would undo.
		out, err = ichorgo.KubeRevisionDiff(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), probeRevision(4))

		return out, err
	}},
	{name: "rollback", args: "KIND NAMESPACE NAME REVISION", run: func(e env) (out string, err error) {
		// rollback KIND NAMESPACE NAME REVISION
		if err = ichorgo.KubeRollbackWorkload(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), probeRevision(4)); err == nil {
			out = "rolled back"
		}

		return out, err
	}},
	{name: "rollout-pause", args: "NAMESPACE DEPLOYMENT", run: runRolloutPause},
	{name: "rollout-resume", args: "NAMESPACE DEPLOYMENT", run: runRolloutPause},
	{name: "pod-logs", args: "NAMESPACE POD [CONTAINER] [previous]", run: func(e env) (out string, err error) {
		// pod-logs NAMESPACE POD [CONTAINER] (previous run with -previous via the 4th arg "previous")
		out, err = ichorgo.KubePodLogs(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4) == "previous", 50)

		return out, err
	}},
	{name: "workload-logs", args: "KIND NAMESPACE NAME [FILTER]", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeWorkloadLogs(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), "", flag.Arg(4), false, 0, 0)

		return out, err
	}},
	{name: "maintenance-plan", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeMaintenancePlan(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "cordon", args: "NODE", run: runCordon},
	{name: "uncordon", args: "NODE", run: runCordon},
	{name: "node-meta", args: "NODE", run: runNodeMeta},
	{name: "node-meta-plan", args: "NODE EDITS_JSON [force]", run: runNodeMeta},
	{name: "node-meta-apply", args: "NODE EDITS_JSON [force]", run: runNodeMeta},
	{name: "maintenance", args: "NODE ACTION [IMAGE]", run: func(e env) (out string, err error) {
		// maintenance NODE reboot|shutdown|none, or NODE upgrade IMAGE: cordons and drains the
		// node for real (then upgrades it to IMAGE).
		m := maintenanceProbe{done: make(chan string, 1)}
		if flag.Arg(2) == "upgrade" {
			ichorgo.StartNodeMaintenanceUpgrade(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(3), false, true, false, m)
		} else {
			ichorgo.StartNodeMaintenance(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), false, true, m)
		}
		out = "done: " + <-m.done

		return out, err
	}},
	{name: "kubespan", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeSpanStatus(e.cfg, e.context)

		return out, err
	}},
	{name: "upgrade-plan", args: "NODE", run: func(e env) (out string, err error) {
		// Read-only: never calls the upgrade itself.
		out, err = ichorgo.UpgradePlan(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "reset-plan", args: "NODE", run: func(e env) (out string, err error) {
		// Read-only: never calls the reset itself.
		out, err = ichorgo.NodeResetPlan(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "diagnose-report", args: "", run: func(e env) (out string, err error) {
		// What the AI diagnosis would send, anonymized; nothing is sent.
		var d *ichorgo.Diagnosis
		if d, err = ichorgo.CollectDiagnosis(e.cfg, e.context, e.kubeServer, true); err == nil {
			out = d.Report()
		}

		return out, err
	}},
	{name: "health-explain-report", args: "", run: func(e env) (out string, err error) {
		// What the health check helper would send, anonymized; nothing is sent.
		var d *ichorgo.Diagnosis
		if d, err = collectHealthExplanation(e, true); err == nil {
			out = d.Report()
		}

		return out, err
	}},
	{name: "health-explain", args: "anthropic|openai [MODEL]", run: func(e env) (out string, err error) {
		err = healthExplain(e, flag.Arg(1), flag.Arg(2))

		return noOutput, err
	}},
	{name: "prom-metrics", args: "SOURCE_JSON", run: func(e env) (out string, err error) {
		// The metric names the panel assistant would be given; nothing is sent.
		out, err = ichorgo.PromMetricNames(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "prom-chat", args: "SOURCE_JSON anthropic|openai QUESTION [MODEL]", run: func(e env) (out string, err error) {
		err = promChat(e, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))

		return noOutput, err
	}},
}

// runSuspendCronjob serves suspend-cronjob, resume-cronjob: e.cmd tells which.
func runSuspendCronjob(e env) (out string, err error) {
	if err = ichorgo.KubeSuspendCronJob(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), e.cmd == "suspend-cronjob"); err == nil {
		out = "done"
	}

	return out, err
}

// runCordon serves cordon, uncordon: e.cmd tells which.
func runCordon(e env) (out string, err error) {
	if err = ichorgo.KubeCordon(e.cfg, e.context, e.kubeServer, flag.Arg(1), e.cmd == "cordon"); err == nil {
		out = e.cmd + "ed"
	}

	return out, err
}

// runNodeMeta reads, plans or applies a node's taints and labels: NODE is the Kubernetes
// node name on a kubeconfig, the Talos address otherwise. "force" allows protected changes
// (for the plan, it sets the edits' force).
func runNodeMeta(e env) (string, error) {
	node, edits, force := flag.Arg(1), flag.Arg(2), flag.Arg(3) == "force"
	kube := ichorgo.IsKubeconfig(e.cfg)

	switch e.cmd {
	case "node-meta":
		if kube {
			return ichorgo.KubeNodeMeta(e.cfg, e.context, e.kubeServer, node)
		}

		return ichorgo.NodeMeta(e.cfg, e.context, e.kubeServer, node)
	case "node-meta-plan":
		edits, err := withForce(edits, force)
		if err != nil {
			return "", err
		}

		if kube {
			return ichorgo.KubeNodeMetaPlan(e.cfg, e.context, e.kubeServer, node, edits)
		}

		return ichorgo.NodeMetaPlan(e.cfg, e.context, e.kubeServer, node, edits)
	default:
		var err error
		if kube {
			err = ichorgo.KubeNodeMetaApply(e.cfg, e.context, e.kubeServer, node, edits, force)
		} else {
			err = ichorgo.NodeMetaApply(e.cfg, e.context, e.kubeServer, node, edits, force)
		}

		if err != nil {
			return "", err
		}

		return "applied", nil
	}
}

// withForce sets "force": true in an edits document.
func withForce(edits string, force bool) (string, error) {
	if !force {
		return edits, nil
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(edits), &doc); err != nil {
		return "", fmt.Errorf("bad edits: %w", err)
	}

	doc["force"] = true

	out, err := json.Marshal(doc)

	return string(out), err
}

// probeCluster stands for the cluster id the apps store: the probe only prints the targets.
const probeCluster = "probeclusterabcd"

// probeDiscover runs a cloud discovery (PROVIDER: gke, eks, …; SECRETS_JSON: the fields
// KubeDiscoverFields names) and prints DiscoverProgress to stderr while it runs, the way the
// apps poll it; then the summary of what was found, never the kubeconfig (it may carry
// tokens).
func probeDiscover(e env) (string, error) {
	done := make(chan struct{})

	go func() {
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()

		for {
			select {
			case <-done:
				return
			case <-tick.C:
				if p, err := ichorgo.DiscoverProgress(); err == nil {
					fmt.Fprintln(os.Stderr, p)
				}
			}
		}
	}()

	yaml, err := ichorgo.DiscoverClusters(flag.Arg(1), flag.Arg(2))
	close(done)

	if err != nil {
		return "", err
	}

	return ichorgo.ParseKubeconfig(yaml)
}

// probeSanitize deletes the pods sanitize-preview lists in NAMESPACE (required: never every
// namespace by mistake), the way the apps pass back the preview's rows.
func probeSanitize(e env) (string, error) {
	namespace := flag.Arg(1)
	if namespace == "" {
		fmt.Fprintln(os.Stderr, "sanitize: NAMESPACE is required")
		os.Exit(2)
	}

	preview, err := ichorgo.KubeSanitizePreview(e.cfg, e.context, e.kubeServer, namespace)
	if err != nil {
		return "", err
	}

	var p struct {
		Pods []struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"pods"`
	}
	if err := json.Unmarshal([]byte(preview), &p); err != nil {
		return "", err
	}

	if len(p.Pods) == 0 {
		return "nothing to clean up", nil
	}

	names := make([]string, 0, len(p.Pods))
	for _, pod := range p.Pods {
		names = append(names, pod.Namespace+"/"+pod.Name)
	}

	l := sweepProbe{done: make(chan [2]string, 1)}
	ichorgo.StartKubeSanitize(e.cfg, e.context, e.kubeServer, namespace, strings.Join(names, "\n"), l)

	res := <-l.done
	if res[1] != "" {
		return "", errors.New(res[1])
	}

	return res[0], nil
}

// sweepProbe prints a pod sweep's progress, then hands over its result and error.
type sweepProbe struct{ done chan [2]string }

func (s sweepProbe) OnProgress(json string) { fmt.Println(json) }
func (s sweepProbe) OnDone(resultJSON, errMessage string) {
	s.done <- [2]string{resultJSON, errMessage}
}

// probeSearch reads the context's overview, pods and workloads the way the apps cache them,
// then ranks them for QUERY with SearchSnapshots. A read that fails (no Talos API on a
// kubeconfig cluster) is left out.
func probeSearch(e env) (string, error) {
	reads := []struct {
		key  string
		read func() (string, error)
	}{
		{"overview", func() (string, error) { return ichorgo.ClusterOverview(e.cfg, e.context) }},
		{"pods", func() (string, error) { return ichorgo.KubePods(e.cfg, e.context, e.kubeServer) }},
		{"workloads", func() (string, error) { return ichorgo.KubeWorkloads(e.cfg, e.context, e.kubeServer) }},
	}

	type entry struct {
		Cluster string `json:"cluster"`
		Context string `json:"context"`
		Key     string `json:"key"`
		At      int64  `json:"at"`
		JSON    string `json:"json"`
	}

	var entries []entry

	for _, r := range reads {
		blob, err := r.read()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", r.key, err)

			continue
		}

		entries = append(entries, entry{probeCluster, e.context, r.key, time.Now().UnixMilli(), blob})
	}

	in, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		return "", err
	}

	return ichorgo.SearchSnapshots(string(in), flag.Arg(1), 0)
}

// watchReadProgress prints the progress of op on e's context to stderr every 500 ms until the
// returned func is called, so a slow read can be followed from the terminal.
func watchReadProgress(e env, op string) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()

		for {
			select {
			case <-done:
				return
			case <-tick.C:
				if line, err := ichorgo.ReadProgress(e.cfg, e.context, op); err == nil {
					fmt.Fprintln(os.Stderr, line)
				}
			}
		}
	}()

	return func() {
		close(done)
		<-stopped
	}
}

// helmRevisionArg is an optional revision argument: none is 0, the previous revision.
func helmRevisionArg(arg string) (int, error) {
	if arg == "" {
		return 0, nil
	}

	revision, err := strconv.Atoi(arg)
	if err != nil {
		return 0, fmt.Errorf("revision %q is not a number", arg)
	}

	return revision, nil
}

// probeRevision is the revision number of argument i; a typo stops the probe.
func probeRevision(i int) int {
	revision, err := strconv.Atoi(flag.Arg(i))
	if err != nil {
		fmt.Fprintf(os.Stderr, "revision %q is not a number\n", flag.Arg(i))
		os.Exit(2)
	}

	return revision
}

// runRolloutPause: rollout-pause|rollout-resume NAMESPACE DEPLOYMENT.
func runRolloutPause(e env) (out string, err error) {
	paused := e.cmd == "rollout-pause"
	if err = ichorgo.KubeRolloutPause(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), paused); err == nil {
		out = map[bool]string{true: "paused", false: "resumed"}[paused]
	}

	return out, err
}
