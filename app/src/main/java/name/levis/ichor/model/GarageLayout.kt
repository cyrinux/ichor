package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_garage_layout.go: a Garage cluster's layout (who holds which role),
// what is staged for its next version and what applying it would do, and its version history.

/** Wire value of a gateway role's capacity: a gateway stores nothing. */
const val GARAGE_GATEWAY_CAPACITY = -1L

/** The least capacity Garage accepts for a storage role, in bytes. */
const val GARAGE_MIN_CAPACITY = 1024L

@Serializable
data class GarageLayout(
    val version: Long = 0,
    /** version + 1 while something is staged, else 0. */
    val nextVersion: Long = 0,
    val partitionSize: Long = 0,
    /** "maximum" or a number of zones. */
    val zoneRedundancy: String = "",
    /** "" when not staged. */
    val stagedZoneRedundancy: String = "",
    /** Only while something is staged. */
    val preview: GarageLayoutPreview? = null,
    val nodes: List<GarageLayoutNode> = emptyList(),
    /** Newest first. */
    val versions: List<GarageLayoutVersion> = emptyList(),
    val minAck: Long = 0,
    /** Only while several versions are live. */
    val trackers: List<GarageLayoutTracker> = emptyList(),
    /** A draining version waits on a node that is down. */
    val stuck: Boolean = false,
    /** Garage's own lines after an apply. */
    val messages: List<String> = emptyList(),
) {
    val hasStaged: Boolean get() = nextVersion > 0

    /** The nodes with a role in the current layout. */
    val members: List<GarageLayoutNode> get() = nodes.filter { it.role != null }

    /** The nodes whose role changes in the next version. */
    val staged: List<GarageLayoutNode> get() = nodes.filter { it.stagedKind != GarageStagedChange.NONE }

    /** Known nodes without a role and nothing staged: they can join. */
    val candidates: List<GarageLayoutNode> get() = nodes.filter { it.role == null && it.stagedKind == GarageStagedChange.NONE && it.known }

    val storageNodes: Int get() = members.count { !it.role!!.gateway }
    val gatewayNodes: Int get() = members.count { it.role!!.gateway }

    /** Every zone a role names, current or staged, for the editor's suggestions. */
    val zones: List<String> get() = nodes.mapNotNull { it.effectiveRole?.zone?.takeIf(String::isNotEmpty) }.distinct().sorted()

    fun node(id: String): GarageLayoutNode? = nodes.firstOrNull { it.id == id }
}

@Serializable
data class GarageLayoutPreview(
    val ok: Boolean = false,
    val error: String = "",
    val messages: List<String> = emptyList(),
    /** -1 when Garage did not say. */
    val movedPartitions: Long = -1,
    val usableCapacity: Long = 0,
    val effectiveCapacity: Long = 0,
    val totalCapacity: Long = 0,
    val lowUsableCapacity: Boolean = false,
    val lowPartitionSize: Boolean = false,
)

@Serializable
data class GarageLayoutRole(
    val zone: String = "",
    /** Bytes, [GARAGE_GATEWAY_CAPACITY] for a gateway. */
    val capacity: Long = GARAGE_GATEWAY_CAPACITY,
    val tags: List<String> = emptyList(),
    val storedPartitions: Long = 0,
    val usableCapacity: Long = 0,
) {
    val gateway: Boolean get() = capacity < 0

    /** How much of the declared capacity the layout uses, 0 when unknown or a gateway. */
    val usableFraction: Float get() = if (capacity > 0 && usableCapacity > 0) (usableCapacity.toFloat() / capacity).coerceIn(0f, 1f) else 0f
}

@Serializable
data class GarageLayoutNode(
    val id: String = "",
    val hostname: String = "",
    val kubeNode: String = "",
    val up: Boolean = false,
    /** The cluster still knows the node (it is in GetClusterStatus). */
    val known: Boolean = false,
    val draining: Boolean = false,
    val dataTotal: Long = 0,
    val role: GarageLayoutRole? = null,
    /** Wire value of [GarageStagedChange]. */
    val staged: String = "",
    val stagedRole: GarageLayoutRole? = null,
) {
    val label: String get() = hostname.ifEmpty { id.take(16) }
    val stagedKind: GarageStagedChange get() = GarageStagedChange.from(staged)

    /** The role after the staged changes: none when the node leaves. */
    val effectiveRole: GarageLayoutRole?
        get() = when (stagedKind) {
            GarageStagedChange.REMOVE -> null
            GarageStagedChange.NONE -> role
            else -> stagedRole ?: role
        }
}

enum class GarageStagedChange(val wire: String) {
    NONE("none"),
    ADD("add"),
    CHANGE("change"),
    REMOVE("remove"),
    ;

    companion object {
        fun from(wire: String): GarageStagedChange = entries.firstOrNull { it.wire == wire } ?: NONE
    }
}

@Serializable
data class GarageLayoutVersion(
    val version: Long = 0,
    /** Wire value of [GarageLayoutStatus]. */
    val status: String = "",
    val storageNodes: Long = 0,
    val gatewayNodes: Long = 0,
) {
    val statusKind: GarageLayoutStatus get() = GarageLayoutStatus.from(status)
}

enum class GarageLayoutStatus(val wire: String) {
    CURRENT("current"),
    DRAINING("draining"),
    HISTORICAL("historical"),
    UNKNOWN(""),
    ;

    companion object {
        fun from(wire: String): GarageLayoutStatus = entries.firstOrNull { it.wire == wire } ?: UNKNOWN
    }
}

@Serializable
data class GarageLayoutTracker(
    val id: String = "",
    val hostname: String = "",
    val ack: Long = 0,
    val sync: Long = 0,
    val syncAck: Long = 0,
) {
    val label: String get() = hostname.ifEmpty { id.take(16) }
}

/** The units the role editor offers for a capacity; Garage shows IEC units too. */
enum class GarageCapacityUnit(val label: String, val bytes: Long) {
    GIB("GiB", 1L shl 30),
    TIB("TiB", 1L shl 40),
    ;

    companion object {
        /** The unit that shows [bytes] with the fewest digits: TiB from one tebibyte up. */
        fun forBytes(bytes: Long): GarageCapacityUnit = if (bytes >= TIB.bytes) TIB else GIB
    }
}

/** "1.5" in [unit] as bytes, null when [text] is not a positive number. */
fun garageCapacityBytes(text: String, unit: GarageCapacityUnit): Long? {
    val value = text.trim().replace(',', '.').toDoubleOrNull() ?: return null
    if (value <= 0 || value.isNaN() || value.isInfinite()) return null
    val bytes = value * unit.bytes
    if (bytes > Long.MAX_VALUE / 2) return null
    return bytes.toLong()
}

/** [bytes] in [unit], with up to two decimals and no trailing zeros: 1.5, 450, 0.25. */
fun garageCapacityText(bytes: Long, unit: GarageCapacityUnit): String {
    val value = bytes.toDouble() / unit.bytes
    val rounded = Math.round(value * 100) / 100.0
    return if (rounded == Math.floor(rounded)) rounded.toLong().toString() else rounded.toString()
}

/**
 * The capacity to propose for a node joining the layout: its data disk rounded down to a
 * whole unit, below the disk so Garage keeps room for metadata; 0 when the disk is unknown.
 */
fun garageSuggestedCapacity(dataTotal: Long): Long {
    if (dataTotal < GarageCapacityUnit.GIB.bytes) return 0
    val unit = GarageCapacityUnit.forBytes(dataTotal)
    val whole = dataTotal / unit.bytes
    // A disk of exactly N units proposes N-1: the disk is never fully given to Garage.
    val units = if (dataTotal % unit.bytes == 0L) whole - 1 else whole
    return if (units <= 0) (dataTotal / GarageCapacityUnit.GIB.bytes - 1).coerceAtLeast(1) * GarageCapacityUnit.GIB.bytes else units * unit.bytes
}
