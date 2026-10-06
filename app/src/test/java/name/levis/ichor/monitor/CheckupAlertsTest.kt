package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.CheckupReport
import name.levis.ichor.model.CheckupSeverity
import name.levis.ichor.model.CheckupStatus
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.alertIssues
import name.levis.ichor.model.count
import name.levis.ichor.model.shownSections
import name.levis.ichor.model.state
import name.levis.ichor.model.subject
import name.levis.ichor.model.verdict
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class CheckupAlertsTest {
    private val now = 1_800_000_000_000L
    private val farCert = now / 1000 + 365L * 86_400

    // A KubeCheckup answer (go/ichorgo/kube_checkup.go), cut down.
    private val report = TalosJson.decodeFromString(
        CheckupReport.serializer(),
        """
            {"status":"critical","kubeVersion":"v1.34.1","sections":[
              {"id":"workloads","status":"critical","checked":8,"findings":[
                {"kind":"podCrashLoop","severity":"critical","namespace":"shop","name":"web-1","count":9},
                {"kind":"podOOMKilled","severity":"warning","namespace":"shop","name":"cache-0","count":2},
                {"kind":"podFailed","severity":"info","namespace":"shop","name":"old"}]},
              {"id":"events","status":"ok","checked":3,"findings":[{"kind":"event","severity":"info","namespace":"shop","name":"web-1","extra":"Pod"}]},
              {"id":"nodes","status":"warning","checked":2,"findings":[{"kind":"nodeCordoned","severity":"warning","name":"n2"}]},
              {"id":"storage","status":"unknown","error":"forbidden","checked":0,"findings":[]},
              {"id":"helm","status":"absent","checked":0,"findings":[]}],
             "nodes":[],"volumes":[],"releases":[],"futureField":1}
        """.trimIndent(),
    )

    private fun snap(issues: Map<String, String>?, watched: Boolean = true, context: String = "lab") = ClusterSnapshot(
        context = context,
        takenAt = now,
        nodes = mapOf("a" to NodeState("host-a", NodeHealth.READY)),
        etcdChecked = true,
        certNotAfter = farCert,
        checkupWatched = watched,
        checkupChecked = watched && issues != null,
        checkupIssues = issues.orEmpty(),
    )

    @Test
    fun reportReadsAsTheScreenShowsIt() {
        assertEquals(CheckupStatus.CRITICAL, report.verdict)
        assertEquals(listOf("workloads", "events", "nodes", "storage"), report.shownSections.map { it.id })
        assertEquals(CheckupStatus.UNKNOWN, report.sections[3].state)
        assertEquals(1, report.count(CheckupSeverity.CRITICAL))
        assertEquals(2, report.count(CheckupSeverity.WARNING))
        assertEquals("shop/web-1", report.sections[0].findings[0].subject)
        assertEquals("n2", report.sections[2].findings[0].subject)
    }

    @Test
    fun onlyCriticalAndWarningFindingsAlertAndEventsNever() {
        assertEquals(
            mapOf(
                "workloads|podCrashLoop|shop/web-1" to DATA_CRITICAL,
                "workloads|podOOMKilled|shop/cache-0" to DATA_WARNING,
                "nodes|nodeCordoned|n2" to DATA_WARNING,
            ),
            report.alertIssues(),
        )
    }

    @Test
    fun anUnreadSectionKeepsWhatWasKnown() {
        val known = mapOf("storage|volumeFull|shop/data" to DATA_CRITICAL, "workloads|podCrashLoop|shop/gone" to DATA_CRITICAL)
        val issues = checkupIssuesWithGaps(report, known)
        assertEquals(DATA_CRITICAL, issues["storage|volumeFull|shop/data"])
        assertTrue("workloads|podCrashLoop|shop/gone" !in issues)
        assertEquals(emptyMap<String, String>(), knownCheckupIssues(snap(known, context = "other"), "lab"))
        assertEquals(known, knownCheckupIssues(snap(known), "lab"))
    }

    @Test
    fun criticalAlertsAtOnceWarningOnTheSecondCheckAndClearingOnce() {
        val crash = "workloads|podCrashLoop|shop/web-1"
        val cordon = "nodes|nodeCordoned|n2"

        // The first check is a silent baseline.
        val base = evaluate(null, snap(emptyMap()), now)
        assertTrue(base.alerts.isEmpty())

        val first = evaluate(base.next, snap(mapOf(crash to DATA_CRITICAL, cordon to DATA_WARNING)), now)
        val alert = first.alerts.single()
        assertEquals(AlertKind.CHECKUP_PROBLEM, alert.kind)
        assertEquals("shop/web-1", alert.subject)
        assertEquals("workloads|podCrashLoop|critical", alert.detail)
        assertEquals(listOf(cordon), first.next.checkupPending)

        val second = evaluate(first.next, snap(mapOf(crash to DATA_CRITICAL, cordon to DATA_WARNING)), now)
        assertEquals(listOf("n2"), second.alerts.map { it.subject })

        val cleared = evaluate(second.next, snap(mapOf(cordon to DATA_WARNING)), now)
        assertEquals(AlertKind.CHECKUP_OK, cleared.alerts.single().kind)
        assertEquals("shop/web-1", cleared.alerts.single().subject)

        // Unreadable: nothing clears. Turned off: forgotten without a word.
        assertTrue(evaluate(cleared.next, snap(null), now).alerts.isEmpty())
        val off = evaluate(cleared.next, snap(emptyMap(), watched = false), now)
        assertTrue(off.alerts.isEmpty())
        assertTrue(off.next.checkupIssues.isEmpty())
    }
}
