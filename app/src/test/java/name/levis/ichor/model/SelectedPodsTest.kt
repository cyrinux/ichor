package name.levis.ichor.model

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SelectedPodsTest {

    @Test
    fun phaseQueriesAreWhatTheCoreTakes() {
        assertEquals(listOf("", "Running", "!Succeeded"), PodPhaseFilter.entries.map { it.query })
    }

    @Test
    fun aNodeListsEveryNamespaceAWorkloadEveryNode() {
        val node = PodSelection.OnNode("192.0.2.20")
        val workload = PodSelection.OfWorkload("Deployment", "demo", "web")
        assertTrue(node.showsNamespace)
        assertFalse(node.showsNode)
        assertFalse(workload.showsNamespace)
        assertTrue(workload.showsNode)
    }

    @Test
    fun keysDifferPerSelectionAndPhase() {
        val node = PodSelection.OnNode("192.0.2.20")
        assertNotEquals(node.key(PodPhaseFilter.ALL), node.key(PodPhaseFilter.RUNNING))
        assertNotEquals(node.key(PodPhaseFilter.ALL), PodSelection.OnNode("192.0.2.21").key(PodPhaseFilter.ALL))
        assertNotEquals(
            PodSelection.OfWorkload("Deployment", "demo", "web").key(PodPhaseFilter.ALL),
            PodSelection.OfWorkload("StatefulSet", "demo", "web").key(PodPhaseFilter.ALL),
        )
        // Never one of the persisted prefixes (pods, workloads...): kept in memory only.
        assertFalse(node.key(PodPhaseFilter.ALL).startsWith("pods|"))
    }

    @Test
    fun onlyTheSelectorKindsListTheirPods() {
        assertEquals(
            PodSelection.OfWorkload("StatefulSet", "db", "postgres"),
            KubeWorkload("StatefulSet", "db", "postgres").podSelection,
        )
        assertNull(KubeWorkload("CronJob", "db", "backup").podSelection)
    }

    @Test
    fun theFirstPageLoadsThenTheNextOnScroll() = runBlocking {
        val pages = mapOf(
            "" to KubePage(List(SELECTED_PODS_PAGE) { "a$it" }, continueToken = "t1", complete = false),
            "t1" to KubePage(listOf("b"), complete = true),
        )
        val first = loadPages(SELECTED_PODS_PAGE, { pages.getValue(it) })
        assertTrue(first.hasMore)
        assertEquals(SELECTED_PODS_PAGE, first.items.size)
        val all = first.loadMore { pages.getValue(it) }
        assertTrue(all.done)
        assertEquals("b", all.items.last())
    }
}
