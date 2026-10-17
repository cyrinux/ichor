package name.levis.ichor.model

import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class OverviewStreamTest {

    private fun up(addr: String) = NodeOverview(node = addr, hostname = "host-$addr", reachable = true, ready = true)

    private fun down(addr: String) =
        NodeOverview(node = addr, hostname = addr, reachable = false, error = "unreachable", errorKind = "network")

    private val targets = listOf("10.0.0.1", "10.0.0.2", "10.0.0.3")

    @Test
    fun partialNodesFollowTheContextOrder() {
        val partial = partialOverview("lab", listOf(up("10.0.0.3"), up("10.0.0.1")), targets)
        assertEquals(listOf("10.0.0.1", "10.0.0.3"), partial.nodes.map { it.node })
        assertTrue(partial.partial)
        assertEquals(3, partial.expectedNodes)
    }

    @Test
    fun nodesTheContextDoesNotListComeLastInArrivalOrder() {
        val partial = partialOverview("lab", listOf(up("x"), up("10.0.0.2"), up("y")), targets)
        assertEquals(listOf("10.0.0.2", "x", "y"), partial.nodes.map { it.node })
    }

    @Test
    fun collectShowsEachStepAndReturnsTheCompleteOverview() = runBlocking {
        val complete = ClusterOverview("lab", listOf(up("10.0.0.1"), up("10.0.0.2"), down("10.0.0.3")))
        val shown = mutableListOf<ClusterOverview>()
        val result = flowOf(
            OverviewPart.Node(up("10.0.0.2")),
            OverviewPart.Node(up("10.0.0.1")),
            OverviewPart.Node(down("10.0.0.3")),
            OverviewPart.Done(complete),
        ).collectOverview("lab", targets) { shown += it }
        assertEquals(complete, result)
        assertFalse(result.partial)
        assertEquals(listOf(1, 2, 3), shown.map { it.nodes.size })
        assertEquals(listOf(NodesAnswered(1, 3), NodesAnswered(2, 3), NodesAnswered(3, 3)), shown.map { it.nodesAnswered })
    }

    @Test(expected = IllegalStateException::class)
    fun aStreamWithoutResultFails() {
        runBlocking { flowOf(OverviewPart.Node(up("10.0.0.1"))).collectOverview("lab", targets) {} }
    }

    @Test
    fun aPartialOverviewIsNeverAnOutage() {
        val partial = partialOverview("lab", listOf(down("10.0.0.1")), targets)
        assertNull(partial.outage)
        assertNotNull(partial.copy(partial = false).outage)
    }

    @Test
    fun aCompleteOverviewHasNoAnsweredCount() {
        assertNull(ClusterOverview("lab", listOf(up("10.0.0.1"))).nodesAnswered)
    }

    @Test
    fun anUnknownOrTooSmallTotalIsLeftOut() {
        assertEquals(NodesAnswered(2, null), partialOverview("lab", listOf(up("a"), up("b")), emptyList()).nodesAnswered)
        assertEquals(NodesAnswered(2, null), partialOverview("lab", listOf(up("a"), up("b")), listOf("a")).nodesAnswered)
    }

    @Test
    fun partialIsNotSerialised() {
        val partial = partialOverview("lab", listOf(up("10.0.0.1")), targets)
        val json = TalosJson.encodeToString(ClusterOverview.serializer(), partial)
        assertFalse("partial" in json)
        assertFalse("expectedNodes" in json)
        assertFalse(TalosJson.decodeFromString(ClusterOverview.serializer(), json).partial)
    }

    @Test
    fun targetsAreTheNodesElseTheEndpointsButNoneForOmni() {
        assertEquals(listOf("a", "b"), ContextSummary(name = "c", nodes = listOf("a", "b", "a"), endpoints = listOf("e")).overviewTargets())
        assertEquals(listOf("e"), ContextSummary(name = "c", endpoints = listOf("e")).overviewTargets())
        assertEquals(emptyList<String>(), ContextSummary(name = "c", endpoints = listOf("e"), omni = true).overviewTargets())
    }
}
