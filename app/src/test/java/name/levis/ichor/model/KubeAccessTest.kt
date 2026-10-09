package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeAccessTest {

    private val json = """
        {"namespace":"shop","actions":{
          "restartWorkload":{"allowed":true},
          "deletePod":{"allowed":false,"verb":"delete","resource":"pods","namespace":"shop","reason":"no RBAC policy matched"},
          "execPod":{"allowed":true,"unknown":true},
          "drainNode":{"allowed":false,"verb":"create","resource":"pods/eviction"}
        }}
    """.trimIndent()

    private val access = TalosJson.decodeFromString(KubeActionAccess.serializer(), json)

    @Test
    fun decodesTheCoreJson() {
        assertEquals("shop", access.namespace)
        val denied = access.actions.getValue("deletePod")
        assertFalse(denied.allowed)
        assertEquals("delete", denied.verb)
        assertEquals("pods", denied.resource)
        assertEquals("shop", denied.namespace)
        assertEquals("no RBAC policy matched", denied.reason)
    }

    @Test
    fun denialOnlyWhenRefusedForSure() {
        assertNull(access.denial(KubeAction.RESTART_WORKLOAD))
        assertEquals("pods", access.denial(KubeAction.DELETE_POD)?.resource)
        // Unknown: the review could not be asked, the action stays offered.
        assertNull(access.denial(KubeAction.EXEC_POD))
        // Not in the answer at all.
        assertNull(access.denial(KubeAction.SCALE))
        // Cluster-wide: no namespace.
        assertEquals("", access.denial(KubeAction.DRAIN_NODE)?.namespace)
    }

    @Test
    fun aMissingAllowedNeverBlocks() {
        val p = TalosJson.decodeFromString(KubePermission.serializer(), """{"verb":"patch"}""")
        assertFalse(p.denied)
        assertTrue(KubePermission(allowed = false).denied)
        assertFalse(KubePermission(allowed = false, unknown = true).denied)
    }

    @Test
    fun wireNamesMatchTheCore() {
        assertEquals(
            listOf(
                "restartWorkload", "scale", "deletePod", "execPod", "suspendCronJob", "triggerCronJob",
                "helmRollback", "argoSync", "fluxReconcile", "cordonNode", "drainNode",
            ),
            KubeAction.entries.map { it.wire },
        )
    }

    @Test
    fun effectiveAccessUsesTheClusterWideAnswerWhenItAllowsAll() {
        val wide = KubeActionAccess("", mapOf("deletePod" to KubePermission(allowed = true)))
        val local = KubeActionAccess("shop", mapOf("deletePod" to KubePermission(allowed = false, verb = "delete", resource = "pods", namespace = "shop")))
        assertSame(wide, effectiveAccess(wide, local))
    }

    @Test
    fun effectiveAccessUsesTheNamespaceWhenTheClusterWideOneRefuses() {
        val wide = KubeActionAccess("", mapOf("deletePod" to KubePermission(allowed = false, verb = "delete", resource = "pods")))
        val local = KubeActionAccess("shop", mapOf("deletePod" to KubePermission(allowed = true)))
        assertSame(local, effectiveAccess(wide, local))
        // The namespace's answer could not be read: nothing is refused.
        val unread = effectiveAccess(wide, null)
        assertNull(unread.denial(KubeAction.DELETE_POD))
        assertFalse(unread.anyDenied)
    }

    @Test
    fun whoAmIIsShownOnlyWhenKnown() {
        val who = TalosJson.decodeFromString(KubeWhoAmI.serializer(), """{"user":"jane@example.com","groups":["devs","system:authenticated"]}""")
        assertTrue(who.known)
        assertEquals(listOf("devs", "system:authenticated"), who.groups)
        assertFalse(TalosJson.decodeFromString(KubeWhoAmI.serializer(), """{"unknown":true}""").known)
        assertFalse(KubeWhoAmI(user = " ").known)
    }
}
