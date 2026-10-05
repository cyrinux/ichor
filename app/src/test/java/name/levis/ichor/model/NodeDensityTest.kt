package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NodeDensityTest {

    private fun ready(i: Int) = NodeOverview(node = "10.0.0.$i", hostname = "node-$i", reachable = true, ready = true)

    private fun cluster(size: Int) = (1..size).map(::ready)

    @Test
    fun denseOnlyAboveTheThreshold() {
        assertFalse(isDenseCluster(cluster(3).size))
        assertFalse(isDenseCluster(cluster(24).size))
        assertTrue(isDenseCluster(cluster(25).size))
        assertTrue(isDenseCluster(cluster(200).size))
    }

    @Test
    fun liveStatsSlowDownOnDenseClusters() {
        assertEquals(5L, clusterPollSeconds(3))
        assertEquals(5L, clusterPollSeconds(24))
        assertEquals(15L, clusterPollSeconds(25))
        assertEquals(15L, clusterPollSeconds(200))
    }

    @Test
    fun countsNodesPerHealth() {
        val nodes = cluster(200).mapIndexed { i, node ->
            when {
                i < 3 -> node.copy(ready = false)
                i < 4 -> node.copy(reachable = false)
                else -> node
            }
        }

        val counts = nodes.healthCounts()

        assertEquals(HealthCounts(ready = 196, notReady = 3, unreachable = 1), counts)
        assertEquals(200, counts.total)
        assertEquals(3, counts[NodeHealth.NOT_READY])
        assertEquals(HealthCounts(), emptyList<NodeOverview>().healthCounts())
    }

    @Test
    fun problemsWorstFirstCappedAtFive() {
        val nodes = cluster(200).mapIndexed { i, node ->
            when (i) {
                0 -> node.copy(error = "disk pressure")
                1, 2, 3 -> node.copy(ready = false)
                10, 11, 12 -> node.copy(reachable = false)
                else -> node
            }
        }

        val problems = nodes.problemNodes()

        assertEquals(listOf("node-11", "node-12", "node-13", "node-2", "node-3"), problems.shown.map { it.hostname })
        assertEquals(2, problems.more)
    }

    @Test
    fun fewProblemsAreAllShown() {
        val nodes = cluster(25).mapIndexed { i, node -> if (i == 24) node.copy(reachable = false) else node }

        assertEquals(ProblemNodes(listOf(nodes[24]), 0), nodes.problemNodes())
        assertEquals(ProblemNodes(emptyList(), 0), cluster(3).problemNodes())
    }

    @Test
    fun filtersBySearchHealthAndSite() {
        val a = TopologySite("site-a")
        val groups = listOf(
            NodeGroup(a, listOf(ready(1), ready(2).copy(reachable = false), ready(3).copy(publicIPs = listOf("203.0.113.9")))),
            NodeGroup(null, listOf(ready(4), ready(5).copy(ready = false))),
        )

        assertEquals(groups, groups.filterNodes("", null, null))
        assertEquals(listOf("node-2", "node-5"), groups.filterNodes("", NodeFilter.ATTENTION, null).flatMap { g -> g.nodes.map { it.hostname } })
        assertEquals(listOf(null), groups.filterNodes("", NodeFilter.NOT_READY, null).map { it.site })
        assertEquals(listOf("node-1", "node-3"), groups.filterNodes("", NodeFilter.READY, "site-a").flatMap { g -> g.nodes.map { it.hostname } })
        assertEquals(listOf("node-4"), groups.filterNodes(" 10.0.0.4 ", null, null).flatMap { g -> g.nodes.map { it.hostname } })
        assertEquals(listOf("node-3"), groups.filterNodes("203.0", null, null).flatMap { g -> g.nodes.map { it.hostname } })
        assertEquals(listOf("node-5"), groups.filterNodes("NODE-5", null, "").flatMap { g -> g.nodes.map { it.hostname } })
        assertEquals(emptyList<NodeGroup>(), groups.filterNodes("nothing", null, null))
    }
}
