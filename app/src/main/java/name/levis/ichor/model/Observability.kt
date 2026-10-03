package name.levis.ichor.model

import kotlinx.serialization.Serializable

@Serializable
data class NetworkCounters(val name: String, val rx: Long, val tx: Long, val errors: Long, val drops: Long)
@Serializable
data class DiskCounters(val name: String, val read: Long, val write: Long, val operations: Long, val timeMs: Long, val busyMs: Long)
@Serializable
data class DeviceRate(val name: String, val read: Double, val write: Double, val errors: Double, val drops: Double, val busy: Double, val latency: Double)
@Serializable
data class Bottlenecks(val wait: Double, val steal: Double, val network: List<DeviceRate>, val disks: List<DeviceRate>, val errors: Map<String, String> = emptyMap())
@Serializable
data class DriftNode(val node: String, val hostname: String, val role: String, val values: Map<String, String>, val errors: Map<String, String>)
@Serializable
data class DriftSnapshot(val scope: String, val at: Long, val nodes: List<DriftNode>)
@Serializable
data class DriftChange(val node: String, val reference: String, val key: String, val before: String, val after: String)
@Serializable
data class IncidentEntry(val id: String, val at: Long, val node: String, val kind: String, val subject: String, val detail: String, val severity: String, val metrics: Bottlenecks? = null, val metricsOmitted: Int = 0)
@Serializable
data class IncidentDocument(val scope: String, val startedAt: Long, val updatedAt: Long, val dropped: Int, val entries: List<IncidentEntry>)
