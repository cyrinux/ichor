package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.AmAlert
import name.levis.ichor.model.AmAlerts
import name.levis.ichor.model.AmGroup
import name.levis.ichor.model.AmSeverity
import name.levis.ichor.model.AmState
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.ShareTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertmanagerAlertsTest {
    private val now = 1_800_000_000_000L
    private val farCert = now / 1000 + 365L * 86_400

    private val disk = "critical|NodeFilesystemAlmostOutOfSpace|worker-1"
    private val crashLoop = "warning|KubePodCrashLooping|shop/api-7d9f"

    private fun snap(issues: Map<String, String>?, watched: Boolean = true, context: String = "lab") = ClusterSnapshot(
        context = context,
        takenAt = now,
        nodes = mapOf("a" to NodeState("host-a", NodeHealth.READY)),
        etcdChecked = true,
        certNotAfter = farCert,
        amWatched = watched,
        amChecked = watched && issues != null,
        amIssues = issues.orEmpty(),
    )

    private fun alert(fp: String, name: String, severity: String, labels: Map<String, String> = emptyMap(), state: String = AmState.ACTIVE) =
        AmAlert(fingerprint = fp, alertname = name, severity = severity, labels = labels + ("alertname" to name), state = state)

    @Test
    fun onlyFiringAlertsAboveInfoAreIssues() {
        val alerts = AmAlerts(
            groups = listOf(
                AmGroup("NodeFilesystemAlmostOutOfSpace", AmSeverity.CRITICAL, alerts = listOf(alert("f1", "NodeFilesystemAlmostOutOfSpace", AmSeverity.CRITICAL, mapOf("instance" to "worker-1")))),
                AmGroup("KubePodCrashLooping", AmSeverity.WARNING, alerts = listOf(alert("f2", "KubePodCrashLooping", AmSeverity.WARNING, mapOf("namespace" to "shop", "pod" to "api-7d9f")))),
                AmGroup("Custom|Rule", AmSeverity.OTHER, alerts = listOf(alert("f3", "Custom|Rule", AmSeverity.OTHER))),
                // Never: an info alert (Watchdog), a silenced or inhibited one, an alert without fingerprint.
                AmGroup("Watchdog", AmSeverity.INFO, alerts = listOf(alert("f4", "Watchdog", AmSeverity.INFO))),
                AmGroup("CPUThrottlingHigh", AmSeverity.CRITICAL, alerts = listOf(alert("f5", "CPUThrottlingHigh", AmSeverity.CRITICAL, state = AmState.SUPPRESSED))),
                AmGroup("Nameless", AmSeverity.CRITICAL, alerts = listOf(alert("", "Nameless", AmSeverity.CRITICAL))),
            ),
        )
        assertEquals(
            mapOf("f1" to disk, "f2" to crashLoop, "f3" to "warning|Custom/Rule|"),
            amIssuesOf(alerts),
        )
    }

    @Test
    fun firstCheckIsASilentBaseline() {
        val result = evaluate(null, snap(mapOf("f1" to disk)), now)
        assertTrue(result.alerts.isEmpty())
        assertEquals(mapOf("f1" to disk), result.next.amIssues)
    }

    @Test
    fun criticalAlertsAtOnce() {
        val result = evaluate(snap(emptyMap()), snap(mapOf("f1" to disk)), now)
        val alert = result.alerts.single()
        assertEquals("am:f1", alert.key)
        assertEquals(AlertKind.AM_FIRING, alert.kind)
        assertEquals("NodeFilesystemAlmostOutOfSpace", alert.subject)
        assertEquals(disk, alert.detail)
        assertTrue(alert.problem)
        assertEquals(AlertChannel.CLUSTER, alert.channel)
        assertEquals(ShareTarget.screen(ShareTarget.ALERTS), alert.shareTarget())
    }

    @Test
    fun warningNeedsTwoChecksInARow() {
        val first = evaluate(snap(emptyMap()), snap(mapOf("f2" to crashLoop)), now)
        assertTrue(first.alerts.isEmpty())
        assertEquals(listOf("f2"), first.next.amPending)

        val second = evaluate(first.next, snap(mapOf("f2" to crashLoop)), now)
        assertEquals(listOf("am:f2"), second.alerts.map { it.key })
        assertTrue(second.next.amPending.isEmpty())

        assertTrue(evaluate(second.next, snap(mapOf("f2" to crashLoop)), now).alerts.isEmpty())
    }

    @Test
    fun aShortWarningNeverAlerts() {
        val first = evaluate(snap(emptyMap()), snap(mapOf("f2" to crashLoop)), now)
        val gone = evaluate(first.next, snap(emptyMap()), now)
        assertTrue(gone.alerts.isEmpty())
        assertTrue(gone.next.amPending.isEmpty())
    }

    @Test
    fun resolvedIsNotifiedOnce() {
        val known = snap(emptyMap()).copy(amIssues = mapOf("f1" to disk))
        val result = evaluate(known, snap(emptyMap()), now)
        val alert = result.alerts.single()
        assertEquals(AlertKind.AM_RESOLVED, alert.kind)
        assertEquals("NodeFilesystemAlmostOutOfSpace", alert.subject)
        assertFalse(alert.problem)
        assertTrue(result.next.amIssues.isEmpty())
        assertTrue(evaluate(result.next, snap(emptyMap()), now).alerts.isEmpty())
    }

    @Test
    fun escalationAlertsAgain() {
        val known = snap(emptyMap()).copy(amIssues = mapOf("f2" to crashLoop))
        val worse = "critical|KubePodCrashLooping|shop/api-7d9f"
        assertEquals(listOf(AlertKind.AM_FIRING), evaluate(known, snap(mapOf("f2" to worse)), now).alerts.map { it.kind })
    }

    @Test
    fun anUnreadableCheckKeepsWhatWasKnown() {
        val known = snap(emptyMap()).copy(amIssues = mapOf("f1" to disk), amPending = listOf("f2"))
        val result = evaluate(known, snap(issues = null), now)
        assertTrue(result.alerts.isEmpty())
        assertEquals(known.amIssues, result.next.amIssues)
        assertEquals(known.amPending, result.next.amPending)
        assertTrue(result.next.amChecked)
    }

    @Test
    fun turningWatchingOffForgetsAndOnAgainIsABaseline() {
        val known = snap(emptyMap()).copy(amIssues = mapOf("f1" to disk))
        val off = evaluate(known, snap(issues = null, watched = false), now)
        assertTrue(off.alerts.isEmpty())
        assertTrue(off.next.amIssues.isEmpty())

        val on = evaluate(off.next, snap(mapOf("f9" to disk)), now)
        assertTrue(on.alerts.isEmpty())
    }

    @Test
    fun anotherClusterIsABaseline() {
        val result = evaluate(snap(emptyMap(), context = "lab"), snap(mapOf("f1" to disk), context = "prod"), now)
        assertTrue(result.alerts.isEmpty())
    }

    @Test
    fun anOlderSnapshotDecodesWithoutAlertmanager() {
        val json = """{"context":"lab","takenAt":1,"nodes":{},"gitopsWatched":true,"gitopsChecked":true}"""
        val snapshot = TalosJson.decodeFromString(ClusterSnapshot.serializer(), json)
        assertFalse(snapshot.amWatched)
        assertTrue(snapshot.amIssues.isEmpty())
    }

    @Test
    fun snapshotOfReadsAlertmanagerOnlyWhenWatched() {
        val overview = ClusterOverview(context = "lab", nodes = emptyList())
        val issues = mapOf("f1" to disk)
        assertTrue(snapshotOf(overview, null, 0, now, amWatched = true, amIssues = issues).amChecked)
        assertFalse(snapshotOf(overview, null, 0, now, amWatched = true, amIssues = null).amChecked)
        assertTrue(snapshotOf(overview, null, 0, now, amWatched = false, amIssues = issues).amIssues.isEmpty())
    }

    @Test
    fun aTruncatedReadKeepsKnownIssuesAndStillAlertsNewOnes() {
        val known = mapOf("f1" to disk, "f2" to crashLoop)
        val newDisk = alert("f7", "NodeFilesystemAlmostOutOfSpace", AmSeverity.CRITICAL, mapOf("instance" to "worker-2"))
        val partial = AmAlerts(groups = listOf(AmGroup("NodeFilesystemAlmostOutOfSpace", AmSeverity.CRITICAL, alerts = listOf(newDisk))), truncated = true)
        val issues = amIssuesWithGaps(partial, known)
        assertEquals(setOf("f1", "f2", "f7"), issues.keys)

        val result = evaluate(snap(known), snap(issues), now)
        // No "resolved" for f1 or f2, the new critical one notifies.
        assertEquals(listOf("am:f7"), result.alerts.map { it.key })
        assertEquals(listOf(AlertKind.AM_FIRING), result.alerts.map { it.kind })

        // A complete read does resolve what is gone.
        assertEquals(setOf("f7"), amIssuesWithGaps(partial.copy(truncated = false), known).keys)
        // Only the same watched cluster's issues carry over.
        assertEquals(known, knownAmIssues(snap(known), "lab"))
        assertTrue(knownAmIssues(snap(known, context = "other"), "lab").isEmpty())
        assertTrue(knownAmIssues(snap(null), "lab").isEmpty())
    }

    @Test
    fun detailSplitsBack() {
        assertEquals(AmDetail("critical", "NodeFilesystemAlmostOutOfSpace", "worker-1"), AmDetail.parse(disk))
        assertEquals(DATA_CRITICAL, amSeverity(disk))
        assertEquals(DATA_WARNING, amSeverity(crashLoop))
        assertEquals(AmDetail("", "", ""), AmDetail.parse(""))
    }
}
