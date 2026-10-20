package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.CheckupKind
import name.levis.ichor.model.CheckupReport
import name.levis.ichor.model.CheckupSectionId
import name.levis.ichor.model.pluginAlertObject
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
    fun findingsTheDataServicesAlertOnAreShownButNotAlertedTwice() {
        val sealed = TalosJson.decodeFromString(
            CheckupReport.serializer(),
            """
                {"status":"critical","sections":[{"id":"secrets","status":"critical","checked":3,"findings":[
                  {"kind":"sealedSecretNotSynced","severity":"critical","namespace":"shop","name":"legacy-cert"},
                  {"kind":"sealingKeyMissing","severity":"critical","name":"sealed-secrets-key","extra":"kube-system"},
                  {"kind":"externalSecretFailed","severity":"critical","namespace":"shop","name":"db"}]}]}
            """.trimIndent(),
        )
        // Still counted and shown on the checkup screen.
        assertEquals(3, sealed.count(CheckupSeverity.CRITICAL))
        assertEquals(mapOf("secrets|externalSecretFailed|shop/db" to DATA_CRITICAL), sealed.alertIssues())
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

    // The plugins section (go/ichorgo/kube_checkup_plugins.go, docs/plugins.md "Findings").
    private val plugins = TalosJson.decodeFromString(
        CheckupReport.serializer(),
        """
            {"status":"warning","sections":[{"id":"plugins","status":"warning","checked":4,"findings":[
              {"kind":"plugin","severity":"warning","namespace":"demo-lab","name":"weekend",
               "key":"plugin:kube-green:schedule-warning:demo-lab/weekend",
               "plugin":"kube-green","pluginName":"kube-green","pluginIcon":"kube-green",
               "id":"schedule-warning","title":"demo-lab/weekend","detail":"no wake-up time",
               "ref":{"group":"kube-green.com","version":"v1alpha1","resource":"sleepinfos","namespace":"demo-lab","name":"weekend"},
               "opens":"list"},
              {"kind":"plugin","severity":"critical","namespace":"demo-lab","name":"weekend",
               "key":"plugin:kube-green:no-target:demo-lab/weekend","plugin":"kube-green","pluginName":"kube-green",
               "id":"no-target","title":"demo-lab/weekend"},
              {"kind":"plugin","severity":"info","key":"plugin:kube-green:asleep:demo-lab/nightly","plugin":"kube-green",
               "id":"asleep","title":"demo-lab/nightly"}]}]}
        """.trimIndent(),
    )

    @Test
    fun aPluginFindingDecodesWithItsPluginsWords() {
        val f = plugins.sections.single().findings.first()
        assertEquals(CheckupKind.PLUGIN, f.kind)
        assertEquals(CheckupSectionId.PLUGINS, plugins.sections.single().id)
        assertEquals("kube-green", f.plugin)
        assertEquals("kube-green", f.pluginName)
        assertEquals("kube-green", f.pluginIcon)
        assertEquals("schedule-warning", f.id)
        assertEquals("demo-lab/weekend", f.title)
        assertEquals("no wake-up time", f.pluginDetail)
        assertEquals("sleepinfos", f.ref?.resource)
        assertEquals("list", f.opens)
        assertEquals("demo-lab/weekend", f.subject)
    }

    @Test
    fun aPluginFindingAlertsUnderItsStableKeyUnlessThePluginTrackIsWatched() {
        val sleep = "plugins|plugin|kube-green:schedule-warning:demo-lab/weekend"
        val target = "plugins|plugin|kube-green:no-target:demo-lab/weekend"
        // Two findings of one object keep apart; the note is never alerted.
        assertEquals(mapOf(sleep to DATA_WARNING, target to DATA_CRITICAL), plugins.alertIssues())
        assertEquals("demo-lab/weekend", pluginAlertObject("kube-green:schedule-warning:demo-lab/weekend"))

        // The plugin findings track reads them itself: the checkup leaves them to it, known ones included.
        assertTrue(plugins.alertIssues(pluginsWatched = true).isEmpty())
        val unread = plugins.copy(sections = listOf(plugins.sections.single().copy(error = "kube-green: sleepinfos: forbidden", findings = emptyList())))
        assertEquals(mapOf(sleep to DATA_WARNING), checkupIssuesWithGaps(unread, mapOf(sleep to DATA_WARNING)))
        assertTrue(checkupIssuesWithGaps(unread, mapOf(sleep to DATA_WARNING), pluginsWatched = true).isEmpty())

        // Its alert: the key's subject, the section and kind in the detail.
        val alert = evaluate(snap(emptyMap()), snap(mapOf(target to DATA_CRITICAL)), now).alerts.single()
        assertEquals("checkup:$target", alert.key)
        assertEquals("kube-green:no-target:demo-lab/weekend", alert.subject)
        assertEquals("plugins|plugin|critical", alert.detail)
    }
}
