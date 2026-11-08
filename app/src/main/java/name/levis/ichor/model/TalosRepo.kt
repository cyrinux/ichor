package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** What a repository file is to a node: its own patch, a shared one, talhelper's talconfig.yaml, or secrets. */
@Serializable
enum class TalosRepoFileKind {
    @SerialName("node") NODE,
    @SerialName("shared") SHARED,
    @SerialName("talconfig") TALCONFIG,
    @SerialName("secrets") SECRETS,
}

/**
 * One file of `TalosRepoFiles`: [why] the rule that matched; [patch] whether it can be applied
 * (secrets files and talconfig.yaml itself cannot). An inline talconfig patch is
 * "talconfig.yaml#L<line>", [line] its line.
 */
@Serializable
data class TalosRepoFile(
    val path: String = "",
    val kind: TalosRepoFileKind = TalosRepoFileKind.SHARED,
    val why: String = "",
    val line: Int = 0,
    val patch: Boolean = false,
) {
    /** The file itself, without an inline patch's line mark. */
    val file: String get() = path.substringBefore("#L")
}

/** `TalosRepoFiles`: a node's files in the cluster repository at [sha], found as [generator] lays them out. */
@Serializable
data class TalosRepoFiles(
    val sha: String = "",
    val generator: String = "",
    val files: List<TalosRepoFile> = emptyList(),
    val note: String = "",
) {
    /** What Compare applies first: the node's own patch and the shared ones. */
    val defaultSelection: Set<String> get() = files.filter { it.patch }.map { it.path }.toSet()
}

@Serializable
data class TalosRepoSkip(val path: String = "", val why: String = "")

/**
 * `TalosRepoCompare`: the node's config with the repository's patches applied, against the node.
 * [draft] is that config, redacted: the editor's draft for "Edit from this diff".
 */
@Serializable
data class TalosRepoCompare(
    val sha: String = "",
    val files: List<String> = emptyList(),
    val diff: FluxDiff = FluxDiff(),
    val same: Boolean = false,
    val needsReboot: Boolean = false,
    val skipped: List<TalosRepoSkip> = emptyList(),
    val draft: String = "",
)

/** The node's role in a config ("controlplane", "worker"), from its machine.type; "" when it says none. */
fun configRole(configYaml: String): String {
    var inMachine = false
    for (line in configYaml.lineSequence()) {
        if (line.isNotEmpty() && !line[0].isWhitespace()) inMachine = line.trimEnd() == "machine:"
        if (inMachine && line.startsWith("  type:")) {
            val type = line.substringAfter("type:").trim().trim('"', '\'')
            return if (type == "init") "controlplane" else type
        }
    }
    return ""
}

/** The paths of [files] picked in [selected], in the order Compare applies them. */
fun orderedSelection(files: List<TalosRepoFile>, selected: Set<String>): List<String> =
    files.filter { it.patch && it.path in selected }.map { it.path }
