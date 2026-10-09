package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_access.go

/** The app's Kubernetes actions whose permissions the core asks before the tap ([wire]: its name there). */
enum class KubeAction(val wire: String) {
    RESTART_WORKLOAD("restartWorkload"),
    SCALE("scale"),
    DELETE_POD("deletePod"),
    EXEC_POD("execPod"),
    SUSPEND_CRON_JOB("suspendCronJob"),
    TRIGGER_CRON_JOB("triggerCronJob"),
    HELM_ROLLBACK("helmRollback"),
    ARGO_SYNC("argoSync"),
    FLUX_RECONCILE("fluxReconcile"),
    CORDON_NODE("cordonNode"),
    DRAIN_NODE("drainNode"),
}

/**
 * Whether the credentials may run one action. When refused: the [verb] on [resource]
 * ("resource" or "resource/subresource") of [group] in [namespace] ("" for every namespace or a
 * cluster-scoped resource), and the API server's [reason]. [unknown]: the review could not be
 * asked, so the action stays offered and the API server still decides.
 */
@Serializable
data class KubePermission(
    // Always sent; true when missing so a short answer never blocks an action.
    val allowed: Boolean = true,
    val unknown: Boolean = false,
    val verb: String = "",
    val group: String = "",
    val resource: String = "",
    val namespace: String = "",
    val reason: String = "",
) {
    /** Refused for sure: not allowed, and the review was answered. */
    val denied: Boolean get() = !allowed && !unknown
}

/** The access to each of the app's actions ([KubeAction.wire] → answer) in [namespace] ("" cluster-wide). */
@Serializable
data class KubeActionAccess(
    val namespace: String = "",
    val actions: Map<String, KubePermission> = emptyMap(),
) {
    /** What refuses [action], or null when it is allowed, unknown or not asked. */
    fun denial(action: KubeAction): KubePermission? = actions[action.wire]?.takeIf { it.denied }

    /** Whether some action is refused here. */
    val anyDenied: Boolean get() = actions.values.any { it.denied }
}

/**
 * The access to use for a namespace, given the cluster-wide answer [wide] and the namespace's
 * own [local]: what is allowed in every namespace is allowed in each, so [local] is only needed
 * (and only asked) when [wide] refuses something. A [local] that could not be read (null) never
 * blocks: nothing is refused.
 */
fun effectiveAccess(wide: KubeActionAccess, local: KubeActionAccess?): KubeActionAccess = when {
    !wide.anyDenied -> wide
    else -> local ?: KubeActionAccess(namespace = wide.namespace)
}

/** Who the API server takes the credentials for; [unknown] when it cannot say (before Kubernetes 1.28). */
@Serializable
data class KubeWhoAmI(
    val user: String = "",
    val groups: List<String> = emptyList(),
    val unknown: Boolean = false,
) {
    /** Worth showing: answered, with a user. */
    val known: Boolean get() = !unknown && user.isNotBlank()
}
