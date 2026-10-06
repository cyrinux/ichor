package name.levis.ichor.ui.node

/** Index of the Cgroups tab, after Pods. */
internal const val CGROUPS_TAB = 5

/** Index of the Kubernetes pods tab (the API server's view), last so deep-link indexes stay. */
internal const val KUBE_PODS_TAB = 6

/**
 * The node tabs shown, in order: Services, Resources, Live, Processes and Pods always (the ones
 * this Talos version lacks stay reachable, dimmed), then Cgroups for admin configs and Kubernetes
 * pods with an API server.
 */
internal fun nodeTabs(canCgroups: Boolean, canKubePods: Boolean): List<Int> =
    listOf(0, 1, 2, 3, 4) + listOfNotNull(CGROUPS_TAB.takeIf { canCgroups }, KUBE_PODS_TAB.takeIf { canKubePods })

/** The tab on screen: a deep link to one this config hides opens Services. */
internal fun shownNodeTab(tab: Int, tabs: List<Int>): Int = if (tab in tabs) tab else 0
