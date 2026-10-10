package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.ClusterStorageHealth
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.HistoryRecord
import name.levis.ichor.model.HistoryVolumeEntry
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.NodeHealth.NOT_READY
import name.levis.ichor.model.NodeHealth.READY
import name.levis.ichor.model.NodeHealth.UNREACHABLE
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.NodeStorageHealth
import name.levis.ichor.model.StorageVolumeHealth
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class HistoryRecordsTest {

    private val now = 1_800_000_000_000L
    private val lab = ContextSummary(name = "lab", fingerprint = "fl", clusterId = "lab")
    private val prod = ContextSummary(name = "prod", fingerprint = "fp", clusterId = "prod")

    private fun snap(context: ContextSummary, vararg nodes: Pair<String, name.levis.ichor.model.NodeHealth>) = ClusterSnapshot(
        context = context.name,
        takenAt = now,
        nodes = nodes.associate { (addr, h) -> addr to NodeState("host-$addr", h) },
        fingerprint = context.fingerprint,
    )

    private fun records(before: MonitorState, vararg reads: ClusterRead, masked: Boolean = false): Map<String, HistoryRecord> {
        val run = monitorRun(before, reads.toList(), now, unreachableAlerts = false, unreachableRuns = 3, active = "fl")
        return historyRecords(before, reads.toList(), run.state, now, masked)
    }

    @Test
    fun aRecordHoldsTheNodesWithTheirVersionAndMemory() {
        val overview = ClusterOverview(
            "lab",
            listOf(
                NodeOverview("10.0.0.11", "cp-1", reachable = true, version = "v1.11.2", memTotal = 1000, memAvailable = 600),
                NodeOverview("10.0.0.21", "worker-1", reachable = false, error = "timeout"),
            ),
        )
        val storage = ClusterStorageHealth(
            "lab",
            listOf(
                NodeStorageHealth("10.0.0.11", "cp-1", volumes = listOf(StorageVolumeHealth("10.0.0.11|EPHEMERAL", "EPHEMERAL", usedPercent = 62.4))),
                NodeStorageHealth("10.0.0.21", "worker-1", error = "timeout"),
            ),
        )
        val read = ClusterRead(lab, snap(lab, "10.0.0.11" to READY, "10.0.0.21" to UNREACHABLE), detail = historyDetailOf(overview, storage))

        val record = records(MonitorState.EMPTY, read).getValue("fl")

        assertEquals(now, record.at)
        assertTrue(record.reachable)
        val cp = record.nodes.single { it.node == "10.0.0.11" }
        assertEquals("host-10.0.0.11", cp.hostname)
        assertEquals("ready", cp.health)
        assertEquals("v1.11.2", cp.version)
        assertEquals(40.0, cp.memUsedPercent!!, 0.001)
        val worker = record.nodes.single { it.node == "10.0.0.21" }
        assertEquals("unreachable", worker.health)
        assertNull(worker.version)
        assertNull(worker.memUsedPercent)
        // Every volume read, the node that did not answer has none.
        assertEquals(listOf(HistoryVolumeEntry("10.0.0.11|EPHEMERAL", "EPHEMERAL", "10.0.0.11", 62.4)), record.volumes)
    }

    @Test
    fun theJsonLeavesOutWhatTheContractDefaults() {
        val record = records(MonitorState.EMPTY, ClusterRead(lab, snap(lab, "a" to NOT_READY))).getValue("fl")
        val json = TalosJson.encodeToString(HistoryRecord.serializer(), record)
        assertEquals("""{"at":$now,"nodes":[{"node":"a","hostname":"host-a","health":"notReady"}]}""", json)
    }

    @Test
    fun aKubeconfigClusterSendsItsKubeletVersions() {
        val nodes = KubeNodesOverview(nodes = listOf(KubeNodeInfo("node-a", ready = true, kubelet = "v1.31.2")))
        val detail = historyDetailOf(nodes)
        assertEquals(mapOf("node-a" to "v1.31.2"), detail.versions)
        assertNull(detail.volumes)
    }

    @Test
    fun screenshotModeRecordsNothing() {
        assertTrue(records(MonitorState.EMPTY, ClusterRead(lab, snap(lab, "a" to READY)), masked = true).isEmpty())
    }

    @Test
    fun skippedClustersAndClustersWithoutFingerprintRecordNothing() {
        val bare = ContextSummary(name = "bare", fingerprint = "", clusterId = "bare")
        val result = records(
            MonitorState.EMPTY,
            ClusterRead(lab, snapshot = null, skipped = true),
            ClusterRead(bare, snap(bare, "a" to READY)),
            ClusterRead(prod, snap(prod, "b" to READY)),
        )
        assertEquals(setOf("fp"), result.keys)
    }

    @Test
    fun openAlertsCoverEveryTrackAfterEvaluation() {
        val issues = ClusterSnapshot(
            context = "lab",
            takenAt = now,
            nodes = mapOf("a" to NodeState("cp-1", READY)),
            fingerprint = "fl",
            etcdAlarms = listOf("m1:NOSPACE"),
            etcdChecked = true,
            dataWatched = true,
            dataChecked = true,
            dataIssues = mapOf("longhorn|pvc-data" to DATA_WARNING),
            gitopsWatched = true,
            gitopsChecked = true,
            gitopsIssues = mapOf("argocd|apps/web" to "critical|degraded"),
            checkupWatched = true,
            checkupChecked = true,
            checkupIssues = mapOf("pods|crash|default/api" to DATA_CRITICAL),
            amWatched = true,
            amChecked = true,
            amIssues = mapOf("abc123" to "critical|KubePodCrashLooping|default"),
            storageWatched = true,
            storageChecked = true,
            storageIssues = mapOf("a|EPHEMERAL" to StorageDetail(DATA_WARNING, false, "cp-1", "EPHEMERAL", 90).format()),
            // Expires in two days: warned.
            certNotAfter = now / 1000 + 2 * 86_400,
        )
        // The baseline: what is there is open.
        val record = records(MonitorState.EMPTY, ClusterRead(lab, issues)).getValue("fl")
        val byKey = record.alerts.associateBy { it.key }

        assertEquals(
            setOf(
                "etcd:m1:NOSPACE", "data:longhorn|pvc-data", "gitops:argocd|apps/web", "checkup:pods|crash|default/api",
                "am:abc123", "storage:a|EPHEMERAL", "cert",
            ),
            byKey.keys,
        )
        assertEquals("KubePodCrashLooping", byKey.getValue("am:abc123").title)
        assertEquals("critical", byKey.getValue("gitops:argocd|apps/web").severity)
        assertEquals("cp-1 EPHEMERAL", byKey.getValue("storage:a|EPHEMERAL").title)
        assertEquals("warning", byKey.getValue("cert").severity)
    }

    @Test
    fun aTrackThatCouldNotBeReadSendsItsPreviousKeysAgain() {
        val before = snap(lab, "a" to READY).copy(
            amWatched = true, amChecked = true, amIssues = mapOf("abc123" to "critical|KubePodCrashLooping|default"),
            etcdChecked = true, etcdAlarms = listOf("m1:NOSPACE"),
        )
        // This run: the Alertmanager and the etcd alarms could not be read.
        val now = snap(lab, "a" to READY).copy(amWatched = true, amChecked = false, etcdChecked = false)

        val record = records(MonitorState(mapOf("fl" to before)), ClusterRead(lab, now)).getValue("fl")

        assertEquals(listOf("am:abc123", "etcd:m1:NOSPACE"), record.alerts.map { it.key }.sorted())
    }

    @Test
    fun aClusterThatDidNotAnswerIsAGapWithItsKnownAlerts() {
        val before = snap(lab, "a" to READY).copy(dataWatched = true, dataChecked = true, dataIssues = mapOf("cnpg|db" to DATA_CRITICAL))
        val state = MonitorState(mapOf("fl" to before))

        val unread = records(state, ClusterRead(lab, snapshot = null)).getValue("fl")
        assertFalse(unread.reachable)
        assertTrue(unread.nodes.isEmpty())
        assertEquals(listOf("data:cnpg|db"), unread.alerts.map { it.key })

        // Off the cluster's network: every node unreachable, the run keeps the previous snapshot.
        val blind = records(state, ClusterRead(lab, snap(lab, "a" to UNREACHABLE))).getValue("fl")
        assertFalse(blind.reachable)
        assertEquals(listOf("data:cnpg|db"), blind.alerts.map { it.key })
    }

    @Test
    fun aResolvedIssueIsNoLongerOpen() {
        val before = snap(lab, "a" to READY).copy(dataWatched = true, dataChecked = true, dataIssues = mapOf("cnpg|db" to DATA_CRITICAL))
        val fixed = snap(lab, "a" to READY).copy(dataWatched = true, dataChecked = true)
        val record = records(MonitorState(mapOf("fl" to before)), ClusterRead(lab, fixed)).getValue("fl")
        assertTrue(record.alerts.isEmpty())
    }
}
