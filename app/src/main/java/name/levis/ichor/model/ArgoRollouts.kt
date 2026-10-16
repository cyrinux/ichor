package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_argo_rollouts.go (KubeArgoRollouts) and kube_argo_rollouts_actions.go (KubeArgoRolloutAction).

/** The Argo Rollouts of the cluster, the worst first. */
@Serializable
data class ArgoRolloutsStatus(
    /** False: the cluster does not serve Rollouts (no error). */
    val installed: Boolean = false,
    val rollouts: List<ArgoRollout> = emptyList(),
) {
    /** Something is moving: worth polling. */
    val needsPolling: Boolean get() = rollouts.any { it.needsPolling }

    /** The rollouts by namespace, namespaces in name order, each keeping the core's order. */
    val byNamespace: Map<String, List<ArgoRollout>> get() = rollouts.groupBy { it.namespace }.toSortedMap()
}

/** One Rollout: a canary or a blue-green, read like `kubectl argo rollouts get rollout`. */
@Serializable
data class ArgoRollout(
    val namespace: String = "",
    val name: String = "",
    /** canary or blueGreen. */
    val strategy: String = "",
    /** The controller's own word: Healthy, Progressing, Paused, Degraded… */
    val phase: String = "",
    val message: String = "",
    /** By spec.paused or a pause condition (a pause step, a blue-green awaiting promotion). */
    val paused: Boolean = false,
    val pauseReasons: List<String> = emptyList(),
    /** Epoch millis of the first pause condition, 0 when unknown. */
    val pausedSince: Long = 0,
    /** The canary's current step index, -1 when not started (or a blue-green). */
    val step: Int = -1,
    val steps: List<ArgoRolloutStep> = emptyList(),
    /** The canary's share of the traffic, in percent. */
    val weight: Int = 0,
    val aborted: Boolean = false,
    val promoteFull: Boolean = false,
    val stableHash: String = "",
    val currentHash: String = "",
    val replicas: Int = 0,
    val updated: Int = 0,
    val ready: Int = 0,
    val available: Int = 0,
    val analysis: ArgoRolloutAnalysis? = null,
    /** A blue-green that promotes by itself. */
    val autoPromotion: Boolean = false,
    /** "Deployment/name" when the pods come from one, "" otherwise. */
    val workloadRef: String = "",
    val images: List<String> = emptyList(),
    /** critical, warning, ok or idle. */
    val level: String = "",
    val updatedAt: Long = 0,
) {
    val key: String get() = "$namespace/$name"
    val isBlueGreen: Boolean get() = strategy == BLUE_GREEN
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(level)

    /** A new version is out next to the stable one. */
    private val updating: Boolean get() = currentHash.isNotEmpty() && currentHash != stableHash

    /** Paused at a step, or a blue-green with a new version waiting for the switch. */
    val canPromote: Boolean get() = paused && !aborted || (isBlueGreen && updating && !aborted)
    val canPromoteFull: Boolean get() = !aborted && updating
    val canAbort: Boolean get() = !aborted && updating
    val canRetry: Boolean get() = aborted
    val canPause: Boolean get() = !paused && !aborted && currentHash != stableHash

    /** Whether [action] makes sense in this state. */
    fun allows(action: ArgoRolloutAction): Boolean = when (action) {
        ArgoRolloutAction.PROMOTE -> canPromote
        ArgoRolloutAction.PROMOTE_FULL -> canPromoteFull
        ArgoRolloutAction.ABORT -> canAbort
        ArgoRolloutAction.RETRY -> canRetry
        ArgoRolloutAction.PAUSE -> canPause
    }

    /** The share of the steps done, 0 to 1; 0 without steps. */
    val progress: Float get() = if (steps.isEmpty()) 0f else step.coerceIn(0, steps.size).toFloat() / steps.size

    /** Moving on its own (or an analysis deciding): worth polling. */
    val needsPolling: Boolean get() = phase == "Progressing" || (paused && analysis?.status == "Running")

    companion object {
        const val CANARY = "canary"
        const val BLUE_GREEN = "blueGreen"
    }
}

/** A canary step: [kind] setWeight, pause, analysis…; [label] "20 %", "1h", "∞", a template name or "". */
@Serializable
data class ArgoRolloutStep(val kind: String = "", val label: String = "")

/** The running or last analysis: [status] Running, Successful, Failed, Error, Inconclusive… */
@Serializable
data class ArgoRolloutAnalysis(val name: String = "", val status: String = "", val message: String = "") {
    val failed: Boolean get() = status == "Failed" || status == "Error"
}

/** The actions of the kubectl plugin, by their [wire] name in the Go core. */
enum class ArgoRolloutAction(val wire: String) {
    PROMOTE("promote"),
    PROMOTE_FULL("promoteFull"),
    ABORT("abort"),
    RETRY("retry"),
    PAUSE("pause"),
}
