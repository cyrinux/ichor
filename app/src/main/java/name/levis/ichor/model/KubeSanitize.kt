package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** Pods younger than this, Pending, may only be scheduling: left unchecked by default. */
const val SANITIZE_FRESH_PENDING_SEC = 120L

/**
 * What a k9s-style sanitize would delete (Go `KubeSanitizePreview`): the dead or stuck pods of
 * a namespace. [truncated]: more pods than one page, the user narrows the namespace.
 */
@Serializable
data class KubeSanitizePreview(
    val pods: List<SanitizePod> = emptyList(),
    val truncated: Boolean = false,
)

/** A pod the sweep would delete; [ownerKind]/[ownerName] recreate it ("" when none). */
@Serializable
data class SanitizePod(
    val namespace: String = "",
    val name: String = "",
    val status: String = "",
    val ownerKind: String = "",
    val ownerName: String = "",
    val ageSec: Long = 0,
) {
    /** "namespace/name", the form the sweep takes back. */
    val key: String get() = "$namespace/$name"

    /** Recreated by its owner after the delete. */
    val owned: Boolean get() = ownerKind.isNotEmpty() && ownerName.isNotEmpty()

    /** Checked by default unless a Pending pod is young enough to still be scheduling. */
    val checkedByDefault: Boolean get() = !(status == PENDING && ageSec < SANITIZE_FRESH_PENDING_SEC)

    companion object {
        const val PENDING = "Pending"
    }
}

/** The outcome of the sweep (Go `KubeSanitize`): how many went, and each that could not. */
@Serializable
data class KubeSanitizeResult(
    val deleted: Int = 0,
    val failed: List<SanitizeFailure> = emptyList(),
)

/** A pod the sweep could not delete: its "namespace/name" and why. */
@Serializable
data class SanitizeFailure(val name: String = "", val error: String = "")

/** One status of the preview and its pods, by name. */
data class SanitizeGroup(val status: String, val pods: List<SanitizePod>)

/** The preview's pods by status, the largest group first (then by status name). */
fun List<SanitizePod>.groupedByStatus(): List<SanitizeGroup> =
    groupBy { it.status }
        .map { (status, pods) -> SanitizeGroup(status, pods.sortedBy { it.key }) }
        .sortedWith(compareByDescending<SanitizeGroup> { it.pods.size }.thenBy { it.status })

/** The keys checked when the preview opens. */
fun List<SanitizePod>.defaultChecked(): Set<String> = filter { it.checkedByDefault }.map { it.key }.toSet()

/** The checked pods as the sweep takes them: newline-separated "namespace/name", in preview order. */
fun List<SanitizePod>.sweepNames(checked: Set<String>): String =
    filter { it.key in checked }.joinToString("\n") { it.key }
