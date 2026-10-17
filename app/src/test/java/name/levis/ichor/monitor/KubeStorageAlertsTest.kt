package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.KubeNodeDisk
import name.levis.ichor.model.KubeNodeDiskUsage
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.ShareTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeStorageAlertsTest {
    private val now = 1_800_000_000_000L
    private val farCert = now / 1000 + 365L * 86_400

    private fun node(name: String = "worker-1", used: Int = 40, pressure: Boolean = false, capacity: Long = 107_374_182_400) =
        KubeNodeDiskUsage(name, used, availableBytes = 9_663_676_416, capacityBytes = capacity, diskPressure = pressure)

    private fun disk(vararg nodes: KubeNodeDiskUsage, forbidden: Boolean = false, skipped: Boolean = false) =
        KubeNodeDisk(nodes.toList(), forbidden = forbidden, measured = nodes.count { it.measured }, skipped = skipped)

    private val listed = setOf("worker-1", "worker-2")

    private fun issues(d: KubeNodeDisk, warn: Int = 85, crit: Int = 95, known: Map<String, String> = emptyMap()) =
        kubeStorageIssuesOf(d, warn, crit, listed, known)

    private fun snap(issues: Map<String, String>?) = ClusterSnapshot(
        context = "lab-kube",
        kube = true,
        takenAt = now,
        nodes = mapOf("worker-1" to NodeState("worker-1", NodeHealth.READY)),
        certNotAfter = farCert,
        storageWatched = true,
        storageChecked = issues != null,
        storageIssues = issues.orEmpty(),
    )

    @Test
    fun theGoJsonDecodesLeniently() {
        val json = """{"nodes":[{"name":"worker-1","usedPercent":91,"availableBytes":9663676416,"capacityBytes":107374182400,
            "imageFsUsedPercent":40,"diskPressure":false,"extra":1},{"name":"worker-2"}],"forbidden":false,"measured":1}"""
        val d = TalosJson.decodeFromString(KubeNodeDisk.serializer(), json)
        assertEquals(91, d.nodes[0].usedPercent)
        assertEquals(0L, d.nodes[1].capacityBytes)
        assertEquals(false, d.skipped)
        assertEquals(setOf("worker-1|nodefs"), issues(d)!!.keys)
    }

    @Test
    fun warningAndCriticalFollowTheThresholds() {
        assertTrue(issues(disk(node(used = 84)))!!.isEmpty())
        val warning = StorageDetail.parse(issues(disk(node(used = 85)))!!.getValue("worker-1|nodefs"))
        assertEquals(DATA_WARNING, warning.severity)
        assertEquals(85, warning.percent)
        assertEquals("worker-1", warning.hostname)
        assertEquals(KUBE_NODEFS, warning.name)
        assertEquals(9_663_676_416, warning.freeBytes)
        assertEquals(107_374_182_400, warning.sizeBytes)
        assertEquals(85, warning.warn)
        val critical = StorageDetail.parse(issues(disk(node(used = 96)))!!.values.single())
        assertEquals(DATA_CRITICAL, critical.severity)
        assertEquals(DATA_CRITICAL, StorageDetail.parse(issues(disk(node(used = 81)), warn = 70, crit = 80)!!.values.single()).severity)
    }

    @Test
    fun diskPressureIsCriticalUnderTheThreshold() {
        val detail = StorageDetail.parse(issues(disk(node(used = 30, pressure = true)))!!.getValue("worker-1|nodefs"))
        assertEquals(DATA_CRITICAL, detail.severity)
        assertEquals(30, detail.percent)
        assertEquals("DiskPressure", detail.reason)
    }

    @Test
    fun forbiddenSkippedOrUnmeasuredIsNoReading() {
        assertNull(issues(disk(forbidden = true)))
        assertNull(issues(disk(node(used = 99), skipped = true)))
        assertNull(issues(disk(node(used = 99, capacity = 0))))
    }

    @Test
    fun aListedNodeThatDidNotAnswerKeepsItsKnownIssues() {
        val known = mapOf(
            "worker-2|nodefs" to StorageDetail(DATA_WARNING, false, "worker-2", KUBE_NODEFS, 90).format(),
            "worker-1|nodefs" to StorageDetail(DATA_WARNING, false, "worker-1", KUBE_NODEFS, 90).format(),
            "gone|nodefs" to StorageDetail(DATA_WARNING, false, "gone", KUBE_NODEFS, 90).format(),
        )
        // worker-1 answered (resolved), worker-2 did not (kept), "gone" is no longer listed (dropped).
        assertEquals(setOf("worker-2|nodefs"), issues(disk(node(used = 40)), known = known)!!.keys)
    }

    @Test
    fun criticalAlertsAtOnceWithTheStorageKind() {
        val result = evaluate(snap(emptyMap()), snap(issues(disk(node(used = 30, pressure = true)))), now)
        val alert = result.alerts.single()
        assertEquals("storage:worker-1|nodefs", alert.key)
        assertEquals(AlertKind.STORAGE_PROBLEM, alert.kind)
        assertEquals("worker-1", alert.subject)
        assertEquals(ShareTarget(target = ShareTarget.NODE, addr = "worker-1", host = "worker-1"), alert.shareTarget())
        val ok = evaluate(result.next, snap(issues(disk(node(used = 40)))), now).alerts.single()
        assertEquals(AlertKind.STORAGE_OK, ok.kind)
        assertEquals("storage:worker-1|nodefs", ok.key)
    }

    @Test
    fun anUnreadableCheckKeepsTheOpenIssue() {
        val open = evaluate(snap(emptyMap()), snap(issues(disk(node(used = 97)))), now).next
        val blind = evaluate(open, snap(issues(disk(forbidden = true))), now)
        assertTrue(blind.alerts.isEmpty())
        assertEquals(setOf("worker-1|nodefs"), blind.next.storageIssues.keys)
    }

    @Test
    fun historyVolumesAreTheMeasuredNodes() {
        val volumes = kubeHistoryVolumesOf(disk(node(used = 91), node("worker-2", capacity = 0)))!!
        assertEquals(listOf("worker-1|nodefs"), volumes.map { it.key })
        assertEquals(91.0, volumes.single().usedPercent, 0.0)
        assertNull(kubeHistoryVolumesOf(null))
    }
}
