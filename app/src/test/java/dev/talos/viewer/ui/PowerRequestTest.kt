package dev.talos.viewer.ui

import dev.talos.viewer.ui.node.PowerAction
import dev.talos.viewer.ui.node.PowerRequest
import dev.talos.viewer.ui.node.RebootMode
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
        assertEquals("Reboot", PowerRequest(PowerAction.REBOOT).title)
        assertEquals("Power cycle", PowerRequest(PowerAction.REBOOT, RebootMode.POWERCYCLE).title)
        assertEquals("Force reboot", PowerRequest(PowerAction.REBOOT, RebootMode.FORCE).title)
        assertEquals("Shut down", PowerRequest(PowerAction.SHUTDOWN).title)
        assertEquals("Force shut down", PowerRequest(PowerAction.SHUTDOWN, forceShutdown = true).title)
    }

    @Test
    fun forcedOnlyAppliesToTheMatchingAction() {
        assertTrue(PowerRequest(PowerAction.REBOOT, RebootMode.FORCE).forced)
        assertFalse(PowerRequest(PowerAction.REBOOT, forceShutdown = true).forced)
        assertFalse(PowerRequest(PowerAction.SHUTDOWN, RebootMode.FORCE).forced)
    }
}
