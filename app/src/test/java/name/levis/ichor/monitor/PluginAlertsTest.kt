package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.PluginFindingsError
import name.levis.ichor.model.PluginFindingsReport
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.linkedPlugin
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PluginAlertsTest {
    private val now = 1_800_000_000_000L
    private val farCert = now / 1000 + 365L * 86_400

    private val sleepKey = "plugin:kube-green:schedule-warning:demo-lab/weekend"
    private val secretKey = "plugin:external-secrets:target-missing:demo-lab/registry-pull"
    private val widgetKey = "plugin:demo-widgets:widget-broken:demo-lab/gear"

    // A KubePluginFindings answer (go/ichorgo/plugin_findings.go, docs/plugins.md "Findings").
    private val report = TalosJson.decodeFromString(
        PluginFindingsReport.serializer(),
        """
            {"plugins": 3, "futureField": true,
             "findings": [
               {"severity": "critical", "checkupNative": true, "key": "$secretKey",
                "plugin": "external-secrets", "pluginName": "External Secrets", "pluginIcon": "external-secrets",
                "id": "target-missing", "title": "demo-lab/registry-pull", "detail": "its Secret is missing",
                "ref": {"group": "external-secrets.io", "version": "v1", "resource": "externalsecrets", "namespace": "demo-lab", "name": "registry-pull"},
                "opens": "secrets"},
               {"severity": "warning", "key": "$sleepKey",
                "plugin": "kube-green", "pluginName": "kube-green", "pluginIcon": "kube-green",
                "id": "schedule-warning", "title": "demo-lab/weekend", "detail": "no wake-up time",
                "ref": {"group": "kube-green.com", "version": "v1alpha1", "resource": "sleepinfos", "namespace": "demo-lab", "name": "weekend"},
                "opens": "list"},
               {"severity": "info", "key": "plugin:kube-green:asleep:demo-lab/nightly",
                "plugin": "kube-green", "pluginName": "kube-green", "id": "asleep", "title": "demo-lab/nightly"}],
             "errors": [{"plugin": "demo-widgets", "pluginName": "Demo Widgets", "resource": "widgets.widgets.example.com",
                "message": "Kubernetes API: permission denied: forbidden"}]}
        """.trimIndent(),
    )

    private fun snap(issues: Map<String, String>?, watched: Boolean = true) = ClusterSnapshot(
        context = "lab",
        takenAt = now,
        nodes = mapOf("a" to NodeState("host-a", NodeHealth.READY)),
        etcdChecked = true,
        certNotAfter = farCert,
        pluginWatched = watched,
        pluginChecked = watched && issues != null,
        pluginIssues = issues.orEmpty(),
    )

    @Test
    fun theGoJsonDecodes() {
        assertEquals(3, report.plugins)
        assertEquals(3, report.findings.size)
        val secret = report.findings[0]
        assertTrue(secret.checkupNative)
        assertEquals("External Secrets", secret.pluginName)
        assertEquals("its Secret is missing", secret.detail)
        assertEquals("registry-pull", secret.ref?.name)
        assertEquals("secrets", secret.opens)
        assertEquals(false, report.findings[1].checkupNative)
        assertNull(report.findings[2].ref)
        assertEquals(0, report.truncated)
        assertEquals(PluginFindingsError("demo-widgets", "Demo Widgets", "widgets.widgets.example.com", "Kubernetes API: permission denied: forbidden"), report.errors.single())
    }

    @Test
    fun warningsAndCriticalOnesAreIssuesKeyedByTheFindingsKey() {
        val issues = pluginIssuesOf(report, checkupWatched = false)
        // The note (info) is shown on the plugin's screen, never notified.
        assertEquals(setOf(secretKey, sleepKey), issues.keys)
        assertEquals(DATA_CRITICAL, pluginSeverity(issues.getValue(secretKey)))
        assertEquals(DATA_WARNING, pluginSeverity(issues.getValue(sleepKey)))
        assertEquals(
            PluginIssue(DATA_WARNING, "kube-green", "kube-green", "demo-lab/weekend", "no wake-up time"),
            PluginIssue.parse(issues.getValue(sleepKey)),
        )
    }

    @Test
    fun aCheckupNativeFindingIsLeftToTheCheckupOnlyWhenItIsWatched() {
        assertEquals(setOf(sleepKey), pluginIssuesOf(report, checkupWatched = true).keys)
        assertTrue(secretKey in pluginIssuesOf(report, checkupWatched = false))
    }

    @Test
    fun aPluginThatCouldNotBeReadKeepsItsKnownIssuesTheOthersClear() {
        val known = mapOf(
            widgetKey to PluginIssue(DATA_CRITICAL, "demo-widgets", "Demo Widgets", "demo-lab/gear").encode(),
            "plugin:kube-green:schedule-warning:demo-lab/gone" to PluginIssue(DATA_WARNING, "kube-green").encode(),
        )
        val issues = pluginIssuesWithGaps(report, known, checkupWatched = false)
        assertEquals(setOf(secretKey, sleepKey, widgetKey), issues.keys)
        assertEquals("demo-widgets", pluginOfKey(widgetKey))
    }

    @Test
    fun aTruncatedReadKeepsWhatItMayHaveCut() {
        val known = mapOf("plugin:kube-green:schedule-warning:demo-lab/other" to PluginIssue(DATA_WARNING, "kube-green").encode())
        val issues = pluginIssuesWithGaps(report.copy(truncated = 4, errors = emptyList()), known, checkupWatched = false)
        assertTrue("plugin:kube-green:schedule-warning:demo-lab/other" in issues)
    }

    @Test
    fun aCriticalFindingAlertsOnceAWarningOnTheSecondCheckAndBothClear() {
        val issues = pluginIssuesOf(report, checkupWatched = true)
        // The first read is a silent baseline.
        val baseline = evaluate(snap(emptyMap()), snap(emptyMap()), now).next

        val first = evaluate(baseline, snap(issues), now)
        // The warning waits for a second check in a row.
        assertTrue(first.alerts.isEmpty())
        val second = evaluate(first.next, snap(issues), now)
        val alert = second.alerts.single()
        assertEquals(sleepKey, alert.key)
        assertEquals(AlertKind.PLUGIN_PROBLEM, alert.kind)
        assertEquals("demo-lab/weekend", alert.subject)
        // Seen again: no duplicate.
        assertTrue(evaluate(second.next, snap(issues), now).alerts.isEmpty())

        val critical = mapOf(widgetKey to PluginIssue(DATA_CRITICAL, "demo-widgets", "Demo Widgets", "demo-lab/gear").encode())
        val raised = evaluate(second.next, snap(issues + critical), now)
        assertEquals(listOf(widgetKey), raised.alerts.map { it.key })

        val cleared = evaluate(raised.next, snap(emptyMap()), now)
        assertEquals(setOf(sleepKey, widgetKey), cleared.alerts.map { it.key }.toSet())
        assertTrue(cleared.alerts.all { it.kind == AlertKind.PLUGIN_OK && !it.problem })
    }

    @Test
    fun anUnreadableCallKeepsWhatWasKnown() {
        val critical = mapOf(widgetKey to PluginIssue(DATA_CRITICAL, "demo-widgets").encode())
        val known = evaluate(snap(emptyMap()), snap(critical), now).next
        val unread = evaluate(known, snap(null), now)
        assertTrue(unread.alerts.isEmpty())
        assertEquals(critical, unread.next.pluginIssues)
        // Turning watching off forgets them, silently.
        val off = evaluate(known, snap(null, watched = false), now)
        assertTrue(off.alerts.isEmpty())
        assertTrue(off.next.pluginIssues.isEmpty())
    }

    @Test
    fun anAlertOpensThePluginOnTheDataChannel() {
        val value = PluginIssue(DATA_CRITICAL, "kube-green", "kube-green", "demo-lab/weekend").encode()
        val alert = Alert(sleepKey, AlertKind.PLUGIN_PROBLEM, problem = true, subject = "demo-lab/weekend", detail = value)
        assertEquals(ShareTarget.plugin("kube-green"), alert.shareTarget())
        assertEquals(AlertChannel.DATA, alert.channel)
        // A data services link of kind "plugin-<id>", as iOS reads it; a data service's tab names no plugin.
        assertEquals(ShareTarget.DATA, alert.shareTarget()?.target)
        assertEquals("plugin-kube-green", alert.shareTarget()?.kind)
        assertEquals("kube-green", alert.shareTarget()?.linkedPlugin)
        assertNull(ShareTarget.dataServices("longhorn").linkedPlugin)
        // An unreadable detail still names the plugin, from the key.
        assertEquals(ShareTarget.plugin("kube-green"), alert.copy(detail = "?").shareTarget())
    }

    @Test
    fun theOpenPluginIssuesAreInTheHistory() {
        val value = PluginIssue(DATA_WARNING, "kube-green", "kube-green", "demo-lab/weekend").encode()
        val entry = openAlertsOf(snap(mapOf(sleepKey to value)), null, now).single { it.track == "plugin" }
        assertEquals(sleepKey, entry.key)
        assertEquals(DATA_WARNING, entry.severity)
        assertEquals("kube-green: demo-lab/weekend", entry.title)
    }
}
