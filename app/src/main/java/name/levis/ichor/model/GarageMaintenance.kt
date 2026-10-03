package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_garage_actions.go: what the blocks failing to resync in a Garage
// cluster are, and the outcome of the safe repairs for them.

/** Resync tranquility: 0 resyncs at full speed (no pause between blocks), 2 is Garage's default. */
const val GARAGE_TRANQUILITY_FULL = 0L
const val GARAGE_TRANQUILITY_DEFAULT = 2L

@Serializable
data class GarageBlockReport(
    /** Blocks failing to resync, every node. */
    val errored: Int = 0,
    /** Of which looked up: only a sample is. */
    val detailed: Int = 0,
    val live: Int = 0,
    /** Looked up, no live reference. */
    val cleanupOnly: Int = 0,
    /** Referenced by deleted metadata: a block-refs repair fixes it. */
    val staleRefs: Int = 0,
    /** Refcount off: a block-rc repair fixes it. */
    val refcountMismatches: Int = 0,
    /** Refcount 0: safe to retry now. */
    val retryable: Int = 0,
    /** A block-refs or block-rc repair is busy. */
    val repairsRunning: Boolean = false,
    val nodes: List<GarageBlockNode> = emptyList(),
) {
    val verdict: GarageBlockVerdict
        get() = when {
            errored == 0 -> GarageBlockVerdict.NONE_FAILING
            live > 0 -> GarageBlockVerdict.LIVE_DATA
            else -> GarageBlockVerdict.DELETED_ONLY
        }
}

enum class GarageBlockVerdict { NONE_FAILING, LIVE_DATA, DELETED_ONLY }

@Serializable
data class GarageBlockNode(
    val id: String = "",
    val hostname: String = "",
    val errored: Int = 0,
    /** The node did not answer. */
    val error: String = "",
    /** The blocks looked up, failing the longest first. */
    val blocks: List<GarageBlock> = emptyList(),
) {
    val label: String get() = hostname.ifEmpty { id.take(16) }
}

@Serializable
data class GarageBlock(
    val hash: String = "",
    val refcount: Long = 0,
    /** Failed resync attempts. */
    val errors: Long = 0,
    val lastTrySecs: Long = 0,
    val nextTrySecs: Long = 0,
    /** Wire value of [GarageBlockImpact]. */
    val impact: String = "",
    val staleRef: Boolean = false,
    val refcountMismatch: Boolean = false,
    /** The lookup failed. */
    val error: String = "",
    val refs: List<GarageBlockRef> = emptyList(),
) {
    val impactKind: GarageBlockImpact get() = GarageBlockImpact.from(impact)
    val shortHash: String get() = hash.take(12)
}

/** What a block failing to resync would cost if it is lost. */
enum class GarageBlockImpact(val wire: String) {
    /** A live object or upload references it. */
    LIVE("live"),

    /** Only a reference to deleted metadata keeps it. */
    STALE_REF("stale-ref"),

    /** Deleted data awaiting garbage collection. */
    CLEANUP("cleanup"),

    /** Not looked up, or the lookup failed. */
    UNKNOWN("unknown"),
    ;

    companion object {
        fun from(wire: String): GarageBlockImpact = entries.firstOrNull { it.wire == wire } ?: UNKNOWN
    }
}

@Serializable
data class GarageBlockRef(
    /** object, upload or version. */
    val kind: String = "",
    /** Bucket ID: Garage does not name buckets there. */
    val bucket: String = "",
    val key: String = "",
    val uploadId: String = "",
    val version: String = "",
    val live: Boolean = false,
) {
    /** "bucket/key"; the upload or the bare version when no key is known. */
    val label: String
        get() = when {
            key.isNotEmpty() -> listOf(bucket.take(16), key).filter { it.isNotEmpty() }.joinToString("/")
            uploadId.isNotEmpty() -> "upload ${uploadId.take(16)}"
            else -> "version ${version.take(16)}"
        }
}

@Serializable
data class GarageRepairResult(
    /** A block-refs repair was launched on every node. */
    val blockRefs: Boolean = false,
    /** A block-rc repair was launched on every node. */
    val blockRc: Boolean = false,
    /** One was already running: none launched again. */
    val repairsRunning: Boolean = false,
    /** A node did not answer: no metadata repair launched. */
    val unreachable: Boolean = false,
    /** Resyncs rescheduled. */
    val retried: Long = 0,
    val errors: List<String> = emptyList(),
) {
    /** What the summary says, in order (errors aside); the UI words each part. */
    val outcomes: List<GarageRepairOutcome>
        get() = buildList {
            when {
                blockRefs && blockRc -> add(GarageRepairOutcome.BOTH_LAUNCHED)
                blockRefs -> add(GarageRepairOutcome.REFS_LAUNCHED)
                blockRc -> add(GarageRepairOutcome.RC_LAUNCHED)
            }
            if (repairsRunning) add(GarageRepairOutcome.ALREADY_RUNNING)
            if (unreachable) add(GarageRepairOutcome.UNREACHABLE)
            if (retried > 0) add(GarageRepairOutcome.RETRIED)
            if (isEmpty() && errors.isEmpty()) add(GarageRepairOutcome.NOTHING)
        }
}

enum class GarageRepairOutcome { BOTH_LAUNCHED, REFS_LAUNCHED, RC_LAUNCHED, ALREADY_RUNNING, UNREACHABLE, RETRIED, NOTHING }
