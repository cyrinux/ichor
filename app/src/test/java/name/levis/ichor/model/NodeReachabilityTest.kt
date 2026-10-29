package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NodeReachabilityTest {

    private val overview = ClusterOverview(
        "lab",
        listOf(
            NodeOverview("10.0.0.4", "worker-0", reachable = true),
            NodeOverview(
                "10.0.0.5",
                "worker-1",
                reachable = false,
                error = "unreachable: no route to host",
                errorKind = "network",
                lastSeen = 1_000L,
            ),
        ),
    )

    @Test
    fun theOverviewSaysWhichNodeAnswers() {
        assertEquals(NodeReachability(true), overview.reachabilityOf("10.0.0.4"))
        assertEquals(
            NodeReachability(false, "network", "unreachable: no route to host", 1_000L),
            overview.reachabilityOf("10.0.0.5"),
        )
        assertNull(overview.reachabilityOf("10.0.0.9"))
    }

    @Test
    fun onlyADialFailureMakesANodeUnreachable() {
        assertEquals(
            NodeReachability(false, "network", "unreachable: connection refused"),
            unreachableFrom("unreachable: connection refused"),
        )
        assertNull(unreachableFrom("permission denied (talosconfig role too limited): x"))
        assertNull(unreachableFrom("TLS error: bad certificate"))
        assertNull(unreachableFrom(null))
    }

    @Test
    fun aLiveFailureWinsOverAnOverviewThatSawTheNodeUp() {
        val live = unreachableFrom("unreachable: no route to host")
        assertEquals(live, mergeReachability(NodeReachability(true), live))
        assertEquals(live, mergeReachability(null, live))
        // The overview already knew, with when it last answered: kept.
        val known = overview.reachabilityOf("10.0.0.5")
        assertEquals(known, mergeReachability(known, live))
        assertEquals(known, mergeReachability(known, null))
        assertNull(mergeReachability(null, null))
    }

    @Test
    fun unknownIsNeverDown() {
        val unknown: NodeReachability? = null
        assertFalse(unknown.down)
        assertFalse(NodeReachability(true).down)
        assertTrue(NodeReachability(false).down)
    }

    @Test
    fun theTailOfTheWindowWithoutACheckIsMeasured() {
        val hour = HISTORY_TAIL_GAP_MILLIS
        val span = HistoryNodeSpan("10.0.0.5", uptimePercent = 100.0, intervals = listOf(HistoryState(HISTORY_READY, 0, 10 * hour)))
        assertEquals(3 * hour, HistoryQuery(from = 0, to = 13 * hour).uncheckedTail(span))
        // Within one check period of the end: nothing to say.
        assertNull(HistoryQuery(from = 0, to = 10 * hour + hour / 2).uncheckedTail(span))
        assertNull(HistoryQuery(from = 0, to = 13 * hour).uncheckedTail(HistoryNodeSpan("10.0.0.6")))
    }
}
