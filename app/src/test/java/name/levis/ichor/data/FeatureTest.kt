package name.levis.ichor.data

import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.KIND_KUBE
import name.levis.ichor.model.accessLabel
import name.levis.ichor.model.isKube
import name.levis.ichor.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class FeatureTest {

    private fun ctx(vararg roles: String) = ContextSummary(name = "lab", roles = roles.toList())

    @Test
    fun readerIsReadOnly() {
        // A reader only browses (non-sensitive) resources and collects a (partial) support bundle.
        val readable = setOf(Feature.RESOURCE_BROWSER, Feature.SUPPORT_BUNDLE)
        Feature.entries.filter { it !in readable }.forEach { assertFalse(it.name, ctx("os:reader").allows(it)) }
        readable.forEach { assertTrue(it.name, ctx("os:reader").allows(it)) }
    }

    @Test
    fun operatorCanPowerButNotAdminOnly() {
        val op = ctx("os:operator")
        assertTrue(op.allows(Feature.POWER))
        assertFalse(op.allows(Feature.HEALTH))
        assertFalse(op.allows(Feature.KUBECONFIG))
        assertFalse(op.allows(Feature.WORKLOADS))
        assertFalse(op.allows(Feature.DEBUG_SHELL))
        assertTrue(op.allows(Feature.SERVICE_CONTROL))
        assertFalse(op.allows(Feature.ISSUE_CONFIG))
        assertTrue(op.allows(Feature.PACKET_CAPTURE))
        assertFalse(op.allows(Feature.UPGRADE))
        assertFalse(op.allows(Feature.ETCD_MEMBER_ACTIONS))
        assertFalse(op.allows(Feature.CGROUPS))
        assertTrue(op.allows(Feature.RESOURCE_BROWSER))
        assertTrue(op.allows(Feature.SUPPORT_BUNDLE))
    }

    @Test
    fun kubeconfigClusterOnlyReachesKubernetes() {
        // No Talos API: every Talos feature is out, whatever roles a summary might carry.
        val kube = ContextSummary(name = "eks", kind = KIND_KUBE, roles = listOf("os:admin"))
        val allowed = setOf(Feature.WORKLOADS, Feature.KUBECONFIG)
        Feature.entries.forEach { assertEquals(it.name, it in allowed, kube.allows(it)) }
        assertTrue(kube.isKube)
        assertEquals(R.string.common_kind_kubernetes, kube.accessLabel)
    }

    @Test
    fun talosIsTheDefaultKind() {
        assertFalse(ctx("os:admin").isKube)
        assertEquals(R.string.common_access_admin, ctx("os:admin").accessLabel)
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
        assertEquals("os:admin", Feature.ETCD_MEMBER_ACTIONS.minimumRole)
        assertEquals("os:reader", Feature.RESOURCE_BROWSER.minimumRole)
        assertEquals("os:reader", Feature.SUPPORT_BUNDLE.minimumRole)
    }
}
