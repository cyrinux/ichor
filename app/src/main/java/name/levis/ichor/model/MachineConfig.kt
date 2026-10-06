package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Value types of the machine config tree, as the Go core names them. */
object ConfigType {
    const val OBJECT = "object"
    const val ARRAY = "array"
    const val STRING = "string"
    const val INTEGER = "integer"
    const val NUMBER = "number"
    const val BOOLEAN = "boolean"
    const val NULL = "null"

    /** A value whose type nothing tells: the core reads what is typed as YAML. */
    const val ANY = "any"

    /** What a new value can be when its type is not known. */
    val choices = listOf(STRING, INTEGER, NUMBER, BOOLEAN, OBJECT, ARRAY)
}

/** Whether the Talos config schema of the node's version could be had (downloaded once, then kept). */
@Serializable
data class ConfigSchemaStatus(
    val version: String = "",
    val available: Boolean = false,
    val source: String = "",
    val reason: String = "",
)

/** Why a draft is not valid YAML; [line] is 1-based, 0 when unknown. */
@Serializable
data class ConfigSyntaxError(val line: Int = 0, val message: String = "")

/** A field the schema knows that an object does not set yet. */
@Serializable
data class ConfigAddable(
    val key: String,
    val type: String = ConfigType.ANY,
    val description: String = "",
    @SerialName("enum") val allowed: List<String> = emptyList(),
)

/** One field of the machine config, with what the schema says about it. */
@Serializable
data class ConfigNode(
    val key: String = "",
    val path: List<String> = emptyList(),
    val type: String = ConfigType.STRING,
    val value: String = "",
    val title: String = "",
    val description: String = "",
    @SerialName("enum") val allowed: List<String> = emptyList(),
    /** A hidden secret: shown as such, never editable. */
    val redacted: Boolean = false,
    val children: List<ConfigNode> = emptyList(),
    val addable: List<ConfigAddable> = emptyList(),
    /** The type of the values this object takes under any key; empty when it only has its declared fields. */
    val freeKeyType: String = "",
    /** The type of this list's items. */
    val itemType: String = "",
) {
    val isContainer: Boolean get() = type == ConfigType.OBJECT || type == ConfigType.ARRAY

    /** Whether something can be added: a declared field, a free key, or a list item. */
    val canGrow: Boolean get() = type == ConfigType.ARRAY || addable.isNotEmpty() || freeKeyType.isNotEmpty()
}

@Serializable
data class ConfigDocument(val index: Int = 0, val title: String = "", val node: ConfigNode = ConfigNode())

/** A machine config as a tree; [error] instead of documents when the text is not valid YAML. */
@Serializable
data class ConfigTree(
    val schema: Boolean = false,
    val documents: List<ConfigDocument> = emptyList(),
    val error: ConfigSyntaxError? = null,
)

/** One change to a draft, applied by the Go core. */
@Serializable
data class ConfigEdit(
    val doc: Int,
    val path: List<String>,
    val op: String,
    val key: String = "",
    val type: String = "",
    val value: String = "",
) {
    companion object {
        fun set(doc: Int, path: List<String>, type: String, value: String) = ConfigEdit(doc, path, "set", type = type, value = value)

        /** [key] is empty to append to a list. */
        fun add(doc: Int, path: List<String>, key: String, type: String, value: String) = ConfigEdit(doc, path, "add", key, type, value)

        fun remove(doc: Int, path: List<String>) = ConfigEdit(doc, path, "remove")
    }
}

@Serializable
data class ConfigDiffLine(val kind: String = CONTEXT, val text: String = "") {
    companion object {
        const val HUNK = "hunk"
        const val CONTEXT = "context"
        const val ADDED = "added"
        const val REMOVED = "removed"
    }
}

/** What applying a draft would change; [needsReboot] changes cannot be tried. */
@Serializable
data class ConfigPreview(
    val changed: Boolean = false,
    val lines: List<ConfigDiffLine> = emptyList(),
    val needsReboot: Boolean = false,
)

/** A step of a config being tried; [deadline] (epoch millis, 0 when none) is when the node reverts. */
@Serializable
data class ConfigTryProgress(val phase: String = "", val message: String = "", val deadline: Long = 0, val at: Long = 0)

sealed interface ConfigTryEvent {
    data class Progress(val progress: ConfigTryProgress) : ConfigTryEvent

    /** [outcome] is "kept" or "reverted"; empty with [error] set when the try failed. */
    data class Done(val outcome: String, val error: String) : ConfigTryEvent
}

enum class ConfigTryCommand { KEEP, REVERT }

/** Where a try stands, for the screen. */
sealed interface ConfigTryState {
    /** [message]: why the last keep or revert failed while the try goes on. */
    data class Running(val phase: String, val message: String = "", val deadline: Long = 0) : ConfigTryState {
        /** The node holds the change and waits: it can be kept or reverted. */
        val trying: Boolean get() = phase == TRYING
    }

    data object Kept : ConfigTryState
    data object Reverted : ConfigTryState
    data class Failed(val message: String) : ConfigTryState

    companion object {
        const val APPLYING = "applying"
        const val TRYING = "trying"
        const val KEEPING = "keeping"
        const val REVERTING = "reverting"
    }
}

/** The state after [event]. The deadline of the try is kept through the phases that carry none. */
fun ConfigTryState?.after(event: ConfigTryEvent): ConfigTryState = when (event) {
    is ConfigTryEvent.Progress -> ConfigTryState.Running(
        phase = event.progress.phase,
        message = event.progress.message,
        deadline = event.progress.deadline.takeIf { it > 0 } ?: (this as? ConfigTryState.Running)?.deadline ?: 0,
    )
    is ConfigTryEvent.Done -> when (event.outcome) {
        "kept" -> ConfigTryState.Kept
        "reverted" -> ConfigTryState.Reverted
        else -> ConfigTryState.Failed(event.error)
    }
}

/** The try durations offered, in seconds (the Go core refuses any other). */
val CONFIG_TRY_TIMEOUTS = listOf(60, 300, 600)
const val CONFIG_TRY_DEFAULT_TIMEOUT = 300

/** Seconds from [nowMs] to [deadlineMs], rounded up, never negative. */
fun secondsLeft(deadlineMs: Long, nowMs: Long): Int = ((deadlineMs - nowMs + 999) / 1000).coerceAtLeast(0).toInt()

/** [seconds] as m:ss. */
fun countdownText(seconds: Int): String = "%d:%02d".format(seconds / 60, seconds % 60)

/** The types a new value of schema type [type] can take: itself, or any when the schema does not tell. */
fun valueTypes(type: String): List<String> = if (type in ConfigType.choices) listOf(type) else ConfigType.choices

/** This node with only what matches [query] (its key, value or a descendant's); null when nothing does. */
fun ConfigNode.filtered(query: String): ConfigNode? {
    val q = query.trim()
    if (q.isEmpty() || key.contains(q, ignoreCase = true) || !redacted && value.contains(q, ignoreCase = true)) return this
    val kept = children.mapNotNull { it.filtered(q) }
    return if (kept.isEmpty()) null else copy(children = kept)
}

/** A line of the tree as a flat list: a document title or a field at [depth]. */
sealed interface ConfigRow {
    val id: String

    data class Title(val doc: Int, val title: String) : ConfigRow {
        override val id = "doc-$doc"
    }

    /** [inList]: an item of a list, whose key is its index. */
    data class Field(val doc: Int, val node: ConfigNode, val depth: Int, val expanded: Boolean, val inList: Boolean = false) : ConfigRow {
        override val id = fieldId(doc, node.path)
        val isRoot: Boolean get() = node.path.isEmpty()
    }
}

fun fieldId(doc: Int, path: List<String>): String = "$doc\u001F" + path.joinToString("\u001F")

/**
 * The tree as rows. A container shows its children when its id is in [expanded]; while
 * searching, everything that matches [query] is shown open. A document's own root is not a
 * row: its fields come right under the title.
 */
fun ConfigTree.rows(expanded: Set<String>, query: String = ""): List<ConfigRow> = buildList {
    val searching = query.isNotBlank()

    fun walk(doc: Int, node: ConfigNode, depth: Int, inList: Boolean) {
        val open = node.isContainer && (searching || fieldId(doc, node.path) in expanded)
        add(ConfigRow.Field(doc, node, depth, open, inList))
        if (open) node.children.forEach { walk(doc, it, depth + 1, node.type == ConfigType.ARRAY) }
    }

    for (d in documents) {
        val root = d.node.filtered(query) ?: continue
        add(ConfigRow.Title(d.index, d.title))
        add(ConfigRow.Field(d.index, root, 0, expanded = true))
        root.children.forEach { walk(d.index, it, 1, inList = false) }
    }
}
