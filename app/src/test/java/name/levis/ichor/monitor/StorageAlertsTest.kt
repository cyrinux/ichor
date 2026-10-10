package name.levis.ichor.monitor

import name.levis.ichor.data.MemoryPrefs
import name.levis.ichor.data.MemoryValue
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.ClusterStorageHealth
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.NodeStorageHealth
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.StorageDiskHealth
import name.levis.ichor.model.StorageVolumeHealth
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class StorageAlertsTest {
    private val now = 1_800_000_000_000L
    private val farCert = now / 1000 + 365L * 86_400

    private fun volume(node: String, name: String, used: Double) =
        StorageVolumeHealth("$node|$name", name, usedPercent = used, freeBytes = 9_663_676_416, sizeBytes = 107_374_182_400)

    private fun disk(node: String, device: String, health: String, reason: String = "", model: String = "") =
        StorageDiskHealth("$node|smart|$device", device, model, health, reason)

    private fun health(vararg nodes: NodeStorageHealth) = ClusterStorageHealth("lab", nodes.toList())

    private fun worker(used: Double = 40.0, smart: String = "ok") = NodeStorageHealth(
        "192.0.2.20", "worker-1",
        volumes = listOf(volume("192.0.2.20", "STATE", 6.0), volume("192.0.2.20", "EPHEMERAL", used)),
        disks = listOf(disk("192.0.2.20", "nvme0n1", smart, reason = "available spare below threshold")),
    )

    private fun issues(h: ClusterStorageHealth, warn: Int = 85, crit: Int = 95, known: Map<String, String> = emptyMap()) =
        storageIssuesOf(h, warn, crit, known)

    private fun snap(issues: Map<String, String>?, watched: Boolean = true, context: String = "lab") = ClusterSnapshot(
        context = context,
        takenAt = now,
        nodes = mapOf("192.0.2.20" to NodeState("worker-1", NodeHealth.READY)),
        etcdChecked = true,
        certNotAfter = farCert,
        storageWatched = watched,
        storageChecked = watched && issues != null,
        storageIssues = issues.orEmpty(),
    )

    @Test
    fun theGoJsonDecodes() {
        val json = """{"context":"lab","nodes":[{"node":"192.0.2.20","hostname":"worker-1","volumes":[
            {"key":"192.0.2.20|EPHEMERAL","name":"EPHEMERAL","mount":"/var","usedPercent":91.4,"freeBytes":9663676416,"sizeBytes":107374182400,"level":"warning"}],
            "disks":[{"key":"192.0.2.20|smart|sda","device":"sda","health":"failing","reason":"media errors"}]},
            {"node":"192.0.2.30","hostname":"192.0.2.30","volumes":[],"disks":[],"error":"connection refused"}]}"""
        val h = TalosJson.decodeFromString(ClusterStorageHealth.serializer(), json)
        assertEquals(91.4, h.nodes[0].volumes[0].usedPercent, 0.0)
        assertEquals("connection refused", h.nodes[1].error)
        assertEquals(setOf("192.0.2.20|EPHEMERAL", "192.0.2.20|smart|sda"), issues(h).keys)
    }

    @Test
    fun fillLevelsFollowTheThresholds() {
        assertTrue(issues(health(worker(used = 84.9))).isEmpty())
        val warning = StorageDetail.parse(issues(health(worker(used = 85.0))).getValue("192.0.2.20|EPHEMERAL"))
        assertEquals(DATA_WARNING, warning.severity)
        assertEquals(85, warning.percent)
        val critical = StorageDetail.parse(issues(health(worker(used = 95.0))).getValue("192.0.2.20|EPHEMERAL"))
        assertEquals(DATA_CRITICAL, critical.severity)
        assertEquals("worker-1", critical.hostname)
        assertEquals("EPHEMERAL", critical.name)
        assertEquals(9_663_676_416, critical.freeBytes)
        assertEquals(107_374_182_400, critical.sizeBytes)
    }

    @Test
    fun customThresholdsApply() {
        val warning = StorageDetail.parse(issues(health(worker(used = 72.0)), warn = 70, crit = 80).getValue("192.0.2.20|EPHEMERAL"))
        assertEquals(DATA_WARNING, warning.severity)
        assertEquals(70, warning.warn)
        assertEquals(DATA_CRITICAL, StorageDetail.parse(issues(health(worker(used = 81.0)), warn = 70, crit = 80).values.single()).severity)
        // A critical threshold not above the warning one is raised past it.
        assertEquals(DATA_WARNING, StorageDetail.parse(issues(health(worker(used = 90.5)), warn = 90, crit = 60).values.single()).severity)
    }

    @Test
    fun aFailingDiskIsCriticalAndOtherVerdictsAreNot() {
        val failing = issues(health(worker(smart = StorageDiskHealth.SMART_FAILING)))
        val detail = StorageDetail.parse(failing.getValue("192.0.2.20|smart|nvme0n1"))
        assertEquals(DATA_CRITICAL, detail.severity)
        assertTrue(detail.smart)
        assertEquals("nvme0n1", detail.name)
        assertEquals("available spare below threshold", detail.reason)
        assertTrue(issues(health(worker(smart = "unknown"))).isEmpty())
        // Without a reason, the model.
        val bare = NodeStorageHealth("n", "h", disks = listOf(disk("n", "sda", "failing", model = "Demo|SSD")))
        assertEquals("Demo/SSD", StorageDetail.parse(issues(health(bare)).values.single()).reason)
    }

    @Test
    fun aNodeThatDidNotAnswerKeepsItsKnownIssues() {
        val known = mapOf(
            "192.0.2.30|EPHEMERAL" to StorageDetail(DATA_WARNING, false, "worker-2", "EPHEMERAL", 90).format(),
            "192.0.2.20|STATE" to StorageDetail(DATA_WARNING, false, "worker-1", "STATE", 90).format(),
        )
        val down = NodeStorageHealth("192.0.2.30", "192.0.2.30", error = "connection refused")
        val read = issues(health(worker(), down), known = known)
        // worker-2's is kept; worker-1 answered, so its STATE is resolved.
        assertEquals(setOf("192.0.2.30|EPHEMERAL"), read.keys)
    }

    @Test
    fun knownIssuesAreOnlyTheSameClustersRead() {
        val prev = snap(mapOf("k" to "v"))
        assertEquals(mapOf("k" to "v"), knownStorageIssues(prev, "lab"))
        assertTrue(knownStorageIssues(prev, "prod").isEmpty())
        assertTrue(knownStorageIssues(snap(issues = null), "lab").isEmpty())
    }

    @Test
    fun firstCheckIsASilentBaseline() {
        val result = evaluate(null, snap(issues(health(worker(used = 96.0)))), now)
        assertTrue(result.alerts.isEmpty())
        assertEquals(1, result.next.storageIssues.size)
    }

    @Test
    fun criticalAlertsAtOnceAndOpensTheNodesStorage() {
        val result = evaluate(snap(emptyMap()), snap(issues(health(worker(used = 96.0)))), now)
        val alert = result.alerts.single()
        assertEquals("storage:192.0.2.20|EPHEMERAL", alert.key)
        assertEquals(AlertKind.STORAGE_PROBLEM, alert.kind)
        assertEquals("worker-1", alert.subject)
        assertTrue(alert.problem)
        assertEquals(AlertChannel.NODES, alert.channel)
        assertEquals(ShareTarget.storage("192.0.2.20", "worker-1"), alert.shareTarget())
        assertEquals(listOf(AlertAction.SNOOZE), alert.actions(canWake = true, canReboot = true))
    }

    @Test
    fun smartFailingAlertsAtOnce() {
        val result = evaluate(snap(emptyMap()), snap(issues(health(worker(smart = "failing")))), now)
        assertEquals(listOf("storage:192.0.2.20|smart|nvme0n1"), result.alerts.map { it.key })
        assertEquals(ShareTarget.storage("192.0.2.20", "worker-1"), result.alerts.single().shareTarget())
    }

    @Test
    fun aWarningNeedsTwoChecksInARow() {
        val filling = issues(health(worker(used = 88.0)))
        val first = evaluate(snap(emptyMap()), snap(filling), now)
        assertTrue(first.alerts.isEmpty())
        assertEquals(listOf("192.0.2.20|EPHEMERAL"), first.next.storagePending)

        // Its percent moved: still the same issue.
        val second = evaluate(first.next, snap(issues(health(worker(used = 89.0)))), now)
        assertEquals(listOf(AlertKind.STORAGE_PROBLEM), second.alerts.map { it.kind })
        assertTrue(evaluate(second.next, snap(filling), now).alerts.isEmpty())
    }

    @Test
    fun escalationToCriticalAlertsAgain() {
        val warned = snap(emptyMap()).copy(storageIssues = issues(health(worker(used = 88.0))))
        assertEquals(1, evaluate(warned, snap(issues(health(worker(used = 97.0)))), now).alerts.size)
    }

    @Test
    fun resolvedIsNotifiedOnce() {
        val known = snap(emptyMap()).copy(storageIssues = issues(health(worker(used = 96.0, smart = "failing"))))
        val result = evaluate(known, snap(issues(health(worker()))), now)
        assertEquals(2, result.alerts.size)
        assertTrue(result.alerts.all { it.kind == AlertKind.STORAGE_OK && !it.problem && it.subject == "worker-1" })
        assertTrue(result.next.storageIssues.isEmpty())
        assertTrue(evaluate(result.next, snap(issues(health(worker()))), now).alerts.isEmpty())
    }

    @Test
    fun anUnreadableCheckKeepsWhatWasKnown() {
        val known = snap(emptyMap()).copy(storageIssues = issues(health(worker(used = 96.0))))
        val result = evaluate(known, snap(issues = null), now)
        assertTrue(result.alerts.isEmpty())
        assertEquals(known.storageIssues, result.next.storageIssues)
    }

    @Test
    fun detailRoundTripsAndSurvivesTheSeparator() {
        val detail = StorageDetail(DATA_CRITICAL, true, "worker|1", "sda", reason = "a|b")
        val parsed = StorageDetail.parse(detail.format())
        assertEquals("worker/1", parsed.hostname)
        assertEquals("a/b", parsed.reason)
        assertTrue(parsed.smart)
        assertEquals(DATA_WARNING, storageSeverity("junk"))
    }

    @Test
    fun thresholdsDefaultAndStayOrdered() {
        val store = MonitorStore(MemoryPrefs(), MemoryValue())
        assertFalse(store.storageWatched.value)
        assertEquals(85, store.storageWarnPercent.value)
        assertEquals(95, store.storageCriticalPercent.value)
        store.setStorageWarnPercent(10)
        assertEquals(50, store.storageWarnPercent.value)
        store.setStorageWarnPercent(97)
        assertEquals(98, store.storageCriticalPercent.value)
        store.setStorageCriticalPercent(60)
        assertEquals(98, store.storageCriticalPercent.value)
        store.setStorageCriticalPercent(100)
        assertEquals(99, store.storageCriticalPercent.value)
    }

    @Test
    fun anOlderSnapshotDecodesWithoutStorage() {
        val json = """{"context":"lab","takenAt":1,"nodes":{},"amWatched":true,"amChecked":true}"""
        val snapshot = TalosJson.decodeFromString(ClusterSnapshot.serializer(), json)
        assertFalse(snapshot.storageWatched)
        assertTrue(snapshot.storageIssues.isEmpty())
    }
}
