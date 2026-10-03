package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.*
import org.junit.Test

class IncidentEvidenceTest {
    private fun entry(detail: String) = IncidentEntry("1", 1, "node", "metrics", "node", detail, "info")
    @Test fun truncatedLegacyDetailsRemainUnavailable() {
        assertNull(entry("{\"counters\":{…").incidentMetrics())
        assertNull(entry("{\"ready\":tr").incidentDetail())
    }
    @Test fun structuredMetricsSurviveTruncatedRawDetails() {
        val raw = """{"id":"1","at":1,"node":"node","kind":"metrics","subject":"node","severity":"info","detail":"{…","metrics":{"wait":0.45,"steal":0,"network":[],"disks":[],"errors":{"disk":"unavailable"}},"metricsOmitted":2}"""
        val entry = TalosJson.decodeFromString(IncidentEntry.serializer(), raw)
        assertEquals(0.45, entry.incidentMetrics()!!.wait, 0.001)
        assertEquals("unavailable", entry.incidentMetrics()!!.errors["disk"])
        assertEquals(2, entry.metricsOmitted)
    }
    @Test fun completeLegacyRatesStillDecode() {
        val entry = entry("""{"rates":{"wait":4,"steal":1,"network":[],"disks":[],"errors":{}}}""")
        assertEquals(4.0, entry.incidentMetrics()!!.wait, 0.001)
        assertNull(entry.metrics)
    }
}
