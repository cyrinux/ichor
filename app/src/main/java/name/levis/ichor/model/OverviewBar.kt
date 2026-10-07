package name.levis.ichor.model

/** The screens the overview's app bar leads to. */
enum class OverviewAction { HEALTH, EVENTS, WORKLOADS, METRICS, KUBESPAN, ETCD, SETTINGS;

    companion object {
        val bar = ActionBarKind(entries, defaultIcons = 3)
    }
}

/** The overview's app-bar actions; one bar for every cluster. */
typealias OverviewBar = ActionBar<OverviewAction>

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
