package name.levis.ichor.monitor

import name.levis.ichor.model.CnpgCluster
import name.levis.ichor.model.CnpgStatus
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.GarageInstance
import name.levis.ichor.model.GarageStatus
import name.levis.ichor.model.LonghornStatus
import name.levis.ichor.model.LonghornVolume
import name.levis.ichor.model.NodeHealth
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DataAlertsTest {
    private val now = 1_800_000_000_000L
    private val farCert = now / 1000 + 365L * 86_400

    private fun snap(issues: Map<String, String>?, watched: Boolean = true, context: String = "lab") = ClusterSnapshot(
        context = context,
        takenAt = now,
        nodes = mapOf("a" to NodeState("host-a", NodeHealth.READY)),
        etcdChecked = true,
        certNotAfter = farCert,
        dataWatched = watched,
        dataChecked = watched && issues != null,
        dataIssues = issues.orEmpty(),
    )

    @Test
    fun issuesOfEachSystem() {
        val services = DataServices(
            longhorn = LonghornStatus(
                volumes = listOf(
                    LonghornVolume(name = "pvc-1", pvcNamespace = "app", pvcName = "search", health = "critical"),
                    LonghornVolume(name = "pvc-2", pvcNamespace = "app", pvcName = "db", health = "warning"),
                    LonghornVolume(name = "pvc-3", health = "idle"),
                ),
            ),
            garage = GarageStatus(
                instances = listOf(
                    GarageInstance(namespace = "s3", name = "main", status = "degraded"),
                    GarageInstance(namespace = "nas", name = "garage", status = "healthy", resyncErrors = 0),
                    GarageInstance(namespace = "old", name = "garage", status = "unavailable"),
                ),
            ),
            cnpg = CnpgStatus(
                clusters = listOf(
                    CnpgCluster(namespace = "db", name = "down", health = "critical", reasons = listOf("noInstance")),
                    CnpgCluster(namespace = "db", name = "backups", health = "warning", reasons = listOf("backupFailed")),
                    // A switchover or a missing replica is usually planned: no alert.
                    CnpgCluster(namespace = "db", name = "moving", health = "warning", reasons = listOf("switchover", "instances")),
                ),
            ),
        )

        assertEquals(
            mapOf(
                "cnpg|db/backups" to DATA_WARNING,
                "cnpg|db/down" to DATA_CRITICAL,
                "garage|old/garage" to DATA_CRITICAL,
                "garage|s3/main" to DATA_WARNING,
                "longhorn|app/db" to DATA_WARNING,
                "longhorn|app/search" to DATA_CRITICAL,
            ),
            dataIssuesOf(services),
        )
    }

    @Test
    fun firstCheckIsASilentBaseline() {
        val result = evaluate(null, snap(mapOf("longhorn|app/db" to DATA_CRITICAL)), now)
        assertTrue(result.alerts.isEmpty())
        assertEquals(mapOf("longhorn|app/db" to DATA_CRITICAL), result.next.dataIssues)
    }

    @Test
    fun criticalAlertsAtOnce() {
        val result = evaluate(snap(emptyMap()), snap(mapOf("cnpg|db/down" to DATA_CRITICAL)), now)
        val alert = result.alerts.single()
        assertEquals("data:cnpg|db/down", alert.key)
        assertEquals(AlertKind.DATA_PROBLEM, alert.kind)
        assertEquals("db/down", alert.subject)
        assertEquals("cnpg|critical", alert.detail)
        assertTrue(alert.problem)
    }

    @Test
    fun warningNeedsTwoChecksInARow() {
        val first = evaluate(snap(emptyMap()), snap(mapOf("longhorn|app/db" to DATA_WARNING)), now)
        assertTrue(first.alerts.isEmpty())
        assertEquals(listOf("longhorn|app/db"), first.next.dataPending)

        val second = evaluate(first.next, snap(mapOf("longhorn|app/db" to DATA_WARNING)), now)
        assertEquals(listOf("data:longhorn|app/db"), second.alerts.map { it.key })
        assertTrue(second.next.dataPending.isEmpty())

        // Still degraded: notified once only.
        assertTrue(evaluate(second.next, snap(mapOf("longhorn|app/db" to DATA_WARNING)), now).alerts.isEmpty())
    }

    @Test
    fun aShortDegradationNeverAlerts() {
        val first = evaluate(snap(emptyMap()), snap(mapOf("longhorn|app/db" to DATA_WARNING)), now)
        val healed = evaluate(first.next, snap(emptyMap()), now)
        assertTrue(healed.alerts.isEmpty())
        assertTrue(healed.next.dataPending.isEmpty())
    }

    @Test
    fun escalationAlertsAgainButDeescalationDoesNot() {
        val warned = snap(emptyMap()).copy(dataIssues = mapOf("garage|s3/main" to DATA_WARNING))
        val worse = evaluate(warned, snap(mapOf("garage|s3/main" to DATA_CRITICAL)), now)
        assertEquals(listOf(AlertKind.DATA_PROBLEM), worse.alerts.map { it.kind })

        val better = evaluate(worse.next, snap(mapOf("garage|s3/main" to DATA_WARNING)), now)
        assertTrue(better.alerts.isEmpty())
    }

    @Test
    fun recoveryIsNotifiedOnce() {
        val known = snap(emptyMap()).copy(dataIssues = mapOf("cnpg|db/down" to DATA_CRITICAL))
        val result = evaluate(known, snap(emptyMap()), now)
        val alert = result.alerts.single()
        assertEquals(AlertKind.DATA_OK, alert.kind)
        assertFalse(alert.problem)
        assertTrue(result.next.dataIssues.isEmpty())
    }

    @Test
    fun anUnreadableCheckKeepsWhatWasKnown() {
        val known = snap(emptyMap()).copy(dataIssues = mapOf("cnpg|db/down" to DATA_CRITICAL), dataPending = listOf("longhorn|app/db"))
        val result = evaluate(known, snap(issues = null), now)
        assertTrue(result.alerts.isEmpty())
        assertEquals(known.dataIssues, result.next.dataIssues)
        assertEquals(known.dataPending, result.next.dataPending)
        assertTrue(result.next.dataChecked)
    }

    @Test
    fun turningWatchingOffForgetsAndOnAgainIsABaseline() {
        val known = snap(emptyMap()).copy(dataIssues = mapOf("cnpg|db/down" to DATA_CRITICAL))
        val off = evaluate(known, snap(issues = null, watched = false), now)
        assertTrue(off.alerts.isEmpty())
        assertTrue(off.next.dataIssues.isEmpty())

        val on = evaluate(off.next, snap(mapOf("longhorn|app/search" to DATA_CRITICAL)), now)
        assertTrue(on.alerts.isEmpty())
    }

    @Test
    fun anotherClusterIsABaseline() {
        val known = snap(emptyMap(), context = "lab")
        val result = evaluate(known, snap(mapOf("cnpg|db/down" to DATA_CRITICAL), context = "prod"), now)
        assertTrue(result.alerts.isEmpty())
    }
}
