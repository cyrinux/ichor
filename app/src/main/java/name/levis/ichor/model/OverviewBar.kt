package name.levis.ichor.model

/**
 * The screens the overview's app bar leads to, in their default order: GitOps (Argo CD, else
 * Flux) and etcd one tap away when the cluster has them. Saved by name: a bar arranged before
 * keeps its order, an action it lacks goes last in its menu.
 */
enum class OverviewAction { HEALTH, WORKLOADS, GITOPS, ETCD, EVENTS, METRICS, ALERTS, KUBESPAN, SEARCH, SETTINGS,
    /** The cluster's own Git repository, once one is saved; last, so the saved bars keep theirs. */
    REPOSITORY;

    companion object {
        val bar = ActionBarKind(entries, defaultIcons = 4)
    }
}

/** The overview's app-bar actions; one bar for every cluster. */
typealias OverviewBar = ActionBar<OverviewAction>

/**
 * The screens the Kubernetes home's app bar leads to (a cluster added from a kubeconfig): the
 * Kubernetes screens that work with its credentials alone, no Talos one. GitOps (Argo CD, else
 * Flux) is one tap away when the cluster has it.
 */
enum class KubeHomeAction { WORKLOADS, RESOURCES, GITOPS, METRICS, ALERTS, HELM, DATA_SERVICES, CHECKUP, API_HEALTH, NETWORK_POLICIES, SEARCH, SETTINGS, CROSSPLANE, TAILSCALE, CLUSTER_LIFECYCLE,
    /** The cluster's own Git repository, once one is saved; last, so the saved bars keep theirs. */
    REPOSITORY;

    companion object {
        val bar = ActionBarKind(entries, defaultIcons = 4)
    }
}

/** The Kubernetes home's app-bar actions; one bar for every cluster added from a kubeconfig. */
typealias KubeHomeBar = ActionBar<KubeHomeAction>

/**
 * What the Kubernetes screen's app bar offers: the screens it leads to and its share link. The
 * API address was here: it is set from the cluster's row in Manage clusters now, and a saved
 * bar drops it.
 */
enum class KubernetesAction { CHECKUP, NETWORK_POLICIES, SHARE, API_HEALTH, FLOWS, RESOURCES, HELM, STORAGE, SERVICES, JOBS, EVENTS,
    /** Kyverno's admission policies, offered when the cluster runs an engine; last, so the saved and default bars keep their icons. */
    POLICIES,
    /** What scales the cluster: HPAs, KEDA, VPA, Karpenter; last, so the saved bars keep theirs. */
    AUTOSCALING,
    /** KubeVirt (and Harvester) virtual machines, offered when the cluster runs it; last, so the saved bars keep theirs. Clean up
     *  dead pods was here: it is a control of the Pods tab now, and a saved bar drops it. */
    VIRTUAL_MACHINES,
    /** Crossplane, the Tailscale operator, Argo Rollouts and Cluster API / Sidero Metal, each offered
     *  when the cluster runs it (as on the Kubernetes home); last, so the saved bars keep theirs. */
    CROSSPLANE, TAILSCALE, ARGO_ROLLOUTS, CLUSTER_LIFECYCLE,
    /** Data services, and the systems the Kubernetes home files in another family (cert-manager,
     *  CAST AI), each offered when the cluster runs it; last, so the saved bars keep theirs.
     *  Sealed Secrets and ExternalDNS are plugins now: the Security and Networking families open
     *  them, and a saved bar drops their entries. */
    DATA_SERVICES, CERT_MANAGER, CASTAI,
    /** Free room per node and what losing one costs, as on the Kubernetes home; last, so the saved bars keep theirs. */
    CAPACITY;

    companion object {
        val bar = ActionBarKind(entries, defaultIcons = 3)
    }
}

typealias KubernetesBar = ActionBar<KubernetesAction>

/** The sections of the Kubernetes screen's menu, in their order. */
enum class KubernetesActionGroup { EXPLORE, HEALTH, INSTALLED }

/** Which section of the menu an action is in: a property of the action, whatever the saved bar. */
val KubernetesAction.group: KubernetesActionGroup
    get() = when (this) {
        KubernetesAction.RESOURCES, KubernetesAction.HELM, KubernetesAction.STORAGE, KubernetesAction.SERVICES,
        KubernetesAction.JOBS, KubernetesAction.EVENTS, KubernetesAction.SHARE,
        -> KubernetesActionGroup.EXPLORE
        KubernetesAction.CHECKUP, KubernetesAction.CAPACITY, KubernetesAction.API_HEALTH,
        KubernetesAction.NETWORK_POLICIES, KubernetesAction.FLOWS,
        -> KubernetesActionGroup.HEALTH
        KubernetesAction.POLICIES, KubernetesAction.AUTOSCALING, KubernetesAction.VIRTUAL_MACHINES, KubernetesAction.CROSSPLANE,
        KubernetesAction.TAILSCALE, KubernetesAction.ARGO_ROLLOUTS, KubernetesAction.CLUSTER_LIFECYCLE,
        KubernetesAction.DATA_SERVICES, KubernetesAction.CERT_MANAGER, KubernetesAction.CASTAI,
        -> KubernetesActionGroup.INSTALLED
    }

/** [menu] in sections, in section order; inside a section the saved order holds. Empty sections are left out. */
fun groupedMenu(menu: List<KubernetesAction>): List<Pair<KubernetesActionGroup, List<KubernetesAction>>> =
    KubernetesActionGroup.entries.mapNotNull { group -> menu.filter { it.group == group }.takeIf { it.isNotEmpty() }?.let { group to it } }

/**
 * What a Kubernetes object's app bar offers (the resource browser's object screen). Delete
 * comes last: in the menu, away from a stray tap.
 */
enum class KubeObjectAction { EDIT, REFRESH, COPY, SHARE, PORT_FORWARD, SCALE, DELETE;

    companion object {
        val bar = ActionBarKind(entries, defaultIcons = 3)
    }
}

typealias KubeObjectBar = ActionBar<KubeObjectAction>
