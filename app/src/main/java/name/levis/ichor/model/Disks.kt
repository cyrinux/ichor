package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo diskLayoutList (NodeDisks).

@Serializable
data class DiskLayoutList(
    val supported: Boolean = true,
    val reason: String = "",
    val disks: List<DiskLayout> = emptyList(),
)

@Serializable
data class DiskLayout(
    val name: String = "",
    val devPath: String = "",
    /** Bytes. */
    val size: Long = 0,
    /** ssd, hdd, nvme, sd or unknown. */
    val type: String = "",
    /** nvme, sata, virtio, usb... "" when unknown. */
    val transport: String = "",
    val model: String = "",
    val serial: String = "",
    val systemDisk: Boolean = false,
    val readonly: Boolean = false,
    /** Bytes not in any partition. */
    val unallocated: Long = 0,
    /** By partition index. */
    val partitions: List<Partition> = emptyList(),
    /** The bar, by offset: what the core cut, drawn as given. */
    val segments: List<DiskSegment> = emptyList(),
)

@Serializable
data class Partition(
    val index: Int = 0,
    val devPath: String = "",
    val label: String = "",
    /** The Talos volume on it (STATE, EPHEMERAL...), "" when none. */
    val volumeId: String = "",
    val offset: Long = 0,
    val size: Long = 0,
    val filesystem: String = "",
    /** enabled, disabled or unknown. */
    val encryption: String = "",
    val encryptionProvider: String = "",
    val uuid: String = "",
    val partitionUuid: String = "",
    val partitionType: String = "",
    val mountedOn: String = "",
    val phase: String = "",
    val error: String = "",
)

@Serializable
data class DiskSegment(
    /** partition or free. */
    val kind: String = "",
    val index: Int = 0,
    val label: String = "",
    val offset: Long = 0,
    val size: Long = 0,
)

enum class EncryptionState { ENABLED, DISABLED, UNKNOWN }

val Partition.encryptionState: EncryptionState
    get() = when (encryption) {
        "enabled" -> EncryptionState.ENABLED
        "disabled" -> EncryptionState.DISABLED
        else -> EncryptionState.UNKNOWN
    }

/** The partition's label, else its Talos volume, else null (the UI says "Partition N"). */
fun Partition.knownLabel(): String? = label.ifBlank { volumeId }.ifBlank { null }

val DiskSegment.isFree: Boolean get() = kind == "free"

/** One slice of a disk's bar: [fraction] of the bar's width, all slices summing to 1. */
data class BarSlice(val segment: DiskSegment, val fraction: Float)

/**
 * The disk's [DiskLayout.segments] as bar slices sized by their share of the segments' total
 * (the core starts the bar at the first partition, so the total is a bit under the disk size).
 * Slices under [minFraction] are widened to it, the others shrunk to keep the sum at 1, so a
 * 1 MiB META partition stays visible next to a 100 GiB EPHEMERAL. Empty when nothing to draw.
 */
fun DiskLayout.barFractions(minFraction: Float = 0.015f): List<BarSlice> {
    val drawn = segments.filter { it.size > 0 }
    val total = drawn.sumOf { it.size }
    if (total <= 0) return emptyList()
    val raw = drawn.map { it.size.toDouble() / total }
    val floor = minFraction.toDouble().coerceAtMost(1.0 / drawn.size)
    val small = raw.filter { it < floor }
    val bigTotal = raw.filter { it >= floor }.sum()
    val room = 1.0 - floor * small.size
    return drawn.zip(raw) { segment, share ->
        val fraction = if (share < floor) floor else share / bigTotal * room
        BarSlice(segment, fraction.toFloat())
    }
}
