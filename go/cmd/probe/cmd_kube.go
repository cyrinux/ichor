package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

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
	{name: "jobs", args: "[NAMESPACE]", run: func(e env) (out string, err error) {
		// jobs [NAMESPACE]: every namespace when omitted.
		out, err = ichorgo.KubeJobs(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "top-nodes", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeTopNodes(e.cfg, e.context, e.kubeServer)

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
		out, err = ichorgo.KubeAPIHealth(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "checkup", args: "", run: func(e env) (out string, err error) {
		// checkup: what no other screen shows (failing pods, full volumes, dead webhooks...).
		out, err = ichorgo.KubeCheckup(e.cfg, e.context, e.kubeServer)

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
	{name: "helm-rollback-plan", args: "NAMESPACE NAME [REVISION]", run: func(e env) (out string, err error) {
		// helm-rollback-plan NAMESPACE NAME [REVISION]: what a rollback would change, dry-run only.
		revision, err := helmRevisionArg(flag.Arg(3))
		if err == nil {
			out, err = ichorgo.KubeHelmRollbackPlan(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), revision)
		}

		return out, err
	}},
	{name: "helm-rollback", args: "NAMESPACE NAME [REVISION]", run: func(e env) (out string, err error) {
		// helm-rollback NAMESPACE NAME [REVISION]: rolls the release back (0 or none: the previous revision).
		revision, err := helmRevisionArg(flag.Arg(3))
		if err != nil {
			return "", err
		}

		if err = ichorgo.KubeHelmRollback(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), revision); err == nil {
			out = "rolled back"
		}

		return out, err
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
	{name: "image-scan", args: "REF...", run: func(e env) (out string, err error) {
		// image-scan REF...: scans image refs with no pod behind them (Trivy in a Job).
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
	{name: "node-pods-watch", args: "KUBENODE [PHASE]", run: func(e env) (out string, err error) {
		// node-pods-watch KUBENODE [PHASE]: prints the node's pod events for 30 s.
		out = nodePodsWatchRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "delete-pod", args: "NAMESPACE NAME", run: func(e env) (out string, err error) {
		// delete-pod NAMESPACE NAME
		if err = ichorgo.KubeDeletePod(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2)); err == nil {
			out = "deleted"
		}

		return out, err
	}},
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
	{name: "revisions", args: "NAMESPACE NAME", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeDeploymentRevisions(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "rollback", args: "NAMESPACE NAME REVISION", run: func(e env) (out string, err error) {
		// rollback NAMESPACE DEPLOYMENT REVISION
		revision, convErr := strconv.Atoi(flag.Arg(3))
		if convErr != nil {
			fmt.Fprintf(os.Stderr, "rollback: revision %q is not a number\n", flag.Arg(3))
			os.Exit(2)
		}

		if err = ichorgo.KubeRollbackDeployment(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), revision); err == nil {
			out = "rolled back"
		}

		return out, err
	}},
	{name: "pod-logs", args: "NAMESPACE POD [CONTAINER] [previous]", run: func(e env) (out string, err error) {
		// pod-logs NAMESPACE POD [CONTAINER] (previous run with -previous via the 4th arg "previous")
		out, err = ichorgo.KubePodLogs(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4) == "previous", 50)

		return out, err
	}},
	{name: "maintenance-plan", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeMaintenancePlan(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "cordon", args: "NODE", run: runCordon},
	{name: "uncordon", args: "NODE", run: runCordon},
	{name: "maintenance", args: "NODE ACTION", run: func(e env) (out string, err error) {
		// maintenance NODE reboot|shutdown|none: cordons and drains the node for real.
		m := maintenanceProbe{done: make(chan string, 1)}
		ichorgo.StartNodeMaintenance(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), false, true, m)
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
	{name: "diagnose-report", args: "", run: func(e env) (out string, err error) {
		// What the AI diagnosis would send, anonymized; nothing is sent.
		var d *ichorgo.Diagnosis
		if d, err = ichorgo.CollectDiagnosis(e.cfg, e.context, e.kubeServer, true); err == nil {
			out = d.Report()
		}

		return out, err
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
