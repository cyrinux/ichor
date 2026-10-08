package name.levis.ichor.model

/** The screens the overview's app bar leads to. */
enum class OverviewAction { HEALTH, EVENTS, WORKLOADS, METRICS, KUBESPAN, ETCD, SETTINGS;

    companion object {
        val bar = ActionBarKind(entries, defaultIcons = 3)
    }
}

/** The overview's app-bar actions; one bar for every cluster. */
typealias OverviewBar = ActionBar<OverviewAction>

/**
 * The screens the Kubernetes home's app bar leads to (a cluster added from a kubeconfig): the
 * Kubernetes screens that work with its credentials alone, no Talos one.
 */
enum class KubeHomeAction { WORKLOADS, RESOURCES, METRICS, HELM, DATA_SERVICES, CHECKUP, API_HEALTH, NETWORK_POLICIES, EVENTS, SETTINGS;

    companion object {
        val bar = ActionBarKind(entries, defaultIcons = 3)
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

/** What a Kubernetes object's app bar offers (the resource browser's object screen). */
enum class KubeObjectAction { EDIT, REFRESH, COPY, SHARE, PORT_FORWARD;

    companion object {
        val bar = ActionBarKind(entries, defaultIcons = 3)
    }
}

typealias KubeObjectBar = ActionBar<KubeObjectAction>
