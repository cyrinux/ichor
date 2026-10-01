package name.levis.talosmobile.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class EventsTest {

    private fun ev(
        id: String,
        at: Long,
        node: String = "10.0.0.1",
        kind: String = "address",
        subject: String = "eth0",
        action: String = "",
        message: String = "10.0.0.1/24",
        severity: String = "info",
    ) = TalosEvent(node, id, at, kind, subject, action, message, severity)

    @Test
    fun insertsNewestFirstWhateverTheArrivalOrder() {
        val events = listOf(ev("b", 2), ev("c", 3), ev("a", 1))
            .fold(emptyList<TalosEvent>()) { acc, e -> acc.withEvent(e) }
        assertEquals(listOf("c", "b", "a"), events.map { it.id })
    }

    @Test
    fun ignoresReplayedEventsOfTheSameNode() {
        val events = listOf(ev("a", 1)).withEvent(ev("a", 1))
        assertEquals(1, events.size)
        // Same id on another node is another event.
        assertEquals(2, events.withEvent(ev("a", 1, node = "10.0.0.2")).size)
    }

    @Test
    fun keepsAtMostCapNewestEvents() {
        val events = (1..10L).fold(emptyList<TalosEvent>()) { acc, i -> acc.withEvent(ev("e$i", i), cap = 5) }
        assertEquals(listOf("e10", "e9", "e8", "e7", "e6"), events.map { it.id })
        // An event older than all kept ones is dropped right away.
        assertEquals(events, events.withEvent(ev("old", 0), cap = 5))
    }

    @Test
    fun collapsesConsecutiveIdenticalEvents() {
        // Newest first: three identical address events, then a different one, then the same again.
        val events = listOf(
            ev("5", 50),
            ev("4", 40),
            ev("3", 30),
            ev("2", 20, kind = "service", subject = "kubelet", action = "running", message = ""),
            ev("1", 10),
        )
        val rows = timelineRows(events, EventFilter.ALL)
        assertEquals(listOf(3, 1, 1), rows.map { it.count })
        assertEquals("5", rows[0].event.id)
        assertEquals(30L, rows[0].oldestAt)
    }

    @Test
    fun doesNotCollapseAcrossNodesOrMessages() {
        val rows = timelineRows(listOf(ev("2", 20), ev("1", 10, node = "10.0.0.2")), EventFilter.ALL)
        assertEquals(2, rows.size)
        val rows2 = timelineRows(listOf(ev("2", 20), ev("1", 10, message = "10.0.0.9/24")), EventFilter.ALL)
        assertEquals(2, rows2.size)
    }

    @Test
    fun collapsesAfterFilteringOutWhatWasBetween() {
        val events = listOf(
            ev("3", 30, kind = "service", subject = "etcd", action = "failed", severity = "error"),
            ev("2", 20),
            ev("1", 10, kind = "service", subject = "etcd", action = "failed", severity = "error"),
        )
        assertEquals(3, timelineRows(events, EventFilter.ALL).size)
        val problems = timelineRows(events, EventFilter.PROBLEMS)
        assertEquals(1, problems.size)
        assertEquals(2, problems[0].count)
    }

    @Test
    fun filters() {
        assertTrue(ev("1", 1, severity = "warning").matches(EventFilter.PROBLEMS))
        assertTrue(ev("1", 1, severity = "error").matches(EventFilter.PROBLEMS))
        assertFalse(ev("1", 1).matches(EventFilter.PROBLEMS))
        assertTrue(ev("1", 1, kind = "service").matches(EventFilter.SERVICES))
        assertFalse(ev("1", 1, kind = "task").matches(EventFilter.SERVICES))
        listOf("sequence", "phase", "task").forEach { assertTrue(it, ev("1", 1, kind = it).matches(EventFilter.BOOT)) }
        assertFalse(ev("1", 1, kind = "machine").matches(EventFilter.BOOT))
        assertTrue(ev("1", 1, kind = "other").matches(EventFilter.ALL))
    }
}
