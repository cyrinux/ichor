package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AuditTest {

    // A KubeAuditAnalysis answer (go/ichorgo/kube_audit.go), cut down from the demo's.
    private val report = TalosJson.decodeFromString(
        AuditReport.serializer(),
        """
            {"nodes":[{"node":"192.0.2.10","bytes":6400000,"events":512000,"last":1791234598267},{"node":"192.0.2.12","error":"permission denied"}],
             "from":1791233698267,"to":1791234598267,"seconds":900,"requests":1324,
             "findings":[
              {"kind":"listLoop","severity":"critical","actor":{"user":"system:serviceaccount:monitoring:pod-exporter","agent":"pod-exporter","kind":"serviceAccount","namespace":"monitoring","name":"pod-exporter"},"count":450,"rate":0.5,"verb":"list","resource":"pods","value":2},
              {"kind":"hotObject","severity":"warning","actor":{"user":"system:serviceaccount:cnpg:manager","agent":"manager","kind":"serviceAccount","namespace":"cnpg","name":"manager"},"count":300,"verb":"update","resource":"clusters/status","value":9,"objects":14,"examples":["a/db","b/db"]},
              {"kind":"staleLog","severity":"critical","actor":{},"count":0,"name":"192.0.2.12","value":86400},
              {"kind":"fromTheFuture","severity":"new"}],
             "actors":[{"actor":{"user":"system:node:w1","agent":"kubelet","kind":"node","name":"w1"},"requests":200,"rate":0.2,"share":0.15,"topVerb":"get","topResource":"nodes","topCount":90}]}
        """.trimIndent(),
    )

    @Test
    fun decodesAndRanks() {
        assertEquals(4, report.findings.size)
        assertEquals(AuditSeverity.CRITICAL, report.findings[0].level)
        assertEquals(AuditSeverity.INFO, report.findings[3].level)
        assertEquals("permission denied", report.nodes[1].error)
        assertEquals(listOf("a/db", "b/db"), report.findings[1].examples)
    }

    @Test
    fun namesActorsAndTargets() {
        assertEquals("monitoring/pod-exporter", report.findings[0].actor.label)
        assertEquals("w1", report.actors[0].actor.label)
        assertEquals("pods", report.findings[0].target)
        assertEquals("clusters/status", report.findings[1].target)
        assertEquals("secrets tools/restic", AuditFinding("missingObject", resource = "secrets", namespace = "tools", name = "restic").target)
        assertEquals("configmaps (media)", AuditFinding("forbidden", resource = "configmaps", namespace = "media").target)
    }

    @Test
    fun serverFindingsHaveNoClient() {
        assertTrue(report.findings[2].aboutServer)
        assertFalse(report.findings[0].aboutServer)
    }

    @Test
    fun formatsIntervals() {
        assertEquals("0.5 s", formatSeconds(0.5))
        assertEquals("26 s", formatSeconds(26.2))
        assertEquals("4 min", formatSeconds(240.0))
        assertEquals("25.1 h", formatSeconds(90400.0))
        assertEquals("6.4 MB", formatMegabytes(6_400_000))
    }
}
