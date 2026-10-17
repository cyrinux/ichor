package name.levis.ichor.wear

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.monitor.ClusterSnapshot
import name.levis.ichor.monitor.MonitorState
import name.levis.ichor.monitor.NodeState
import name.levis.ichor.widget.NodeCounts
import name.levis.ichor.widget.encodeWearPayload
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class WearPayloadTest {

    private val now = 1_800_000_000_000L
    private val lab = ContextSummary(
        name = "admin@lab",
        fingerprint = "fl",
        clusterId = "lab",
        endpoints = listOf("https://192.0.2.10:50000"),
        nodes = listOf("192.0.2.11"),
    )
    private val edge = ContextSummary(name = "admin@edge", fingerprint = "fe", clusterId = "edge")
    private val summary = ConfigSummary(current = "admin@lab", contexts = listOf(edge, lab))

    private fun snap(context: String, fingerprint: String, vararg health: NodeHealth, alarms: Int = 0) = ClusterSnapshot(
        context = context,
        takenAt = now,
        nodes = health.withIndex().associate { (i, h) -> "192.0.2.${i + 11}" to NodeState("cp-$i.lab.example", h, "kubelet: token expired") },
        etcdAlarms = List(alarms) { "1234:NOSPACE" },
        etcdChecked = true,
        certNotAfter = now + 1_000,
        fingerprint = fingerprint,
        dataIssues = mapOf("longhorn|pvc-secret" to "warning"),
    )

    private val state = MonitorState(
        mapOf(
            "fe" to snap("admin@edge", "fe", NodeHealth.READY),
            "fl" to snap("admin@lab", "fl", NodeHealth.READY, NodeHealth.NOT_READY, NodeHealth.UNREACHABLE, alarms = 1),
        ),
        active = "fl",
    )

    @Test
    fun theClusterOnScreenComesFirstWithItsCounts() {
        val p = wearPayload(state, summary, "admin@lab", mapOf("fl" to "Home lab"), masked = false)
        assertEquals(listOf("Home lab", "admin@edge"), p.clusters.map { it.label })
        assertEquals(NodeCounts(total = 3, ready = 1, notReady = 1, unreachable = 1, etcdChecked = true, etcdAlarms = 1), p.clusters[0].counts)
        assertEquals(now, p.clusters[0].takenAt)
    }

    @Test
    fun screenshotModeDropsTheGivenNames() {
        val p = wearPayload(state, summary, "admin@lab", mapOf("fl" to "Home lab", "fe" to "Edge"), masked = true)
        assertEquals(listOf("admin@lab", "admin@edge"), p.clusters.map { it.label })
    }

    @Test
    fun noCredentialEndpointOrHostnameGoesToTheWatch() {
        val json = encodeWearPayload(wearPayload(state, summary, "admin@lab", emptyMap(), masked = false)).decodeToString()
        val lower = json.lowercase()
        val forbidden = listOf("talosconfig", "kubeconfig", "token", "endpoint", "cert", "192.0.2.", "cp-0", "https://", "nospace", "longhorn", "fingerprint")
        for (word in forbidden) {
            assertFalse("payload carries \"$word\": $json", lower.contains(word))
        }
    }

    @Test
    fun noSnapshotSendsAnEmptyPayload() {
        assertEquals(emptyList<Any>(), wearPayload(MonitorState.EMPTY, null, null, emptyMap(), masked = false).clusters)
    }
}
