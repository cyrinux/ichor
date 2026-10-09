package name.levis.ichor.model

import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder

/**
 * The Storage screen (Go `KubeStorage`): each PersistentVolumeClaim with its volume, class,
 * pods and fill, problems first. [partialAccess]: volumes, classes or nodes could not be read.
 */
@Serializable
data class KubeStorage(
    val claims: List<StorageClaim> = emptyList(),
    val partialAccess: Boolean = false,
)

/** A claim. Capacity and used in bytes; [measured]: a kubelet reported the fill. */
@Serializable
data class StorageClaim(
    val namespace: String = "",
    val name: String = "",
    val phase: String = "",
    val storageClass: String = "",
    val provisioner: String = "",
    val volume: String = "",
    val reclaimPolicy: String = "",
    val accessModes: List<String> = emptyList(),
    val capacity: Double = 0.0,
    val used: Double = 0.0,
    val usedPercent: Double = 0.0,
    val inodesPercent: Double = 0.0,
    val measured: Boolean = false,
    val pods: List<String> = emptyList(),
    val terminating: Boolean = false,
    /** The data service the volume belongs to, by its catalog id; "" for none. */
    val managedBy: String = "",
    @Serializable(with = StorageLevelSerializer::class)
    val level: StorageLevel = StorageLevel.OK,
) {
    val key: String get() = "$namespace/$name"

    val usedFraction: Float get() = (usedPercent / 100).coerceIn(0.0, 1.0).toFloat()

    /** The data service screen that manages this volume, null for none this version knows. */
    val managedKind: DataServiceKind? get() = DataServiceKind.entries.firstOrNull { it.catalogId == managedBy }
}

/** How a claim is doing: ok, warning (pending, terminating, filling), critical (lost, nearly full). */
enum class StorageLevel(val wire: String) { OK("ok"), WARNING("warning"), CRITICAL("critical") }

/** Reads a level a newer core may name as [StorageLevel.OK]. */
object StorageLevelSerializer : KSerializer<StorageLevel> {
    override val descriptor = PrimitiveSerialDescriptor("StorageLevel", PrimitiveKind.STRING)

    override fun deserialize(decoder: Decoder): StorageLevel {
        val wire = decoder.decodeString()
        return StorageLevel.entries.firstOrNull { it.wire == wire } ?: StorageLevel.OK
    }

    override fun serialize(encoder: Encoder, value: StorageLevel) = encoder.encodeString(value.wire)
}

/** The claims whose namespace/name, class, volume or a pod contains [query] (any case). */
fun List<StorageClaim>.filteredClaims(query: String): List<StorageClaim> {
    val q = query.trim()
    if (q.isEmpty()) return this
    return filter { c ->
        listOf(c.key, c.storageClass, c.volume, c.provisioner).any { it.contains(q, ignoreCase = true) } ||
            c.pods.any { it.contains(q, ignoreCase = true) }
    }
}
