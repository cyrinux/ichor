package name.levis.talosmobile.data

import name.levis.talosmobile.model.ContextSummary
import name.levis.talosmobile.model.Feature
import name.levis.talosmobile.model.allows
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class FeatureTest {

    private fun ctx(vararg roles: String) = ContextSummary(name = "lab", roles = roles.toList())

    @Test
    fun readerIsReadOnly() {
        Feature.entries.forEach { assertFalse(it.name, ctx("os:reader").allows(it)) }
    }

    @Test
    fun operatorCanPowerButNotAdminOnly() {
        val op = ctx("os:operator")
        assertTrue(op.allows(Feature.POWER))
        assertFalse(op.allows(Feature.HEALTH))
        assertFalse(op.allows(Feature.KUBECONFIG))
        assertFalse(op.allows(Feature.DEBUG_SHELL))
        assertTrue(op.allows(Feature.SERVICE_CONTROL))
        assertFalse(op.allows(Feature.ISSUE_CONFIG))
        assertTrue(op.allows(Feature.PACKET_CAPTURE))
        assertFalse(op.allows(Feature.UPGRADE))
    }

    @Test
    fun adminCanEverything() {
        Feature.entries.forEach { assertTrue(it.name, ctx("os:admin").allows(it)) }
    }

    @Test
    fun minimumRoles() {
        assertEquals("os:operator", Feature.POWER.minimumRole)
        assertEquals("os:admin", Feature.HEALTH.minimumRole)
        assertEquals("os:operator", Feature.SERVICE_CONTROL.minimumRole)
        assertEquals("os:admin", Feature.ISSUE_CONFIG.minimumRole)
        assertEquals("os:operator", Feature.PACKET_CAPTURE.minimumRole)
        assertEquals("os:admin", Feature.UPGRADE.minimumRole)
    }
}
