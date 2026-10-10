package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/configmulti.go (MachineConfigMultiPreview, StartConfigApplyMulti).

/** One node's preview of the same edits: its own diff, or why the edits do not fit it ([error]). */
@Serializable
data class MultiConfigNodePreview(
    val node: String,
    val hostname: String = "",
    val changed: Boolean = false,
    val lines: List<ConfigDiffLine> = emptyList(),
    val needsReboot: Boolean = false,
    val error: String? = null,
) {
    val name: String get() = hostname.ifEmpty { node }
}

@Serializable
data class MultiConfigPreview(val nodes: List<MultiConfigNodePreview> = emptyList(), val anyReboot: Boolean = false) {
    /** The nodes the run would change: the others are skipped or already have the change. */
    val changing: List<MultiConfigNodePreview> get() = nodes.filter { it.error == null && it.changed }

    /** No try (one node only); no "now without a reboot" when one node would need one. */
    val applyModes: List<ConfigApplyMode>
        get() = if (anyReboot) listOf(ConfigApplyMode.STAGED, ConfigApplyMode.REBOOT) else ConfigApplyMode.entries
}

/** A node's line in a multi-node apply: [state] is one of the constants below. */
@Serializable
data class MultiConfigNodeState(val node: String, val hostname: String = "", val state: String = PENDING, val error: String? = null) {
    val name: String get() = hostname.ifEmpty { node }

    companion object {
        const val PENDING = "pending"
        const val APPLYING = "applying"
        const val DONE = "done"
        const val SKIPPED = "skipped"
        const val UNCHANGED = "unchanged"
        const val FAILED = "failed"
    }
}

@Serializable
data class MultiConfigProgress(
    val phase: String = "",
    val message: String = "",
    val at: Long = 0,
    val index: Int = 0,
    val total: Int = 0,
    val node: String = "",
    val nodes: List<MultiConfigNodeState> = emptyList(),
)

sealed interface MultiConfigEvent {
    data class Progress(val progress: MultiConfigProgress) : MultiConfigEvent

    /** [error] null when every node took the change (or was skipped). */
    data class Done(val error: String?) : MultiConfigEvent
}

/** A multi-node apply for the screen: the latest progress, then how it ended. */
data class MultiApplyRun(
    val mode: ConfigApplyMode,
    val progress: MultiConfigProgress? = null,
    val finished: Boolean = false,
    /** Set when it ended in a failure (empty when the run said nothing). */
    val error: String? = null,
) {
    fun after(event: MultiConfigEvent): MultiApplyRun = when (event) {
        is MultiConfigEvent.Progress -> copy(progress = event.progress)
        is MultiConfigEvent.Done -> copy(finished = true, error = event.error)
    }
}
