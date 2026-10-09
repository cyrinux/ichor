package name.levis.ichor.model

/**
 * The screens the overview's app bar leads to, in their default order: GitOps (Argo CD, else
 * Flux) and etcd one tap away when the cluster has them. Saved by name: a bar arranged before
 * keeps its order, an action it lacks goes last in its menu.
 */
enum class OverviewAction { HEALTH, WORKLOADS, GITOPS, ETCD, EVENTS, METRICS, KUBESPAN, SETTINGS;

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
enum class KubeHomeAction { WORKLOADS, RESOURCES, GITOPS, METRICS, HELM, DATA_SERVICES, CHECKUP, API_HEALTH, NETWORK_POLICIES, SETTINGS;

    companion object {
        val bar = ActionBarKind(entries, defaultIcons = 4)
    }
}

/** The Kubernetes home's app-bar actions; one bar for every cluster added from a kubeconfig. */
typealias KubeHomeBar = ActionBar<KubeHomeAction>

/** What the Kubernetes screen's app bar offers: the screens it leads to, its share link and the API address. */
enum class KubernetesAction { CHECKUP, NETWORK_POLICIES, SHARE, API_HEALTH, FLOWS, RESOURCES, HELM, API_ADDRESS;

    companion object {
        val bar = ActionBarKind(entries, defaultIcons = 3)
    }
}

typealias KubernetesBar = ActionBar<KubernetesAction>

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
