package name.levis.ichor.ui.insights

import name.levis.ichor.model.IncidentEntry
import name.levis.ichor.model.ServiceHealth
import org.junit.Assert.assertEquals
import org.junit.Test

class IncidentTimelineTest {
    private fun service(id: String, node: String, health: String = "healthy", at: Long = 0) =
        IncidentEntry(id, at, node, "service", id, """{"state":"Running","health":"$health"}""", "info")

    private fun status(id: String, reachable: Boolean, ready: Boolean, severity: String = "info") =
        IncidentEntry(id, 0, "10.0.0.1", "status", "node-a", """{"reachable":$reachable,"ready":$ready,"stage":"running","error":"","conditions":[]}""", severity)

    @Test fun relativeTimeFormatsMinutesHoursAndClockSkew() {
        assertEquals("+00:00", relativeTime(0))
        assertEquals("+00:07", relativeTime(7_400))
        assertEquals("+09:59", relativeTime(599_000))
        assertEquals("+1:00:05", relativeTime(3_605_000))
        assertEquals("-00:02", relativeTime(-2_000))
    }

    @Test fun healthReadsTheEvidence() {
        assertEquals(ServiceHealth.CRITICAL, evidenceHealth(status("1", reachable = false, ready = false, severity = "warning")))
        assertEquals(ServiceHealth.WARNING, evidenceHealth(status("2", reachable = true, ready = false, severity = "warning")))
        assertEquals(ServiceHealth.OK, evidenceHealth(status("3", reachable = true, ready = true)))
        assertEquals(ServiceHealth.WARNING, evidenceHealth(service("etcd", "n1", health = "unhealthy")))
        assertEquals(ServiceHealth.OK, evidenceHealth(service("etcd", "n1")))
        assertEquals(ServiceHealth.CRITICAL, evidenceHealth(IncidentEntry("e", 0, "n1", "error", "services", "timeout", "warning")))
        assertEquals(ServiceHealth.WARNING, evidenceHealth(IncidentEntry("l", 0, "n1", "link", "eth0", """{"before":"up","after":"down"}""", "info")))
    }

    @Test fun consecutiveHealthyServicesOfOneNodeCollapse() {
        val entries = listOf(
            service("a", "n1", at = 3_000),
            service("b", "n1", at = 2_000),
            service("c", "n1", at = 1_000),
            service("d", "n2"),
            service("e", "n2", health = "unhealthy"),
            service("f", "n2"),
        )
        val items = incidentTimeline(entries, startedAt = 0)
        assertEquals(4, items.size)
        val run = items[0] as TimelineItem.HealthyRun
        assertEquals(listOf("a", "b", "c"), run.rows.map { it.entry.id })
        assertEquals("n1", run.node)
        assertEquals("+00:03", run.rows.first().offset)
        assertEquals("d", (items[1] as TimelineItem.Single).row.entry.id)
        assertEquals(ServiceHealth.WARNING, (items[2] as TimelineItem.Single).row.health)
        assertEquals("f", (items[3] as TimelineItem.Single).row.entry.id)
    }

    @Test fun otherKindsAndBrokenRunsStaySingle() {
        val entries = listOf(
            service("a", "n1"),
            status("s", reachable = true, ready = true),
            service("b", "n1"),
        )
        val items = incidentTimeline(entries, startedAt = 0)
        assertEquals(listOf("a", "s", "b"), items.map { it.key })
    }
}
