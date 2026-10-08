package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeNodeDensityTest {

    private fun node(i: Int, ready: Boolean = true, cordoned: Boolean = false, pressure: List<String> = emptyList()) =
        KubeNodeInfo(name = "ip-10-0-0-$i", ready = ready, cordoned = cordoned, internalIP = "10.0.0.$i", pressure = pressure)

    private fun cluster(size: Int) = (1..size).map { node(it) }

    @Test
    fun statusIsTheWorstOfReadyCordonAndPressure() {
        assertEquals(NodeStatus.READY, node(1).status)
        assertEquals(NodeStatus.ATTENTION, node(1, cordoned = true).status)
        assertEquals(NodeStatus.ATTENTION, node(1, pressure = listOf("DiskPressure")).status)
        // Not ready outranks a cordon: the node is down, the cordon is a detail.
        assertEquals(NodeStatus.NOT_READY, node(1, ready = false, cordoned = true).status)
        assertFalse(node(1).needsAttention)
        assertTrue(node(1, cordoned = true).needsAttention)
    }

    @Test
    fun countsNodesPerStatus() {
        val nodes = cluster(40).mapIndexed { i, n ->
            when {
                i < 2 -> n.copy(ready = false)
                i < 5 -> n.copy(cordoned = true)
                else -> n
            }
        }

        assertEquals(HealthCounts(ready = 35, attention = 3, notReady = 2), nodes.kubeHealthCounts())
        assertEquals(HealthCounts(), emptyList<KubeNodeInfo>().kubeHealthCounts())
    }

    @Test
    fun groupsByStatusWorstFirstKeepingTheOrderWithin() {
        val nodes = listOf(node(1), node(2, cordoned = true), node(3, ready = false), node(4), node(5, pressure = listOf("MemoryPressure")))

        val groups = nodes.byStatus()

        assertEquals(listOf(NodeStatus.NOT_READY, NodeStatus.ATTENTION, NodeStatus.READY), groups.map { it.status })
        assertEquals(listOf("ip-10-0-0-3"), groups[0].nodes.map { it.name })
        assertEquals(listOf("ip-10-0-0-2", "ip-10-0-0-5"), groups[1].nodes.map { it.name })
        assertEquals(listOf("ip-10-0-0-1", "ip-10-0-0-4"), groups[2].nodes.map { it.name })
        // No empty group: a healthy cluster is one group.
        assertEquals(listOf(NodeStatus.READY), cluster(3).byStatus().map { it.status })
    }

    @Test
    fun problemNodesAreWorstFirstAndCapped() {
        val nodes = cluster(30).mapIndexed { i, n ->
            when {
                i < 6 -> n.copy(cordoned = true)
                i < 8 -> n.copy(ready = false)
                else -> n
            }
        }

        val problems = nodes.kubeProblemNodes()

        // The two not-ready ones first, then the cordoned, cut at five.
        assertEquals(listOf("ip-10-0-0-7", "ip-10-0-0-8", "ip-10-0-0-1", "ip-10-0-0-2", "ip-10-0-0-3"), problems.shown.map { it.name })
        assertEquals(3, problems.more)
        assertEquals(ProblemNodes(emptyList<KubeNodeInfo>(), 0), cluster(3).kubeProblemNodes())
    }

    @Test
    fun filtersByStatusAndQuery() {
        val nodes = listOf(node(1), node(2, cordoned = true), node(3, ready = false), node(14))

        assertEquals(listOf("ip-10-0-0-2", "ip-10-0-0-3"), nodes.filterKubeNodes("", NodeFilter.ATTENTION).map { it.name })
        assertEquals(listOf("ip-10-0-0-1", "ip-10-0-0-2", "ip-10-0-0-14"), nodes.filterKubeNodes("", NodeFilter.READY).map { it.name })
        assertEquals(listOf("ip-10-0-0-3"), nodes.filterKubeNodes("", NodeFilter.NOT_READY).map { it.name })
        // Nothing is unreachable to Kubernetes: the filter is not offered, and matches nothing.
        assertEquals(emptyList<KubeNodeInfo>(), nodes.filterKubeNodes("", NodeFilter.UNREACHABLE))
        assertFalse(KUBE_NODE_FILTERS.contains(NodeFilter.UNREACHABLE))
        // The query matches the name or the address, case-insensitively, trimmed.
        assertEquals(listOf("ip-10-0-0-1", "ip-10-0-0-14"), nodes.filterKubeNodes(" 0-1 ", null).map { it.name })
        assertEquals(listOf("ip-10-0-0-14"), nodes.filterKubeNodes("10.0.0.14", null).map { it.name })
        assertEquals(listOf("ip-10-0-0-2"), nodes.filterKubeNodes("IP-10-0-0-2", NodeFilter.ATTENTION).map { it.name })
        assertEquals(nodes, nodes.filterKubeNodes("", null))
    }

    @Test
    fun externalAddressMatchesToo() {
        val nodes = listOf(KubeNodeInfo(name = "w-1", ready = true, externalIP = "203.0.113.9"))

        assertEquals(1, nodes.filterKubeNodes("203.0", null).size)
    }
}
