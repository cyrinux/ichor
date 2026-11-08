package name.levis.ichor.widget

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/** The Data Layer path of the phone's snapshot for the watch. */
const val WEAR_SNAPSHOT_PATH = "/snapshot"

/** The DataMap key holding [WearPayload] as JSON bytes. */
const val WEAR_PAYLOAD_KEY = "payload"

/**
 * What the phone sends the watch after each background check: per cluster its label (already
 * masked in screenshot mode) and node counts. No hostname, address, endpoint or credential:
 * the watch only draws the tile and the complication.
 */
@Serializable
data class WearPayload(
    /** The cluster on screen on the phone first. */
    val clusters: List<WearCluster> = emptyList(),
    /** The whole fleet's tally (null from an older phone app, or before the first check). */
    val fleet: FleetTally? = null,
)

@Serializable
data class WearCluster(
    val label: String,
    val takenAt: Long,
    val counts: NodeCounts,
)

private val WearJson = Json { ignoreUnknownKeys = true }

fun encodeWearPayload(p: WearPayload): ByteArray = WearJson.encodeToString(WearPayload.serializer(), p).encodeToByteArray()

/** Null for bytes of an unknown shape (a newer or older phone app): the watch shows no data. */
fun decodeWearPayload(bytes: ByteArray): WearPayload? =
    runCatching { WearJson.decodeFromString(WearPayload.serializer(), bytes.decodeToString()) }.getOrNull()
