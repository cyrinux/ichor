package main

import (
	"encoding/json"
	"flag"
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
		out, err = ichorgo.KubeDataServices(e.cfg, e.context, e.kubeServer, flag.Arg(1))

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
	{name: "cilium", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeCilium(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "netpol", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeNetworkPolicies(e.cfg, e.context, e.kubeServer)

		return out, err
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
