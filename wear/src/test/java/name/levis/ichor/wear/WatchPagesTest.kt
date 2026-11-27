package name.levis.ichor.wear

import name.levis.ichor.widget.NodeCounts
import name.levis.ichor.widget.StatusItem
import name.levis.ichor.widget.Tone
import name.levis.ichor.widget.WIDGET_STALE_AFTER_MS
import name.levis.ichor.widget.WearCluster
import name.levis.ichor.widget.WearNode
import name.levis.ichor.widget.WearNodeHealth
import name.levis.ichor.widget.WearNodeProblem
import name.levis.ichor.widget.WearPayload
import name.levis.ichor.widget.decodeWearPayload
import name.levis.ichor.widget.encodeWearPayload
import name.levis.ichor.widget.trimmedWearPayload
import name.levis.ichor.widget.wearNodeName
import name.levis.ichor.widget.wearNodeProblem
import name.levis.ichor.widget.wearNodes
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WatchPagesTest {

    private val now = 1_800_000_000_000L
    private val fine = NodeCounts(total = 3, ready = 3, notReady = 0, unreachable = 0, etcdChecked = true, etcdAlarms = 0)
    private val broken = NodeCounts(total = 3, ready = 1, notReady = 1, unreachable = 1, etcdChecked = true, etcdAlarms = 2)

    private fun cluster(counts: NodeCounts, takenAt: Long = now, label: String = "Lab") = WearCluster(label, takenAt, counts)

    @Test
    fun aFineClusterHasNoIssueAndTheOkTone() {
        val row = clusterRow(cluster(fine), now)
        assertEquals(Tone.OK, row.tone)
        assertFalse(row.stale)
        assertNull(row.problem)
        assertEquals(emptyList<StatusItem>(), clusterIssues(cluster(fine)))
    }

    @Test
    fun theIssuesComeWorstFirst() {
        assertEquals(
            listOf(StatusItem.Unreachable(1), StatusItem.Etcd(2), StatusItem.NotReady(1)),
            clusterIssues(cluster(broken)),
        )
        val row = clusterRow(cluster(broken), now)
        assertEquals(Tone.BAD, row.tone)
        assertEquals(StatusItem.Unreachable(1), row.problem)
    }

    @Test
    fun aCheckOlderThanAnHourIsStale() {
        val row = clusterRow(cluster(broken, takenAt = now - WIDGET_STALE_AFTER_MS - 1), now)
        assertTrue(row.stale)
        assertEquals(Tone.UNKNOWN, row.tone)
    }

    @Test
    fun theTileOpensAClusterInRangeOnly() {
        val clusters = listOf(cluster(fine, label = "a"), cluster(broken, label = "b"))
        assertEquals("b", clusterAt(clusters, 1)?.label)
        assertNull(clusterAt(clusters, 2))
        assertNull(clusterAt(clusters, -1))
        assertNull(clusterAt(emptyList(), 0))
    }

    @Test
    fun theLastCheckIsTheNewest() {
        assertEquals(now, lastCheck(listOf(cluster(fine, takenAt = now - 5), cluster(fine, takenAt = now))))
        assertNull(lastCheck(listOf(cluster(fine, takenAt = 0))))
    }

    @Test
    fun nodeTonesFollowTheirState() {
        assertEquals(Tone.OK, nodeTone(WearNodeHealth.READY))
        assertEquals(Tone.WARN, nodeTone(WearNodeHealth.NOT_READY))
        assertEquals(Tone.BAD, nodeTone(WearNodeHealth.UNREACHABLE))
    }

    @Test
    fun aNodeNameIsShortAndNeverAnAddress() {
        assertEquals("cp-1", wearNodeName("cp-1.lab.example", masked = false))
        assertEquals("", wearNodeName("cp-1.lab.example", masked = true))
        assertEquals("", wearNodeName("192.0.2.11", masked = false))
        assertEquals("", wearNodeName("2001:db8::1", masked = false))
        assertEquals("", wearNodeName("[2001:db8::1]", masked = false))
        assertEquals("", wearNodeName(" ", masked = false))
    }

    @Test
    fun anErrorBecomesACategory() {
        assertEquals(WearNodeProblem.TIMEOUT, wearNodeProblem("dial tcp 192.0.2.1:50000: i/o timeout"))
        assertEquals(WearNodeProblem.TIMEOUT, wearNodeProblem("context deadline exceeded"))
        assertEquals(WearNodeProblem.REFUSED, wearNodeProblem("connect: connection refused"))
        assertEquals(WearNodeProblem.NO_SUCH_HOST, wearNodeProblem("lookup cp-1.lab.example: no such host"))
        assertEquals(WearNodeProblem.TLS, wearNodeProblem("x509: certificate signed by unknown authority"))
        assertEquals(WearNodeProblem.UNREACHABLE, wearNodeProblem("EOF"))
    }

    @Test
    fun nodesGoDownFirstThenByName() {
        val nodes = wearNodes(
            listOf(
                WearNode("b", WearNodeHealth.READY),
                WearNode("a", WearNodeHealth.READY),
                WearNode("c", WearNodeHealth.NOT_READY),
                WearNode("d", WearNodeHealth.UNREACHABLE, WearNodeProblem.TLS),
            ),
            max = 3,
        )
        assertEquals(listOf("d", "c", "a"), nodes.map { it.name })
    }

    @Test
    fun anOlderPhonePayloadWithoutNodesStillReads() {
        val old = """{"clusters":[{"label":"Lab","takenAt":1,"counts":{"total":1,"ready":1,"notReady":0,"unreachable":0,"etcdChecked":false,"etcdAlarms":0}}]}"""
        val p = decodeWearPayload(old.encodeToByteArray())
        assertEquals(emptyList<WearNode>(), p?.clusters?.single()?.nodes)
    }

    @Test
    fun thirtyNodesOfTenClustersFitTheDataLayerItem() {
        val nodes = List(30) { WearNode("worker-node-$it", WearNodeHealth.UNREACHABLE, WearNodeProblem.NO_SUCH_HOST) }
        val payload = WearPayload(List(10) { WearCluster("cluster number $it", now, broken, nodes) })
        assertTrue(encodeWearPayload(payload).size < 100 * 1024)
    }

    @Test
    fun aTooBigPayloadDropsTheOtherClustersNodesFirst() {
        val nodes = List(30) { WearNode("worker-node-$it", if (it == 0) WearNodeHealth.UNREACHABLE else WearNodeHealth.READY) }
        val payload = WearPayload(List(5) { WearCluster("c$it", now, broken, nodes) })
        val oneCluster = encodeWearPayload(WearPayload(listOf(payload.clusters[0]))).size
        val readyDropped = trimmedWearPayload(payload, limit = oneCluster + 4 * 400)
        assertEquals(30, readyDropped.clusters[0].nodes.size)
        assertEquals(listOf(1, 1, 1, 1), readyDropped.clusters.drop(1).map { it.nodes.size })
        val allDropped = trimmedWearPayload(payload, limit = oneCluster + 4 * 150)
        assertEquals(30, allDropped.clusters[0].nodes.size)
        assertEquals(listOf(0, 0, 0, 0), allDropped.clusters.drop(1).map { it.nodes.size })
        assertEquals(5, allDropped.clusters.size)
    }
}
