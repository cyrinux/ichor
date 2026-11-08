package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive

/**
 * One value a kept machine config change sets: [doc] and [kind] the document ("v1alpha1",
 * "HostnameConfig"), [path] dotted, [from] absent for a new value, [to] absent for a removed one.
 */
@Serializable
data class TalosConfigChange(
    val doc: Int = 0,
    val kind: String = "",
    val path: String = "",
    val from: JsonElement? = null,
    val to: JsonElement? = null,
) {
    /** A removed value: not written back (a patch only adds and changes). */
    val removed: Boolean get() = to == null || to is JsonNull
}

/** `TalosConfigPatchFrom`: the patch of a kept change, its values, whether one is a secret's, what it leaves out. */
@Serializable
data class TalosConfigPatch(
    val patch: String = "",
    val changes: List<TalosConfigChange> = emptyList(),
    val touchesSecrets: Boolean = false,
    val notes: List<String> = emptyList(),
) {
    /** The first value written: the pull request's title names it. */
    val firstPath: String get() = changes.firstOrNull { !it.removed }?.path.orEmpty()
}

/** `TalosRepoWriteBackPlan`: the node's [file] (a new one when [create]), the [patch] left to write, the values [skipped]. */
@Serializable
data class TalosWriteBackPlan(
    val sha: String = "",
    val file: String = "",
    val create: Boolean = false,
    val patch: String = "",
    val skipped: List<TalosRepoSkip> = emptyList(),
)

/** A change's value as one line: a string bare, anything else as JSON; "—" when there is none. */
fun changeValueText(v: JsonElement?): String = when {
    v == null || v is JsonNull -> "—"
    v is JsonPrimitive && v.isString -> v.content
    else -> v.toString()
}
