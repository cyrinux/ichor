package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

// Mirrors go/ichorgo/kube_node_meta.go

/** The cordon's taint: Cordon / Uncordon owns it, the editor never sends it (the core keeps the server's copy). */
const val UNSCHEDULABLE_TAINT = "node.kubernetes.io/unschedulable"

/** The taint effects, shown verbatim. */
val TAINT_EFFECTS = listOf("NoSchedule", "PreferNoSchedule", "NoExecute")

/**
 * A taint of the node. [timeAdded]: unix seconds, 0 when the API server did not set it.
 * [protected]: set by Kubernetes, Talos or the cloud provider; changed only with force.
 */
@Serializable
data class NodeTaint(
    val key: String = "",
    val value: String = "",
    val effect: String = "",
    val timeAdded: Long = 0,
    val protected: Boolean = false,
) {
    /** The taint as the edits carry it. */
    val edit: NodeTaintEdit get() = NodeTaintEdit(key, value, effect)
}

/** A label of the node; [protected] as for [NodeTaint]. */
@Serializable
data class NodeLabel(val key: String = "", val value: String = "", val protected: Boolean = false)

/** A node's taints and labels, sorted by key, read at [resourceVersion] (KubeNodeMeta / NodeMeta). */
@Serializable
data class KubeNodeMeta(
    val node: String = "",
    val resourceVersion: String = "",
    val taints: List<NodeTaint> = emptyList(),
    val labels: List<NodeLabel> = emptyList(),
) {
    /** The labels as a map, key → value. */
    val labelMap: Map<String, String> get() = labels.associate { it.key to it.value }

    /** Whether [key] is a protected label. */
    fun labelProtected(key: String): Boolean = labels.any { it.key == key && it.protected } || protectedNodeKey(key)

    /** Whether the taint [key] is protected (the cordon's always is). */
    fun taintProtected(key: String): Boolean = taints.any { it.key == key && it.protected } || protectedNodeKey(key)
}

/** A taint in the edits: its [key], [value] ("" for none) and [effect] (one of [TAINT_EFFECTS]). */
@Serializable
data class NodeTaintEdit(val key: String = "", val value: String = "", val effect: String = "") {
    /** "key=value:Effect", as kubectl shows it. */
    val display: String get() = if (value.isEmpty()) "$key:$effect" else "$key=$value:$effect"
}

/**
 * What to change on a node, the same document for the plan and the apply: labels as deltas
 * ([setLabels], [removeLabels]) so a stale copy never wipes the others, [taints] as the whole
 * desired list (without the cordon's). [resourceVersion]: the read the edits start from, sent as
 * a precondition (the node changed since: refused, reload).
 */
@Serializable
data class NodeMetaEdits(
    val resourceVersion: String = "",
    val setLabels: Map<String, String> = emptyMap(),
    val removeLabels: List<String> = emptyList(),
    val taints: List<NodeTaintEdit> = emptyList(),
    /** Protected keys may change ("Show protected"); the plan only sees it here. */
    val force: Boolean = false,
) {
    /** The JSON the core parses (parseNodeMetaEdits). */
    fun toJson(): String = EditsJson.encodeToString(serializer(), this)

    /** Whether [taints] differ from [original]'s (order aside, the cordon's taint ignored). */
    fun hasTaintChanges(original: KubeNodeMeta): Boolean =
        taints.toSet() != original.taints.filter { it.key != UNSCHEDULABLE_TAINT }.map { it.edit }.toSet()

    /** Whether the edits change nothing of [original]. */
    fun isEmpty(original: KubeNodeMeta): Boolean =
        setLabels.isEmpty() && removeLabels.isEmpty() && !hasTaintChanges(original)

    companion object {
        /**
         * The edits from [original] to the desired [taints] and [labels]: the label deltas, the
         * taints whole (the cordon's taint left out, the core keeps it).
         */
        fun between(original: KubeNodeMeta, taints: List<NodeTaintEdit>, labels: Map<String, String>, force: Boolean = false): NodeMetaEdits {
            val before = original.labelMap
            return NodeMetaEdits(
                resourceVersion = original.resourceVersion,
                setLabels = labels.filter { (key, value) -> before[key] != value }.toSortedMap(),
                removeLabels = before.keys.filter { it !in labels }.sorted(),
                taints = taints.filter { it.key != UNSCHEDULABLE_TAINT },
                force = force,
            )
        }
    }
}

/** Every field written, empty ones too: the core reads an absent `taints` like an empty one. */
private val EditsJson = Json { encodeDefaults = true }

/** The well-known key prefixes the core protects (protectedNodeKeyPrefixes), to lock rows added locally too. */
private val PROTECTED_NODE_KEY_PREFIXES = listOf(
    "kubernetes.io/", "k8s.io/", "node-role.kubernetes.io/", "node.kubernetes.io/", "topology.kubernetes.io/",
    "beta.kubernetes.io/", "talos.dev/", "eks.amazonaws.com/", "cloud.google.com/", "kubernetes.azure.com/", "karpenter.sh/",
)

/** Whether [key] is under a well-known prefix, as the core decides it. */
fun protectedNodeKey(key: String): Boolean = PROTECTED_NODE_KEY_PREFIXES.any { key.startsWith(it) }

/** A label or a taint in a plan's added or removed list. */
@Serializable
data class NodeKeyValue(val key: String = "", val value: String = "")

/** A label whose value changes from [from] to [to]. */
@Serializable
data class NodeLabelChange(val key: String = "", val from: String = "", val to: String = "")

/** The labels a plan adds, removes and changes. */
@Serializable
data class NodeLabelDiff(
    val added: List<NodeKeyValue> = emptyList(),
    val removed: List<NodeKeyValue> = emptyList(),
    val changed: List<NodeLabelChange> = emptyList(),
) {
    val isEmpty: Boolean get() = added.isEmpty() && removed.isEmpty() && changed.isEmpty()
}

/**
 * A taint that changes: [from] and [to] are its value, or the whole taint ({value, effect}),
 * kept as JSON so either shape reads (see [fromText], [toText]).
 */
@Serializable
data class NodeTaintChange(
    val key: String = "",
    val effect: String = "",
    val from: JsonElement? = null,
    val to: JsonElement? = null,
) {
    val fromText: String get() = taintSide(from)
    val toText: String get() = taintSide(to)
}

/** One side of a taint change as text: the value itself, or "value:Effect" for a whole taint. */
private fun taintSide(side: JsonElement?): String = when (side) {
    null -> ""
    is JsonPrimitive -> side.contentOrNull.orEmpty()
    is JsonObject -> {
        val value = (side["value"] as? JsonPrimitive)?.contentOrNull.orEmpty()
        val effect = (side["effect"] as? JsonPrimitive)?.contentOrNull.orEmpty()
        listOf(value, effect).filter { it.isNotEmpty() }.joinToString(":")
    }
    else -> side.toString()
}

/** The taints a plan adds, removes and changes. */
@Serializable
data class NodeTaintDiff(
    val added: List<NodeTaintEdit> = emptyList(),
    val removed: List<NodeTaintEdit> = emptyList(),
    val changed: List<NodeTaintChange> = emptyList(),
) {
    val isEmpty: Boolean get() = added.isEmpty() && removed.isEmpty() && changed.isEmpty()
}

/** A pod a new NoExecute taint evicts at once. */
@Serializable
data class NodeEviction(val namespace: String = "", val name: String = "", val owner: String = "")

/**
 * What the edits would do to [node] (KubeNodeMetaPlan / NodeMetaPlan, a dry run): the diff, the
 * pods evicted, what refuses the apply ([blockers], the API server's own words among them) and
 * what to know first ([warnings]).
 */
@Serializable
data class NodeMetaPlan(
    val node: String = "",
    val labels: NodeLabelDiff = NodeLabelDiff(),
    val taints: NodeTaintDiff = NodeTaintDiff(),
    val evicts: List<NodeEviction> = emptyList(),
    val blockers: List<String> = emptyList(),
    val warnings: List<String> = emptyList(),
    val noChange: Boolean = false,
) {
    /** Whether a taint changes: the typed hostname confirms it. */
    val hasTaintChanges: Boolean get() = !taints.isEmpty

    /** Whether Apply may run: something changes and nothing refuses it. */
    val canApply: Boolean get() = !noChange && blockers.isEmpty()

    /** The node changed since it was read: the editor must reload first. */
    val stale: Boolean get() = (blockers + warnings).any { it.contains("reload", ignoreCase = true) }
}
