package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo kube_longhorn_engine.go.

/** An engine image a volume can move to; [deployed] on its node and every replica's node. */
@Serializable
data class LonghornEngineImage(
    val image: String = "",
    val version: String = "",
    val deployed: Boolean = false,
    val default: Boolean = false,
    val incompatible: Boolean = false,
) {
    val selectable: Boolean get() = deployed && !incompatible
}

/**
 * What upgrading a volume's engine would do: [live] while attached, offline when detached.
 * [reason] is Longhorn's rule that refuses it (English, shown as is) when [canUpgrade] is false.
 */
@Serializable
data class LonghornEngineUpgradePreview(
    val namespace: String = "",
    val name: String = "",
    val state: String = "",
    val robustness: String = "",
    val currentImage: String = "",
    val specImage: String = "",
    val defaultImage: String = "",
    val live: Boolean = false,
    val canUpgrade: Boolean = false,
    val reason: String = "",
    val images: List<LonghornEngineImage> = emptyList(),
    val error: String = "",
) {
    /** [selected] is a deployed, compatible image other than the current one, and the volume may upgrade. */
    fun accepts(selected: String): Boolean =
        selected.isNotEmpty() && selected != currentImage && error.isEmpty() && canUpgrade &&
            images.any { it.image == selected && it.selectable }

    /** The image to preselect: the default when it can be picked, "" otherwise. */
    val preselected: String
        get() = images.firstOrNull { it.default && it.selectable && it.image != currentImage }?.image.orEmpty()
}

/** An engine image installed in Longhorn, with the volumes using it ([refCount]). */
@Serializable
data class LonghornEngineImageStatus(
    val image: String = "",
    val version: String = "",
    /** deploying or deployed. */
    val state: String = "",
    val refCount: Int = 0,
    val incompatible: Boolean = false,
    val deployedNodes: Int = 0,
    val nodes: Int = 0,
)

/** An image's tag: "longhornio/longhorn-engine:v1.12.0" is "v1.12.0", the image itself when untagged. */
fun engineImageTag(image: String): String =
    image.substringAfterLast('/').substringAfter(':', image.substringAfterLast('/'))

/** The default engine's version, its tag when Longhorn reports none, "" when unknown. */
val LonghornStatus.defaultEngineVersion: String
    get() = engineImages.firstOrNull { it.image == defaultEngineImage }?.version?.takeIf { it.isNotEmpty() }
        ?: defaultEngineImage.takeIf { it.isNotEmpty() }?.let(::engineImageTag).orEmpty()

/** Volumes on an engine older than the default, counted when more than one image is in use; 0 otherwise. */
val LonghornStatus.oldEngineVolumes: Int
    get() = if (engineImages.count { it.refCount > 0 } > 1) volumes.count { it.engineOutdated } else 0
