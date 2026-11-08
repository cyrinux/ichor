package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** A file that runs a dependency-update bot on the repository: its configuration, a workflow, or Dependabot's. */
@Serializable
data class UpdateBotFile(val path: String = "", val kind: String = "")

/** The configuration file drafted for a repository without a bot; empty when there is nothing to propose. */
@Serializable
data class UpdateBotDraft(val path: String = "", val content: String = "")

/**
 * `UpdateBotConfig`'s answer for the cluster repository at commit [sha]: the bot files [present]
 * (a bot is configured), else the [draft] to propose, and [notes] saying why.
 */
@Serializable
data class UpdateBotConfig(
    val sha: String = "",
    val present: List<UpdateBotFile> = emptyList(),
    val draft: UpdateBotDraft = UpdateBotDraft(),
    val notes: List<String> = emptyList(),
) {
    val configured: Boolean get() = present.isNotEmpty()
    val proposable: Boolean get() = !configured && draft.path.isNotEmpty() && draft.content.isNotEmpty()

    /** The pull request's description: one line per note. */
    val proposalBody: String get() = notes.joinToString("\n") { "- $it" }
}
