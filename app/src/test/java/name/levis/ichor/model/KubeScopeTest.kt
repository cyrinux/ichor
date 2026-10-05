package name.levis.ichor.model

import kotlinx.coroutines.runBlocking
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeScopeTest {

    @Test
    fun storedScopes() {
        assertEquals(KubeScope(), KubeScope.fromStored(null))
        assertEquals(KubeScope(chosen = true), KubeScope.fromStored(""))
        assertEquals(KubeScope("shop", chosen = true), KubeScope.fromStored("shop"))
        assertNull(KubeScope().stored)
        assertEquals("", KubeScope(chosen = true).stored)
        assertEquals("shop", KubeScope("shop", chosen = true).stored)
    }

    @Test
    fun defaultIsEveryNamespaceUnlessRemembered() {
        val listed = KubeNamespaces(listOf("a", "shop"))
        assertEquals(KubeScope(), defaultScope(null, null))
        assertEquals(KubeScope(), defaultScope(null, listed))
        assertEquals(KubeScope("shop", chosen = true), defaultScope(KubeScope("shop", chosen = true), listed))
    }

    @Test
    fun forbiddenNamespacesFallBackToTheContextOne() {
        val forbidden = KubeNamespaces(forbidden = true, contextNamespace = "team-a")
        assertEquals(KubeScope("team-a"), defaultScope(null, forbidden))
        // A typed one wins.
        assertEquals(KubeScope("team-b", chosen = true), defaultScope(KubeScope("team-b", chosen = true), forbidden))
        // No namespace to fall back to: every namespace would be refused too, the user types one.
        assertNull(defaultScope(null, KubeNamespaces(forbidden = true)))
    }

    @Test
    fun recognisesARefusedList() {
        assertTrue(isKubeForbidden("Kubernetes API: permission denied: pods is forbidden"))
        assertFalse(isKubeForbidden("Kubernetes API: not found: x"))
    }

    @Test
    fun onlyAChosenScopeLoadsEagerlyUpToTheCap() {
        assertEquals(KUBE_PAGE_SIZE, eagerLimit(KubeScope(), metered = false))
        assertEquals(10_000, eagerLimit(KubeScope(chosen = true), metered = false))
        assertEquals(5_000, eagerLimit(KubeScope("shop", chosen = true), metered = true))
        // The context namespace, not chosen but narrow, loads in full too.
        assertEquals(10_000, eagerLimit(KubeScope("team-a"), metered = false))
    }

    @Test
    fun choicesFromTheClusterElseTheLoadedRows() {
        val scope = KubeScope("typed", chosen = true)
        assertEquals(listOf("a", "b", "typed"), scopeChoices(KubeNamespaces(listOf("b", "a")), listOf("x"), scope))
        assertEquals(listOf("x", "y"), scopeChoices(null, listOf("y", "x"), KubeScope()))
        assertEquals(listOf("x"), scopeChoices(KubeNamespaces(forbidden = true), listOf("x"), KubeScope()))
        assertEquals(listOf("kube-system"), listOf("argocd", "kube-system").matchingNamespaces("SYSTEM"))
    }

    @Test
    fun decodesTheGoNamespaces() {
        val ns = TalosJson.decodeFromString(KubeNamespaces.serializer(), """{"namespaces":["a"],"forbidden":false,"contextNamespace":"default"}""")
        assertEquals(listOf("a"), ns.namespaces)
        assertEquals("default", ns.contextNamespace)
    }

    @Test
    fun decodesAPodPage() {
        val json = """{"pods":[{"namespace":"a","name":"x"}],"continue":"6869","remaining":1500,"complete":false}"""
        val page = TalosJson.decodeFromString(KubePodPage.serializer(), json).toPage(detailed = false)
        assertEquals("6869", page.continueToken)
        assertEquals(1500L, page.remaining)
        assertFalse(page.complete)
        assertFalse(page.detailed)
    }

    @Test
    fun workloadKindsAreMergedIntoOnePage() = runBlocking {
        // Deployments: two pages; StatefulSets: one; DaemonSets: one, uncounted.
        val asked = mutableListOf<String>()
        val fetch: suspend (String, String) -> KubePage<KubeWorkload> = { kind, token ->
            asked += "$kind=$token"
            when {
                kind == "Deployment" && token.isEmpty() -> KubePage(listOf(KubeWorkload(kind, "a", "web")), continueToken = "d1", remaining = 1, complete = false)
                kind == "Deployment" -> KubePage(listOf(KubeWorkload(kind, "a", "api")))
                else -> KubePage(listOf(KubeWorkload(kind, "a", kind.lowercase())))
            }
        }

        val first = fetchWorkloadPage("", fetch)
        assertEquals(3, first.items.size)
        assertFalse(first.complete)
        assertEquals("Deployment=d1", first.continueToken)
        assertEquals(1L, first.remaining)

        val second = fetchWorkloadPage(first.continueToken, fetch)
        assertTrue(second.complete)
        assertEquals(listOf("api"), second.items.map { it.name })
        assertEquals(listOf("Deployment=", "StatefulSet=", "DaemonSet=", "Deployment=d1"), asked)
    }

    @Test
    fun workloadTokens() {
        assertEquals(WORKLOAD_KINDS.associateWith { "" }, parseWorkloadToken(""))
        assertEquals(mapOf("StatefulSet" to "ab", "DaemonSet" to "cd"), parseWorkloadToken("StatefulSet=ab;DaemonSet=cd;Bogus=1"))
        assertEquals("Deployment=a;DaemonSet=b", workloadToken(mapOf("DaemonSet" to "b", "Deployment" to "a")))
    }

    @Test
    fun anUncountedKindLeavesTheTotalUnknown() = runBlocking {
        val page = fetchWorkloadPage("") { kind, _ ->
            KubePage(emptyList<KubeWorkload>(), continueToken = "x", remaining = if (kind == "DaemonSet") -1 else 3, complete = false)
        }
        assertEquals(-1L, page.remaining)
    }
}
