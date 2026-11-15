package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/newcluster.go.

/** What the person typed for a new cluster, sent to NewClusterPrepare. */
@Serializable
data class NewClusterSpec(
    val name: String,
    /** The machine's address: its only control plane, and the cluster's endpoint. */
    val endpoint: String,
    /** Empty: the default the machine's Talos runs. */
    val kubernetesVersion: String = "",
    /** The machine's own Talos version, from its inspection. */
    val talosVersion: String = "",
    val installDisk: String,
    val installImage: String = "",
    val allowSchedulingOnControlPlanes: Boolean = true,
    val additionalSans: List<String> = emptyList(),
    val extraEditsJSON: String = "",
)

/**
 * A prepared cluster, its secrets held by the Go core behind [draftId]. [talosconfig] is a
 * credential: the app stores it before the run, and never in saved state.
 */
@Serializable
data class NewClusterDraft(
    val draftId: String = "",
    val talosconfig: String = "",
    val redactedControlplane: String = "",
    val redactedWorker: String = "",
    val warnings: List<String> = emptyList(),
    val size: Int = 0,
    val talosVersion: String = "",
    val kubernetesVersion: String = "",
) {
    override fun toString(): String = "NewClusterDraft($draftId, talos $talosVersion, k8s $kubernetesVersion)"
}
