package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_argocd_follow.go and kube_flux_follow.go.

/** Phases and results of a followed Argo CD sync (Argo CD's own operation phases). */
const val ARGO_SYNC_PENDING = "Pending"
const val ARGO_SYNC_TERMINATING = "Terminating"
const val ARGO_SYNC_SUCCEEDED = "Succeeded"
const val ARGO_SYNC_FAILED = "Failed"
const val ARGO_SYNC_ERROR = "Error"
const val ARGO_SYNC_TERMINATED = "Terminated"

/** Phases and results of a followed Flux reconcile. */
const val FLUX_FOLLOW_RECONCILING = "reconciling"
const val FLUX_FOLLOW_READY = "ready"
const val FLUX_FOLLOW_FAILED = "failed"

/** The reconcile outlived the 10-minute follow: not a failure. */
const val FLUX_FOLLOW_STILL = "still reconciling"

/** Where a followed Argo CD sync stands (StartArgoSyncFollow's OnProgress). */
@Serializable
data class ArgoSyncProgress(
    val phase: String = "",
    val message: String = "",
    /** The current sync wave's position among the app's waves, 1-based; 0 when unknown. */
    val wave: Int = 0,
    val waves: Int = 0,
    /** Resources synced so far, of [total]. */
    val done: Int = 0,
    val total: Int = 0,
    /** Unix millis. */
    val at: Long = 0,
)

/** Where a followed Flux reconcile stands (StartFluxReconcileFollow's OnProgress). */
@Serializable
data class FluxReconcileProgress(
    val phase: String = "",
    /** The Ready (or Reconciling) condition's message. */
    val message: String = "",
    /** Unix millis. */
    val at: Long = 0,
)
