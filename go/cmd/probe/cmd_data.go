package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// GitOps, the network and data services.
var dataCommands = []command{
	{name: "longhorn-attachment-preview", args: "NAMESPACE VOLUME", run: func(e env) (string, error) {
		return ichorgo.KubeLonghornAttachmentPreview(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))
	}},
	{name: "longhorn-attachment", args: "NAMESPACE VOLUME NODE ATTACH", run: func(e env) (string, error) {
		attach, err := strconv.ParseBool(flag.Arg(4))
		if err != nil {
			return "", err
		}
		return "requested", ichorgo.KubeLonghornAttachment(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), attach)
	}},
	{name: "longhorn-engine-preview", args: "NAMESPACE VOLUME", run: func(e env) (string, error) {
		return ichorgo.KubeLonghornEngineUpgradePreview(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))
	}},
	{name: "longhorn-engine-upgrade", args: "NAMESPACE VOLUME IMAGE", run: func(e env) (string, error) {
		return "requested", ichorgo.KubeLonghornEngineUpgrade(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))
	}},
	{name: "longhorn-salvage-preview", args: "NAMESPACE VOLUME", run: func(e env) (string, error) {
		return ichorgo.KubeLonghornSalvagePreview(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))
	}},
	{name: "longhorn-salvage", args: "NAMESPACE VOLUME REPLICAS_JSON", run: func(e env) (string, error) {
		return "requested", ichorgo.KubeLonghornSalvage(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))
	}},
	{name: "cnpg-restart-primary", args: "NAMESPACE CLUSTER", run: func(e env) (string, error) {
		return "", ichorgo.KubeCNPGRestartPrimary(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))
	}},
	{name: "cnpg-hibernate", args: "NAMESPACE CLUSTER ENABLED", run: func(e env) (string, error) {
		enabled, err := strconv.ParseBool(flag.Arg(3))
		if err != nil {
			return "", err
		}
		return "", ichorgo.KubeCNPGHibernate(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), enabled)
	}},
	{name: "cnpg-promote-preview", args: "NAMESPACE CLUSTER INSTANCE", run: func(e env) (string, error) {
		return ichorgo.KubeCNPGPromotePreview(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))
	}},
	{name: "cnpg-promote", args: "NAMESPACE CLUSTER INSTANCE", run: func(e env) (string, error) {
		err := ichorgo.KubeCNPGPromote(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))
		return "requested", err
	}},
	{name: "velero-backups", args: "NAMESPACE [SCHEDULE]", run: func(e env) (string, error) {
		// velero-backups NAMESPACE [SCHEDULE]: a schedule's backups, or the ones taken by hand.
		return ichorgo.KubeVeleroBackups(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))
	}},
	{name: "velero-backup-now", args: "NAMESPACE SCHEDULE", run: func(e env) (string, error) {
		return ichorgo.KubeVeleroBackupNow(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))
	}},
	{name: "velero-delete-backup", args: "NAMESPACE BACKUP", run: func(e env) (string, error) {
		return ichorgo.KubeVeleroDeleteBackup(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))
	}},
	{name: "velero-restore-preview", args: "NAMESPACE BACKUP", run: func(e env) (string, error) {
		// velero-restore-preview NAMESPACE BACKUP: what the backup holds and its Restores.
		return ichorgo.KubeVeleroRestorePreview(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))
	}},
	{name: "velero-restore", args: "NAMESPACE BACKUP [OPTIONS_JSON]", run: func(e env) (string, error) {
		return ichorgo.KubeVeleroRestore(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))
	}},
	{name: "vms", args: "[NAMESPACE]", run: func(e env) (string, error) {
		return ichorgo.KubeVirtualMachines(e.cfg, e.context, e.kubeServer, flag.Arg(1))
	}},
	{name: "vm-action", args: "NAMESPACE NAME ACTION", run: func(e env) (string, error) {
		// vm-action NAMESPACE NAME ACTION: start, stop, restart, migrate, pause or unpause.
		return ichorgo.KubeVMAction(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))
	}},
	{name: "vm-stop-force", args: "NAMESPACE NAME [GRACE_SECONDS]", run: func(e env) (string, error) {
		// vm-stop-force NAMESPACE NAME [GRACE_SECONDS]: the forced stop, 0 by default.
		grace := 0
		if flag.Arg(3) != "" {
			parsed, err := strconv.Atoi(flag.Arg(3))
			if err != nil {
				return "", err
			}

			grace = parsed
		}

		return ichorgo.KubeVMStop(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), grace)
	}},
	{name: "tailscale", args: "", run: func(e env) (string, error) {
		return ichorgo.KubeTailscale(e.cfg, e.context, e.kubeServer)
	}},
	{name: "cluster-api", args: "", run: func(e env) (string, error) {
		return ichorgo.KubeClusterAPI(e.cfg, e.context, e.kubeServer)
	}},
	// The Git forge client reads with the token in HOSTJSON ({"kind":"auto","baseURL":"https://github.com","token":"…"});
	// no cluster is used.
	{name: "git-check", args: "HOSTJSON", run: func(env) (string, error) {
		return ichorgo.GitForgeCheck(flag.Arg(1))
	}},
	{name: "git-tree", args: "HOSTJSON REPO REF DIR", run: func(env) (string, error) {
		return ichorgo.GitForgeTree(flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))
	}},
	{name: "git-repo", args: "HOSTJSON REPO [BRANCH]", run: func(env) (string, error) {
		return ichorgo.GitForgeRepo(flag.Arg(1), flag.Arg(2), flag.Arg(3))
	}},
	{name: "git-snapshot", args: "HOSTJSON REPO REF", run: func(env) (string, error) {
		return ichorgo.GitRepoSnapshotPrepare(flag.Arg(1), flag.Arg(2), flag.Arg(3))
	}},
	{name: "git-search", args: "HOSTJSON REPO REF QUERY", run: func(env) (string, error) {
		return ichorgo.GitRepoSearch(flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), 0)
	}},
	{name: "talos-repo-files", args: "HOSTJSON REPO REF DIR HOSTNAME ROLE", run: func(env) (string, error) {
		return ichorgo.TalosRepoFiles(flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), flag.Arg(5), flag.Arg(6))
	}},
	// Reads NODE's config with its secrets (os:admin); only a dry run reaches the node.
	{name: "talos-repo-compare", args: "HOSTJSON REPO REF NODE HOSTNAME PATHSJSON", run: func(e env) (string, error) {
		return ichorgo.TalosRepoCompare(e.cfg, e.context, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), flag.Arg(5), flag.Arg(6))
	}},
	{name: "gitops-check", args: "HOSTJSON REPO REF PATH FILE [FLUXSPECJSON]", run: func(env) (string, error) {
		// The edit of PATH to FILE's content checked on the repository's snapshot. Local.
		content, err := os.ReadFile(flag.Arg(5))
		if err != nil {
			return "", err
		}

		return ichorgo.GitOpsEditCheck(flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), string(content), flag.Arg(6))
	}},
	{name: "git-request", args: "HOSTJSON REPO NUMBER", run: func(env) (string, error) {
		n, err := strconv.Atoi(flag.Arg(3))
		if err != nil {
			return "", fmt.Errorf("NUMBER: %w", err)
		}

		return ichorgo.GitForgeRequest(flag.Arg(1), flag.Arg(2), n)
	}},
	{name: "gitops-revision-runs", args: "TOOL KIND NAMESPACE NAME SHA", run: func(e env) (string, error) {
		return ichorgo.GitOpsRevisionRuns(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), flag.Arg(5))
	}},
	{name: "git-file", args: "HOSTJSON REPO REF PATH", run: func(env) (string, error) {
		return ichorgo.GitForgeFile(flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))
	}},
	// Opens a real pull or merge request: the context only names the cluster in the audit log.
	{name: "git-propose", args: "HOSTJSON OPTIONSJSON", run: func(e env) (string, error) {
		return ichorgo.GitForgePropose(e.cfg, e.context, flag.Arg(1), flag.Arg(2))
	}},
	{name: "crossplane", args: "", run: func(e env) (out string, err error) {
		return ichorgo.KubeCrossplane(e.cfg, e.context, e.kubeServer)
	}},
	{name: "sealed-secrets", run: func(e env) (string, error) {
		raw, err := ichorgo.KubeDataServices(e.cfg, e.context, e.kubeServer, "sealed-secrets")
		if err != nil {
			return "", err
		}
		var report map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &report); err != nil {
			return "", err
		}
		if section, ok := report["sealedSecrets"]; ok {
			return string(section), nil
		}
		return "null", nil
	}},
	{name: "external-dns", run: func(e env) (out string, err error) {
		// external-dns: the ExternalDNS section of Data services (read-only).
		raw, err := ichorgo.KubeDataServices(e.cfg, e.context, e.kubeServer, "external-dns")
		if err != nil {
			return "", err
		}
		var report map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &report); err != nil {
			return "", err
		}
		if section, ok := report["externalDns"]; ok {
			return string(section), nil
		}
		return "null", nil
	}},
	{name: "volume-snapshots", args: "[NAMESPACE]", run: func(e env) (string, error) {
		return ichorgo.KubeVolumeSnapshots(e.cfg, e.context, e.kubeServer, flag.Arg(1))
	}},
	{name: "create-volume-snapshot", args: "NAMESPACE PVC", run: func(e env) (string, error) {
		return ichorgo.KubeCreateVolumeSnapshot(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))
	}},
	{name: "dataservices", args: "[HINTS]", run: func(e env) (out string, err error) {
		// dataservices [HINTS], e.g. "garage" or "longhorn,cloudnative-pg"; none checks all.
		// Its progress goes to stderr every 500 ms while it reads.
		stop := watchReadProgress(e, "dataservices")
		out, err = ichorgo.KubeDataServices(e.cfg, e.context, e.kubeServer, flag.Arg(1))
		stop()

		return out, err
	}},
	{name: "kubegreen", args: "NAMESPACE", run: func(e env) (out string, err error) {
		// kubegreen NAMESPACE: what kube-green has done to one namespace (read-only).
		out, err = ichorgo.KubeGreenNamespace(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "garage-blocks", args: "NAMESPACE POD", run: func(e env) (out string, err error) {
		// garage-blocks NAMESPACE POD: what the blocks failing to resync are (read-only).
		out, err = ichorgo.KubeGarageBlockErrors(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "garage-operator", args: "", run: func(e env) (out string, err error) {
		// garage-operator: the garage-operator's clusters, buckets, keys and nodes (read-only).
		out, err = ichorgo.KubeGarageOperator(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "garage-repair", args: "NAMESPACE POD", run: func(e env) (out string, err error) {
		// garage-repair NAMESPACE POD: metadata repairs if needed, retry of unreferenced blocks.
		out, err = ichorgo.KubeGarageRepairBlocks(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "garage-tranquility", args: "NAMESPACE POD GARAGENODE VALUE", run: func(e env) (out string, err error) {
		// garage-tranquility NAMESPACE POD NODE_ID|* VALUE (0 full speed, 2 Garage's default)
		var value int
		if value, err = strconv.Atoi(flag.Arg(4)); err == nil {
			if err = ichorgo.KubeGarageSetTranquility(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), value); err == nil {
				out = "set"
			}
		}

		return out, err
	}},
	{name: "longhorn-snapshot", args: "NAMESPACE VOLUME ACTION [SNAPSHOT]", run: func(e env) (string, error) {
		// longhorn-snapshot NAMESPACE VOLUME take|backup|delete [SNAPSHOT]; take needs none.
		return "requested", ichorgo.KubeLonghornSnapshotAction(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(4), flag.Arg(3))
	}},
	{name: "longhorn-action", args: "NAMESPACE NAME ACTION [VALUE]", run: func(e env) (out string, err error) {
		// longhorn-action NAMESPACE NAME ACTION [VALUE], e.g. "longhorn-system pvc-1 backup"
		// or "longhorn-system worker-1 evict"; VALUE is the replica count of "replicas".
		value, _ := strconv.Atoi(flag.Arg(4))
		if err = ichorgo.KubeLonghornAction(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), value); err == nil {
			out = "requested"
		}

		return out, err
	}},
	{name: "cert-details", args: "NAMESPACE NAME", run: func(e env) (out string, err error) {
		// cert-details NAMESPACE NAME: conditions, requests, ACME orders and challenges, events, controller log.
		out, err = ichorgo.KubeCertManagerDetails(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "cert-renew", args: "NAMESPACE NAME", run: func(e env) (out string, err error) {
		// cert-renew NAMESPACE NAME: issue the cert-manager certificate again now.
		if err = ichorgo.KubeCertManagerRenew(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2)); err == nil {
			out = "requested"
		}

		return out, err
	}},
	{name: "argocd", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeArgoCD(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "argocd-network", args: "NAMESPACE NAME", run: func(e env) (out string, err error) {
		// argocd-network NAMESPACE NAME
		out, err = ichorgo.KubeArgoNetwork(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "argo-rollouts", args: "", run: func(e env) (string, error) {
		return ichorgo.KubeArgoRollouts(e.cfg, e.context, e.kubeServer)
	}},
	{name: "argo-rollout-action", args: "NAMESPACE NAME ACTION", run: func(e env) (out string, err error) {
		// ACTION: promote, promoteFull, abort, retry or pause.
		if err = ichorgo.KubeArgoRolloutAction(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3)); err == nil {
			out = "requested"
		}

		return out, err
	}},
	{name: "argocd-action", args: "NAMESPACE NAME ACTION [OPTIONS]", run: func(e env) (out string, err error) {
		// argocd-action NAMESPACE NAME ACTION [OPTIONS_JSON], e.g. "argocd web refresh"
		if err = ichorgo.KubeArgoAction(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4)); err == nil {
			out = "requested"
		}

		return out, err
	}},
	{name: "argocd-freeze", args: "NAMESPACE PROJECT ACTION [OPTIONS]", run: func(e env) (out string, err error) {
		// argocd-freeze NAMESPACE PROJECT ACTION [OPTIONS_JSON], e.g.
		// `argocd infra freeze '{"namespaces":["web"],"minutes":60,"manualSync":true}'`
		if err = ichorgo.KubeArgoFreeze(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4)); err == nil {
			out = "requested"
		}

		return out, err
	}},
	{name: "argo-follow", args: "NAMESPACE NAME", run: func(e env) (string, error) {
		// argo-follow NAMESPACE NAME, e.g. "argocd web" after argocd-action ... sync: prints the
		// progress to stderr until the sync ends.
		return argoFollowRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2)), nil
	}},
	{name: "flux", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeFlux(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "flux-action", args: "KIND NAMESPACE NAME ACTION", run: func(e env) (out string, err error) {
		// flux-action KIND NAMESPACE NAME ACTION, e.g. "Kustomization flux-system apps reconcile"
		if err = ichorgo.KubeFluxAction(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4)); err == nil {
			out = "requested"
		}

		return out, err
	}},
	{name: "flux-follow", args: "KIND NAMESPACE NAME", run: func(e env) (string, error) {
		// flux-follow KIND NAMESPACE NAME, e.g. "Kustomization flux-system apps" after
		// flux-action ... reconcile: prints the progress to stderr until it is handled.
		return fluxFollowRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3)), nil
	}},
	{name: "gitops-source", args: "TOOL KIND NAMESPACE NAME", run: func(e env) (string, error) {
		return ichorgo.GitOpsSource(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))
	}},
	{name: "gitops-find", args: "TOOL KIND NAMESPACE NAME RESKIND RESNS RESNAME [HOST_JSON [SOURCE_INDEX]]", run: func(e env) (string, error) {
		// gitops-find TOOL KIND NAMESPACE NAME RESKIND RESNS RESNAME, e.g. "flux Kustomization
		// flux-system podinfo Deployment podinfo podinfo"; an Argo CD app needs HOST_JSON.
		return ichorgo.GitOpsSourceFind(e.cfg, e.context, e.kubeServer, flag.Arg(8), flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4),
			flag.Arg(9), flag.Arg(5), flag.Arg(6), flag.Arg(7))
	}},
	{name: "flux-tree", args: "KIND NAMESPACE NAME DIR", run: func(e env) (string, error) {
		return ichorgo.FluxSourceTree(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))
	}},
	{name: "flux-file", args: "KIND NAMESPACE NAME PATH", run: func(e env) (string, error) {
		return ichorgo.FluxSourceFile(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))
	}},
	{name: "flux-diff", args: "KIND NAMESPACE NAME", run: func(e env) (out string, err error) {
		// flux-diff KIND NAMESPACE NAME, e.g. "Kustomization flux-system apps"
		out, err = ichorgo.KubeFluxDiff(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3))

		return out, err
	}},
	{name: "argo-diff", args: "NAMESPACE NAME", run: func(e env) (out string, err error) {
		// argo-diff NAMESPACE NAME, e.g. "argocd web": reads the controller's compared state from Argo CD's Redis.
		out, err = ichorgo.KubeArgoDiff(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "text-diff", args: "BEFORE_FILE AFTER_FILE NAME", noConfig: true, run: func(e env) (string, error) {
		// text-diff BEFORE_FILE AFTER_FILE NAME: the review diff of an edited source file.
		before, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			return "", err
		}

		after, err := os.ReadFile(flag.Arg(2))
		if err != nil {
			return "", err
		}

		return ichorgo.TextDiff(string(before), string(after), flag.Arg(3))
	}},
	{name: "talos-patch-from", args: "BASE_FILE DRAFT_FILE", noConfig: true, run: func(e env) (string, error) {
		// talos-patch-from BASE_FILE DRAFT_FILE: the patch that turns a redacted config into an edited one.
		base, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			return "", err
		}

		draft, err := os.ReadFile(flag.Arg(2))
		if err != nil {
			return "", err
		}

		return ichorgo.TalosConfigPatchFrom(string(base), string(draft))
	}},
	{name: "talos-writeback-plan", args: "HOSTJSON REPO REF DIR HOSTNAME ROLE PATCH_FILE", run: func(env) (string, error) {
		patch, err := os.ReadFile(flag.Arg(7))
		if err != nil {
			return "", err
		}

		return ichorgo.TalosRepoWriteBackPlan(flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), flag.Arg(5), flag.Arg(6), string(patch))
	}},
	{name: "talos-writeback", args: "FILE PATCH_FILE [OPTIONSJSON]", noConfig: true, run: func(e env) (string, error) {
		// talos-writeback FILE PATCH_FILE [OPTIONSJSON]: a node's file with the patch written into it.
		content, err := os.ReadFile(flag.Arg(1))
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}

		patch, err := os.ReadFile(flag.Arg(2))
		if err != nil {
			return "", err
		}

		return ichorgo.TalosRepoWriteBack(string(content), string(patch), flag.Arg(3))
	}},
	{name: "gitops-quickedit", args: "FILE EDITJSON", noConfig: true, run: func(e env) (string, error) {
		// gitops-quickedit FILE EDITJSON: the file rewritten by a quick edit (image, replicas, path).
		content, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			return "", err
		}

		out, err := ichorgo.GitOpsQuickEdit(string(content), flag.Arg(2))
		if err != nil {
			return "", err
		}

		var res struct {
			Content string `json:"content"`
			Changed bool   `json:"changed"`
			Line    int    `json:"line"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			return "", err
		}

		fmt.Fprintf(os.Stderr, "changed=%t line=%d\n", res.Changed, res.Line)

		return res.Content, nil
	}},
	{name: "gitops-quickedit-targets", args: "FILE", noConfig: true, run: func(e env) (string, error) {
		content, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			return "", err
		}

		return ichorgo.GitOpsQuickEditTargets(string(content))
	}},
	{name: "gitops-proposal-defaults", args: "TOOL KIND NAMESPACE NAME PATH CLUSTER", noConfig: true, run: func(e env) (string, error) {
		return ichorgo.GitOpsProposalDefaults(flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), flag.Arg(5), flag.Arg(6))
	}},
	{name: "cilium", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeCilium(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "netpol", args: "", run: func(e env) (out string, err error) {
		// netpol: the network policies, with their progress on stderr while it reads.
		stop := watchReadProgress(e, "netpol")
		out, err = ichorgo.KubeNetworkPolicies(e.cfg, e.context, e.kubeServer)
		stop()

		return out, err
	}},
	{name: "integrations", args: "", run: func(e env) (out string, err error) {
		// integrations: the API groups Ichor does not read yet, with their progress on stderr.
		stop := watchReadProgress(e, "integrations")
		out, err = ichorgo.KubeIntegrations(e.cfg, e.context, e.kubeServer)
		stop()

		return out, err
	}},
	{name: "kubegreen-set", args: "NAMESPACE NAME WEEKDAYS SLEEP_AT [WAKE_UP_AT] [TZ]", run: func(e env) (string, error) {
		// kubegreen-set: move a kube-green SleepInfo's schedule ("" WAKE_UP_AT: no wake-up).
		// Mutates the cluster.
		return "updated", ichorgo.KubeGreenSetSchedule(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), flag.Arg(5), flag.Arg(6))
	}},
	{name: "crossplane-action", args: "GROUP VERSION RESOURCE NAMESPACE|- NAME pause|resume|reconcile", run: func(e env) (string, error) {
		// crossplane-action: pause, resume or reconcile now a Crossplane managed or composite
		// resource (NAMESPACE "-" for a cluster-scoped one). Mutates the cluster.
		namespace := flag.Arg(4)
		if namespace == "-" {
			namespace = ""
		}

		return flag.Arg(6) + " requested", ichorgo.KubeCrossplaneAction(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), namespace, flag.Arg(5), flag.Arg(6))
	}},
	{name: "kube-scaledobject-pause", args: "NAMESPACE NAME REPLICAS", run: func(e env) (string, error) {
		// kube-scaledobject-pause: pause a KEDA ScaledObject at REPLICAS (-1 holds the current
		// count, -2 resumes). Mutates the cluster.
		replicas, err := strconv.Atoi(flag.Arg(3))
		if err != nil {
			return "", err
		}

		return "updated", ichorgo.KubePauseScaledObject(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), replicas)
	}},
	{name: "kube-hpa-bounds", args: "NAMESPACE NAME MIN MAX", run: func(e env) (string, error) {
		// kube-hpa-bounds: set an HPA's minReplicas and maxReplicas. Mutates the cluster.
		minReplicas, err := strconv.Atoi(flag.Arg(3))
		if err != nil {
			return "", err
		}

		maxReplicas, err := strconv.Atoi(flag.Arg(4))
		if err != nil {
			return "", err
		}

		return "updated", ichorgo.KubeSetHPABounds(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), minReplicas, maxReplicas)
	}},
	{name: "kube-policies", args: "", run: func(e env) (out string, err error) {
		// kube-policies: Kyverno policies with their PolicyReport findings and Gatekeeper
		// constraints with their audit violations (read-only).
		out, err = ichorgo.KubePolicies(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "kube-policy-set-action", args: "NAMESPACE NAME Audit|Enforce", run: func(e env) (string, error) {
		// kube-policy-set-action: switch a Kyverno policy (NAMESPACE "-" for a ClusterPolicy)
		// between Audit and Enforce, the rules' failureAction with it. Mutates the cluster.
		namespace := flag.Arg(1)
		if namespace == "-" {
			namespace = ""
		}

		return ichorgo.KubePolicySetAction(e.cfg, e.context, e.kubeServer, namespace, flag.Arg(2), flag.Arg(3))
	}},
	{name: "kube-autoscaling", args: "", run: func(e env) (out string, err error) {
		// kube-autoscaling: HPAs, KEDA, VPA recommendations and Karpenter pools (read-only).
		out, err = ichorgo.KubeAutoscaling(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "hubble", args: "[SECONDS] [all|drops] [NAMESPACE [POD]]", run: func(e env) (out string, err error) {
		// hubble [SECONDS] [all|drops] [NAMESPACE [POD]]: read-only exec in the cilium agents.
		out = hubbleRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))

		return out, err
	}},
	{name: "inventory", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.ClusterInventory(e.cfg, e.context)

		return out, err
	}},
}
