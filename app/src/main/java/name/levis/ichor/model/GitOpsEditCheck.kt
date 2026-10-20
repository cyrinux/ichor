package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/gitops_check.go.

/** A build error, with the repository file and line it names (0 when none). */
@Serializable
data class EditCheckError(val message: String = "", val path: String = "", val line: Int = 0)

/**
 * An edit checked before it is proposed: [kind] is kustomize (the nearest kustomization built
 * before and after), plain (the file's own documents), helm (not rendered on the phone) or
 * none; when [ok], [diff] lists the rendered objects that change ([unchanged] counted).
 */
@Serializable
data class GitOpsEditCheck(
    val sha: String = "",
    val kind: String = "",
    val dir: String = "",
    val ok: Boolean = false,
    val errors: List<EditCheckError> = emptyList(),
    val diff: FluxDiff = FluxDiff(),
    val unchanged: Int = 0,
    val note: String = "",
) {
    /** Something was built or parsed: the objects say what changes. */
    val built: Boolean get() = ok && (kind == EDIT_CHECK_KUSTOMIZE || kind == EDIT_CHECK_PLAIN)
}

const val EDIT_CHECK_KUSTOMIZE = "kustomize"
const val EDIT_CHECK_PLAIN = "plain"

/** The minimal Flux spec of an app's source: its path, built as kustomize-controller would. */
fun fluxCheckSpec(path: String): String = """{"path":${kotlinx.serialization.json.JsonPrimitive(path)}}"""
