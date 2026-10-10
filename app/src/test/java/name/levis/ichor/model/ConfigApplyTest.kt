package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class ConfigApplyTest {

    @Test
    fun modesFollowTheNeedForAReboot() {
        assertEquals(ConfigApplyMode.entries, ConfigPreview(changed = true).applyModes)
        assertEquals(listOf(ConfigApplyMode.STAGED, ConfigApplyMode.REBOOT), ConfigPreview(changed = true, needsReboot = true).applyModes)
        assertEquals(listOf("auto", "staged", "reboot"), ConfigApplyMode.entries.map { it.wire })
    }

    @Test
    fun stateFollowsTheEvents() {
        val progress = TalosJson.decodeFromString(ConfigApplyProgress.serializer(), """{"phase":"rebooting","message":"the node reboots","at":5}""")
        var state: ConfigApplyState = ConfigApplyState.Running(ConfigApplyMode.REBOOT)
        state = state.after(ConfigApplyEvent.Progress(progress))
        assertEquals(ConfigApplyState.Running(ConfigApplyMode.REBOOT, "rebooting", "the node reboots"), state)
        assertEquals(ConfigApplyState.Done(ConfigApplyMode.REBOOT), state.after(ConfigApplyEvent.Done(null)))
        assertEquals(ConfigApplyState.Failed(ConfigApplyMode.REBOOT, "refused"), state.after(ConfigApplyEvent.Done("refused")))
    }
}
