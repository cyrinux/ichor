package name.levis.ichor.ui.join

import name.levis.ichor.model.NodeOverview
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class JoinPresetTest {
    private val members = listOf(
        NodeOverview(node = "192.0.2.10", hostname = "cp-1", reachable = true, role = "controlplane"),
        NodeOverview(node = "192.0.2.11", hostname = "cp-2", reachable = true, role = "init"),
        NodeOverview(node = "192.0.2.20", hostname = "worker-1", reachable = true, role = "worker"),
    )

    @Test
    fun replaceAControlPlanePicksTheTemplateByAddressWithItsRoleAndTheOldHostName() {
        val form = presetForm(JoinConfigForm(disk = "/dev/sda"), JoinPreset("192.0.2.11", "cp-3", replacing = true), members)
        assertEquals(JoinConfigForm(template = "192.0.2.11", role = "controlplane", hostname = "cp-3", disk = "/dev/sda"), form)
    }

    @Test
    fun aPresetWithoutAHostNameKeepsTheOneTyped() {
        val form = presetForm(JoinConfigForm(hostname = "typed-name"), JoinPreset("192.0.2.10", " ", replacing = false), members)
        assertEquals("typed-name", form?.hostname)
        assertEquals("controlplane", form?.role)
    }

    @Test
    fun noPresetOrAnUnknownTemplateLeavesTheFormToThePerson() {
        assertNull(presetForm(JoinConfigForm(), JoinPreset(), members))
        assertNull(presetForm(JoinConfigForm(), JoinPreset("192.0.2.99", "cp-3", replacing = true), members))
    }
}
