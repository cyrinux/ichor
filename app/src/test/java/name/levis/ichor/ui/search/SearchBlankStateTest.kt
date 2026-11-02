package name.levis.ichor.ui.search

import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KIND_KUBE
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.RecentTarget
import name.levis.ichor.model.ShareTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SearchBlankStateTest {
    private val opened = listOf(RecentTarget(ShareTarget.node("10.0.0.1", "node-a", 0), "node-a"))
    private val kube = ContextSummary(name = "kind-dev", kind = KIND_KUBE)
    private val reader = ContextSummary(name = "reader@lab", roles = listOf("os:reader"))
    private val nodes = listOf(NodeOverview(node = "10.0.0.1", hostname = "node-a", reachable = true))

    @Test
    fun nothingInAnyCacheAndNoRecentsSaysNothingIsKnown() {
        assertEquals(BlankBody.NOTHING_KNOWN, blankBody(snapshotEmpty = true, recent = emptyList(), opened = emptyList()))
    }

    @Test
    fun aKnownClusterWithNoRecentsGetsTheFirstUseState() {
        assertEquals(BlankBody.FIRST_USE, blankBody(snapshotEmpty = false, recent = emptyList(), opened = emptyList()))
    }

    @Test
    fun recentSearchesOrOpenedObjectsKeepTheirLists() {
        assertEquals(BlankBody.RECENTS, blankBody(snapshotEmpty = false, recent = listOf("ngin"), opened = emptyList()))
        assertEquals(BlankBody.RECENTS, blankBody(snapshotEmpty = false, recent = emptyList(), opened = opened))
        // A recently opened object is still useful with an empty snapshot.
        assertEquals(BlankBody.RECENTS, blankBody(snapshotEmpty = true, recent = emptyList(), opened = opened))
    }

    @Test
    fun aKubeconfigClusterIsOfferedItsMainScreensInOrder() {
        assertEquals(listOf("nodes", "workloads", "alerts", "argocd"), firstUseShortcuts(screenCatalog(kube, nodes)).map { it.id })
    }

    @Test
    fun noNodeOrSettingsScreenAndAtMostFour() {
        val ids = firstUseShortcuts(screenCatalog(reader, nodes)).map { it.id }
        assertTrue(ids.size <= 4)
        assertTrue(ids.none { it.startsWith("node/") || it.startsWith("settings/") })
        assertEquals("nodes", ids.first())
    }

    @Test
    fun noCatalogNoShortcut() {
        assertEquals(emptyList<ScreenSpec>(), firstUseShortcuts(emptyList()))
    }
}
