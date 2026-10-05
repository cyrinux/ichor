package name.levis.ichor.model

import kotlinx.serialization.json.Json
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ShareTargetTest {

    private val summary = ConfigSummary(
        current = "admin@prod",
        contexts = listOf(
            ContextSummary(name = "admin@prod", clusterId = "prodprodprodprod"),
            ContextSummary(name = "reader@lab", clusterId = "lablablablablabl"),
            ContextSummary(name = "admin@lab", clusterId = "lablablablablabl"),
            ContextSummary(name = "admin@legacy"),
        ),
    )

    @Test
    fun linkOpensTheActiveContextOfItsCluster() {
        assertEquals("admin@lab", summary.contextFor("lablablablablabl", active = "admin@lab")?.name)
    }

    @Test
    fun linkOpensTheFirstContextOfItsClusterOtherwise() {
        assertEquals("reader@lab", summary.contextFor("lablablablablabl", active = "admin@prod")?.name)
    }

    @Test
    fun linkOfAnUnknownClusterOpensNothing() {
        assertNull(summary.contextFor("otherotherotherx", active = "admin@prod"))
        // A summary from a core without cluster ids never matches an empty one.
        assertNull(summary.contextFor("", active = "admin@legacy"))
    }

    @Test
    fun goJsonDecodes() {
        val target = TalosJson.decodeFromString(
            ShareTarget.serializer(),
            """{"cluster":"lablablablablabl","target":"flux-app","kind":"HelmRelease","ns":"flux-system","name":"podinfo"}""",
        )
        assertEquals(ShareTarget.fluxApp("HelmRelease", "flux-system", "podinfo").copy(cluster = "lablablablablabl"), target)
    }

    @Test
    fun encodedForGoWithItsFieldNames() {
        val json = Json.encodeToString(ShareTarget.serializer(), ShareTarget.argoApp("argocd", "guestbook"))
        assertEquals("""{"target":"argo-app","ns":"argocd","name":"guestbook"}""", json)
    }

    @Test
    fun nodeTabsRoundTrip() {
        val target = ShareTarget.node("10.0.0.2", "cp-1", tab = 2)
        assertEquals("live", target.tab)
        assertEquals(2, target.nodeTab)
        assertEquals(0, ShareTarget(target = ShareTarget.NODE).nodeTab)
        assertEquals("", ShareTarget.node("10.0.0.2", "cp-1", tab = 99).tab)
    }

    @Test
    fun kubernetesTargetsFocusTheirTabAndRow() {
        assertEquals(KubeFocus(2), ShareTarget.kubernetes(2).kubeFocus)
        assertEquals(KubeFocus(0), ShareTarget.kubernetes(3).kubeFocus)
        assertEquals(
            KubeFocus(0, "StatefulSet/db/postgres", "db", "postgres"),
            ShareTarget.workload("StatefulSet", "db", "postgres").kubeFocus,
        )
        assertEquals(KubeFocus(1, "db/postgres-0", "db", "postgres-0"), ShareTarget.pod("db", "postgres-0").kubeFocus)
        assertEquals(KubeFocus(2, "ops/backup", "ops", "backup"), ShareTarget.cronJob("ops", "backup").kubeFocus)
        assertNull(ShareTarget.argoApp("argocd", "guestbook").kubeFocus)
    }
}
