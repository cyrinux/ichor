package name.levis.ichor.model

// Drill-down pod lists (plans/roadmap/large-clusters.md, Phase 5): the Kubernetes pods of one
// node (KubeNodePodsPage) or of one workload (KubeWorkloadPodsPage), page by page.

/** Rows a drill-down page asks for: the first page eagerly, the next ones on scroll. */
const val SELECTED_PODS_PAGE = 200

/** The phase a drill-down list is narrowed to, as the Go core takes it ([query]). */
enum class PodPhaseFilter(val query: String) {
    ALL(""),
    RUNNING("Running"),
    /** Every pod but the finished ones (Jobs' Completed pods). */
    NOT_COMPLETED("!Succeeded"),
}

/** What a drill-down pod list shows. */
sealed interface PodSelection {
    /** Its rows come from several namespaces: each row says which. */
    val showsNamespace: Boolean

    /** Its rows run on several nodes: each row says which. */
    val showsNode: Boolean

    /** Key of its list narrowed to [phase] (kept in memory only, never on disk). */
    fun key(phase: PodPhaseFilter): String

    /** The pods scheduled on the Talos node [node] (its address), in every namespace. */
    data class OnNode(val node: String) : PodSelection {
        override val showsNamespace get() = true
        override val showsNode get() = false
        override fun key(phase: PodPhaseFilter) = "nodepods|$node|${phase.query}"
    }

    /** The pods a Deployment, StatefulSet or DaemonSet's selector matches. */
    data class OfWorkload(val kind: String, val namespace: String, val name: String) : PodSelection {
        override val showsNamespace get() = false
        override val showsNode get() = true
        override fun key(phase: PodPhaseFilter) = "workloadpods|$kind/$namespace/$name|${phase.query}"
    }
}

/** The selection of this workload's pods; null for a kind the Go core cannot list them of. */
val KubeWorkload.podSelection: PodSelection.OfWorkload?
    get() = if (kind in WORKLOAD_KINDS) PodSelection.OfWorkload(kind, namespace, name) else null
