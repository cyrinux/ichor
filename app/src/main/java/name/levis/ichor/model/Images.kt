package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/talosmobile/images.go.

@Serializable
data class ImageInfo(
    val name: String,
    val digest: String = "",
    /** Bytes. */
    val size: Long = 0,
    /** Unix millis, 0 when unknown. */
    val created: Long = 0,
)

enum class ImageSort { NAME, SIZE, CREATED }

/**
 * Images whose name or digest contains [query] (case-insensitive), sorted by [sort]: name
 * A→Z, or largest/newest first (ties by name).
 */
fun List<ImageInfo>.filteredSorted(query: String, sort: ImageSort): List<ImageInfo> {
    val q = query.trim()
    val matching = if (q.isEmpty()) this else filter { it.name.contains(q, ignoreCase = true) || it.digest.contains(q, ignoreCase = true) }
    val order = when (sort) {
        ImageSort.NAME -> compareBy<ImageInfo> { it.name }
        ImageSort.SIZE -> compareByDescending<ImageInfo> { it.size }.thenBy { it.name }
        ImageSort.CREATED -> compareByDescending<ImageInfo> { it.created }.thenBy { it.name }
    }
    return matching.sortedWith(order)
}

/** Sum of the image sizes (shared layers are counted once per image, like `crictl images`). */
val List<ImageInfo>.totalSize: Long get() = sumOf { it.size }

/** "sha256:0123456789ab…" shortened to its first 12 hex digits. */
fun shortDigest(digest: String): String {
    val algorithm = digest.substringBefore(':', missingDelimiterValue = "")
    val hex = digest.substringAfter(':')
    return if (algorithm.isEmpty()) hex.take(12) else "$algorithm:${hex.take(12)}"
}
