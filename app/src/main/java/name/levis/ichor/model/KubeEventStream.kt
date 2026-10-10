package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_events_live.go and kube_events_coalesce.go (StartKubeEvents).

/** Most rows kept, as the Go core keeps them: its `removed` keys keep both in step. */
const val KUBE_EVENTS_CAP = 2000

/** The object an event is about; [resource] empty when discovery does not know its kind. */
@Serializable
data class KubeEventObject(
    val kind: String = "",
    val apiVersion: String = "",
    val group: String = "",
    val version: String = "",
    val resource: String = "",
    val namespaced: Boolean = false,
    val namespace: String = "",
    val name: String = "",
) {
    /** "Pod shop/web-1", "Node worker-1". */
    val label: String get() = "$kind ${if (!namespaced || namespace.isEmpty()) name else "$namespace/$name"}".trim()

    /** Its object screen; null when its resource is unknown (no navigation offered). */
    val ref: KubeObjectRef?
        get() = if (resource.isEmpty() || version.isEmpty() || name.isEmpty()) null else
            KubeObjectRef(group, version, resource, kind, if (namespaced) namespace else "", name, editable = true)
}

/** Event objects of one object, reason and type coalesced into one row (kubeEventRow). */
@Serializable
data class LiveKubeEvent(
    /** Stable for the row's life. */
    val key: String = "",
    /** Normal or Warning. */
    val type: String = "",
    val reason: String = "",
    /** The latest event's message. */
    val note: String = "",
    val regarding: KubeEventObject = KubeEventObject(),
    val count: Int = 1,
    /** Unix millis. */
    val firstSeen: Long = 0,
    /** Unix millis. */
    val lastSeen: Long = 0,
    val source: String = "",
) {
    val isWarning: Boolean get() = type == "Warning"
}

/** One OnEvents call: apply [reset], then [removed], then [upserts]. */
@Serializable
data class KubeEventsBatch(
    val reset: Boolean = false,
    val upserts: List<LiveKubeEvent> = emptyList(),
    val removed: List<String> = emptyList(),
) {
    /** This batch then [next] as one batch: what [applyTo] of both in turn would give. */
    fun then(next: KubeEventsBatch): KubeEventsBatch {
        if (next.reset) return next
        val dropped = next.removed.toSet()
        val upserts = LinkedHashMap<String, LiveKubeEvent>()
        this.upserts.filter { it.key !in dropped }.forEach { upserts[it.key] = it }
        next.upserts.forEach { upserts[it.key] = it }
        return KubeEventsBatch(reset, upserts.values.toList(), (removed + next.removed).distinct())
    }

    /** [rows] after this batch: newest lastSeen first, at most [cap]. */
    fun applyTo(rows: List<LiveKubeEvent>, cap: Int = KUBE_EVENTS_CAP): List<LiveKubeEvent> {
        val dropped = removed.toSet()
        val byKey = LinkedHashMap<String, LiveKubeEvent>()
        if (!reset) rows.filter { it.key !in dropped }.forEach { byKey[it.key] = it }
        upserts.forEach { byKey[it.key] = it }
        return byKey.values.sortedWith(compareByDescending<LiveKubeEvent> { it.lastSeen }.thenBy { it.key }).take(cap)
    }
}

/** One OnStatus call (kubeEventsStatus). */
@Serializable
data class KubeEventsStatus(
    /** live, reconnecting, relisting or polling. */
    val state: String = "",
    val reason: String = "",
    /** events.k8s.io/v1, or v1 when that is not served. */
    val api: String = "",
    val tracked: Int = 0,
    val limit: Int = 0,
    val dropped: Int = 0,
    /** The first list stopped before reading every event. */
    val partial: Boolean = false,
    /** One sentence when [partial] or rows were let go. */
    val note: String = "",
)

/**
 * The rows on screen and, while [paused], the batches received since, folded into [pending]
 * and applied on [resume].
 */
data class KubeEventsFeed(
    val rows: List<LiveKubeEvent> = emptyList(),
    val paused: Boolean = false,
    val pending: KubeEventsBatch? = null,
) {
    /** Rows new or changed since the pause. */
    val pendingCount: Int get() = pending?.upserts?.size ?: 0

    fun receive(batch: KubeEventsBatch): KubeEventsFeed =
        if (paused) copy(pending = pending?.then(batch) ?: batch) else copy(rows = batch.applyTo(rows))

    fun pause(): KubeEventsFeed = if (paused) this else copy(paused = true)

    fun resume(): KubeEventsFeed = KubeEventsFeed(rows = pending?.applyTo(rows) ?: rows)
}

/** [this] (newest first) whose reason, object or message contains [query], ignoring case. */
fun List<LiveKubeEvent>.matching(query: String): List<LiveKubeEvent> {
    val q = query.trim()
    if (q.isEmpty()) return this
    return filter { e ->
        listOf(e.reason, e.regarding.label, e.note, e.source).any { it.contains(q, ignoreCase = true) }
    }
}

/**
 * [rows] as plain text to share, one event per entry: when ([time] of its last sighting),
 * type, reason, object and how often, then its message on the next line.
 */
fun kubeEventsText(rows: List<LiveKubeEvent>, time: (Long) -> String): String =
    rows.joinToString("\n") { e ->
        val head = listOfNotNull(
            time(e.lastSeen),
            e.type,
            e.reason,
            e.regarding.label.ifEmpty { null },
            if (e.count > 1) "×${e.count}" else null,
        ).joinToString(" ")
        if (e.note.isEmpty()) head else "$head\n    ${e.note}"
    }
