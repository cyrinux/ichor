package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

class AuditEventsTest {
    private val summary = ConfigSummary(
        current = "lab",
        contexts = listOf(
            ContextSummary("lab", fingerprint = "fp-lab"),
            ContextSummary("prod", fingerprint = "fp-prod"),
            ContextSummary("demo", fingerprint = "fp-demo", demo = true),
            ContextSummary("unnamed"),
        ),
    )

    @Test
    fun theAppWideSwitchOnKeepsEveryRealClusterRecording() {
        assertEquals(setOf("fp-lab", "fp-prod"), seededAuditEventClusters(true, summary))
    }

    @Test
    fun theSwitchOffOrNeverSetRecordsNowhere() {
        assertEquals(emptySet<String>(), seededAuditEventClusters(false, summary))
        assertEquals(emptySet<String>(), seededAuditEventClusters(null, summary))
    }

    @Test
    fun noClusterNothingToSeed() {
        assertEquals(emptySet<String>(), seededAuditEventClusters(true, ConfigSummary("", emptyList())))
    }

    @Test
    fun goGetsOneFingerprintPerLineInAStableOrder() {
        assertEquals("fp-a\nfp-b", auditEventClustersArgument(setOf("fp-b", "fp-a")))
        assertEquals("", auditEventClustersArgument(emptySet()))
    }
}
