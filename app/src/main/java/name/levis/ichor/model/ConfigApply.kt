package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/configapply_modes.go (StartConfigApply).

/** How a reviewed change is applied for good (try mode aside); [wire] is StartConfigApply's mode. */
enum class ConfigApplyMode(val wire: String) {
    /** Now, without a reboot: only for a change that needs none. */
    AUTO("auto"),
    /** At the next reboot, which is not asked for. */
    STAGED("staged"),
    /** Now, then the node reboots: confirmed with the typed hostname. */
    REBOOT("reboot"),
}

/** The modes a [ConfigPreview] allows: a change that needs a reboot cannot be applied now without one. */
val ConfigPreview.applyModes: List<ConfigApplyMode>
    get() = if (needsReboot) listOf(ConfigApplyMode.STAGED, ConfigApplyMode.REBOOT) else ConfigApplyMode.entries

@Serializable
data class ConfigApplyProgress(val phase: String = "", val message: String = "", val at: Long = 0)

sealed interface ConfigApplyEvent {
    data class Progress(val progress: ConfigApplyProgress) : ConfigApplyEvent

    /** [error] null when the change was applied (or staged). */
    data class Done(val error: String?) : ConfigApplyEvent
}

/** Where an apply stands, for the screen. */
sealed interface ConfigApplyState {
    val mode: ConfigApplyMode

    data class Running(override val mode: ConfigApplyMode, val phase: String = APPLYING, val message: String = "") : ConfigApplyState
    data class Done(override val mode: ConfigApplyMode) : ConfigApplyState
    data class Failed(override val mode: ConfigApplyMode, val message: String) : ConfigApplyState

    companion object {
        const val APPLYING = "applying"
        const val REBOOTING = "rebooting"
        const val WAITING = "waiting"
        const val DONE = "done"
    }
}

/** The state after [event]. */
fun ConfigApplyState.after(event: ConfigApplyEvent): ConfigApplyState = when (event) {
    is ConfigApplyEvent.Progress -> ConfigApplyState.Running(mode, event.progress.phase, event.progress.message)
    is ConfigApplyEvent.Done -> event.error?.let { ConfigApplyState.Failed(mode, it) } ?: ConfigApplyState.Done(mode)
}
