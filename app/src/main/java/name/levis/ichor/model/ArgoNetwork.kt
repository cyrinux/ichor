package name.levis.ichor.model

// An Argo CD app's network is a NetMap (go/ichorgo/kube_argocd_network.go):
// host → Gateway → Ingress/HTTPRoute/GRPCRoute/TLSRoute → Service → Pod → node, left to right.
// What is Argo's own is how its problem banner words the root cause.

/** How the Argo CD network's problem banner words a [NetMapProblem]. */
enum class ArgoNetWording { NODE_NOT_READY, NODE_CORDONED, POD_STATUS, NO_READY_PODS, NO_HEALTHY_BACKEND }

val NetMapProblem.wording: ArgoNetWording
    get() = when (kind) {
        // Go words a node that is only cordoned SchedulingDisabled.
        NetMapNode.NODE -> if (detail == NetMap.SCHEDULING_DISABLED) ArgoNetWording.NODE_CORDONED else ArgoNetWording.NODE_NOT_READY
        NetMapNode.POD -> ArgoNetWording.POD_STATUS
        NetMapNode.SERVICE -> ArgoNetWording.NO_READY_PODS
        else -> ArgoNetWording.NO_HEALTHY_BACKEND
    }
