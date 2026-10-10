package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** Where an image is pulled: Talos installer and system images, or Kubernetes (CRI) images. */
enum class ImagePullNamespace(val wire: String) {
    SYSTEM("system"),
    CRI("cri"),
}

/** One node's state in an image pull (Go StartImagePull progress). */
enum class ImagePullState(val wire: String) {
    PENDING("pending"),
    PULLING("pulling"),
    DONE("done"),
    FAILED("failed"),
    ;

    companion object {
        fun of(wire: String): ImagePullState = entries.firstOrNull { it.wire == wire } ?: PENDING
    }
}

@Serializable
data class ImagePullNode(
    val node: String = "",
    val hostname: String = "",
    val state: String = "",
    val error: String = "",
) {
    val pullState: ImagePullState get() = ImagePullState.of(state)
    val label: String get() = hostname.ifEmpty { node }
}

@Serializable
data class ImagePullProgress(
    val nodes: List<ImagePullNode> = emptyList(),
    val done: Int = 0,
    val total: Int = 0,
    val at: Long = 0,
) {
    val failed: Int get() = nodes.count { it.pullState == ImagePullState.FAILED }
}
