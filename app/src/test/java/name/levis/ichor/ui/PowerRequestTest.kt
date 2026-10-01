package name.levis.ichor.ui

import name.levis.ichor.R
import name.levis.ichor.ui.node.PowerAction
import name.levis.ichor.ui.node.PowerRequest
import name.levis.ichor.ui.node.RebootMode
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PowerRequestTest {

    @Test
    fun cliModesMatchTalosctl() {
        assertEquals(listOf("default", "powercycle", "force"), RebootMode.entries.map { it.cli })
    }

    @Test
    fun titlesNameExactlyWhatHappens() {
        assertEquals(R.string.power_reboot, PowerRequest(PowerAction.REBOOT).title)
        assertEquals(R.string.power_power_cycle, PowerRequest(PowerAction.REBOOT, RebootMode.POWERCYCLE).title)
        assertEquals(R.string.power_force_reboot, PowerRequest(PowerAction.REBOOT, RebootMode.FORCE).title)
        assertEquals(R.string.power_shut_down, PowerRequest(PowerAction.SHUTDOWN).title)
        assertEquals(R.string.power_force_shut_down, PowerRequest(PowerAction.SHUTDOWN, forceShutdown = true).title)
    }

    @Test
    fun forcedOnlyAppliesToTheMatchingAction() {
        assertTrue(PowerRequest(PowerAction.REBOOT, RebootMode.FORCE).forced)
        assertFalse(PowerRequest(PowerAction.REBOOT, forceShutdown = true).forced)
        assertFalse(PowerRequest(PowerAction.SHUTDOWN, RebootMode.FORCE).forced)
    }
}
