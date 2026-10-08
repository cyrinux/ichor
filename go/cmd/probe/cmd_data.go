package main

import (
	"flag"
	"strconv"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// GitOps, the network and data services.
var dataCommands = []command{
	{name: "dataservices", args: "[HINTS]", run: func(e env) (out string, err error) {
		// dataservices [HINTS], e.g. "garage" or "longhorn,cloudnative-pg"; none checks all.
		out, err = ichorgo.KubeDataServices(e.cfg, e.context, e.kubeServer, flag.Arg(1))

		return out, err
	}},
	{name: "garage-blocks", args: "NAMESPACE POD", run: func(e env) (out string, err error) {
		// garage-blocks NAMESPACE POD: what the blocks failing to resync are (read-only).
		out, err = ichorgo.KubeGarageBlockErrors(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2))

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
	{name: "cilium", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeCilium(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "netpol", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.KubeNetworkPolicies(e.cfg, e.context, e.kubeServer)

		return out, err
	}},
	{name: "hubble", args: "[SECONDS] [all|drops] [NAMESPACE [POD]]", run: func(e env) (out string, err error) {
		// hubble [SECONDS] [all|drops] [NAMESPACE [POD]]: read-only exec in the cilium agents.
		out = hubbleRun(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))

		return out, err
	}},
	{name: "kube-inventory", args: "", run: func(e env) (out string, err error) {
		// The apps from the Kubernetes pod list, as a cluster added from a kubeconfig gets them.
		return ichorgo.KubeInventory(e.cfg, e.context, e.kubeServer)
	}},
	{name: "inventory", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.ClusterInventory(e.cfg, e.context)

		return out, err
	}},
}
