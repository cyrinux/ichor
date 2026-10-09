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
                "restartWorkload", "restartStatefulSet", "restartDaemonSet", "scale", "scaleStatefulSet",
                "deletePod", "execPod", "suspendCronJob", "triggerCronJob", "helmRollback", "argoSync",
                "fluxReconcile", "fluxReconcileHelmRelease", "fluxReconcileGitRepository", "fluxReconcileOCIRepository",
                "fluxReconcileHelmRepository", "fluxReconcileBucket", "cordonNode", "drainNode",
            ),
            KubeAction.entries.map { it.wire },
        )
    }

    @Test
    fun eachKindIsAskedOnItsOwnResource() {
        assertEquals(KubeAction.RESTART_WORKLOAD, KubeAction.restart("Deployment"))
        assertEquals(KubeAction.RESTART_STATEFUL_SET, KubeAction.restart("StatefulSet"))
        assertEquals(KubeAction.RESTART_DAEMON_SET, KubeAction.restart("DaemonSet"))
        assertNull(KubeAction.restart("Rollout"))
        assertEquals(KubeAction.SCALE, KubeAction.scale("Deployment"))
        assertEquals(KubeAction.SCALE_STATEFUL_SET, KubeAction.scale("StatefulSet"))
        // A DaemonSet runs one pod per node: never scaled.
        assertNull(KubeAction.scale("DaemonSet"))
        assertEquals(KubeAction.FLUX_RECONCILE, KubeAction.fluxReconcile("Kustomization"))
        assertEquals(KubeAction.FLUX_RECONCILE_HELM_RELEASE, KubeAction.fluxReconcile("HelmRelease"))
        assertEquals(KubeAction.FLUX_RECONCILE_GIT_REPOSITORY, KubeAction.fluxReconcile("GitRepository"))
        assertEquals(KubeAction.FLUX_RECONCILE_OCI_REPOSITORY, KubeAction.fluxReconcile("OCIRepository"))
        assertEquals(KubeAction.FLUX_RECONCILE_HELM_REPOSITORY, KubeAction.fluxReconcile("HelmRepository"))
        assertEquals(KubeAction.FLUX_RECONCILE_BUCKET, KubeAction.fluxReconcile("Bucket"))
        assertNull(KubeAction.fluxReconcile("ImagePolicy"))
        // No action asked: nothing refused.
        assertNull(access.denial(null))
    }

    @Test
    fun aBulkActionIsRefusedWhenOneNamespaceRefusesIt() {
        val sync = "argoSync"
        val byNamespace = mapOf(
            "argocd" to KubeActionAccess("argocd", mapOf(sync to KubePermission(allowed = true))),
            "team-a" to KubeActionAccess("team-a", mapOf(sync to KubePermission(allowed = false, verb = "patch", resource = "applications", namespace = "team-a"))),
            "team-b" to KubeActionAccess("team-b", mapOf(sync to KubePermission(allowed = false, verb = "patch", resource = "applications", namespace = "team-b"))),
        )
        val checks = { namespaces: List<String> -> namespaces.map { KubeAction.ARGO_SYNC to it } }
        assertNull(firstDenial(checks(listOf("argocd", "argocd")), byNamespace::get))
        // The first refused namespace gives the reason.
        assertEquals("team-a", firstDenial(checks(listOf("argocd", "team-a", "team-b")), byNamespace::get)?.namespace)
        // A namespace not answered yet never blocks.
        assertNull(firstDenial(checks(listOf("argocd", "elsewhere")), byNamespace::get))
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
