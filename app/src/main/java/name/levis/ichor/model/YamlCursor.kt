package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * Where the cursor of the YAML editor is, as the field path the schema help (KubeExplain)
 * walks: "spec.template.spec.containers.image". List items are not in the path: the schema
 * walks an array through its items.
 *
 * Read from indentation alone, line by line upwards, so it works on YAML that does not parse
 * yet. Limits: block-style YAML only (flow style `{a: 1}` / `[a]` is one value), the current
 * document only (stops at `---`), and a line inside a multi-line string (`|`, `>`) reads as
 * if it were a key.
 */
data class YamlCursor(
    /** The keys of the maps holding the cursor's line, outermost first. */
    val parents: List<String>,
    /** The key on the cursor's line, null on a blank line or a list scalar ("- a"). */
    val key: String?,
    /** The key's column, or the cursor's column on a blank line. */
    val column: Int,
    /** The line's key opens a block ("spec:"): fields are added inside it. */
    val opensBlock: Boolean,
) {
    /** What "Explain field" describes: the key's path, or the map holding a blank line. */
    val fieldPath: String get() = (parents + listOfNotNull(key)).joinToString(".")

    /** The map whose fields "Add field" lists: the block the line opens, else the one holding it. */
    val addPath: String get() = if (opensBlock) fieldPath else parents.joinToString(".")

    /** The column a field added by "Add field" starts at. */
    val addColumn: Int get() = if (opensBlock) column + 2 else column
}

/** The schema help's answer for one field (KubeExplain). */
@Serializable
data class KubeExplain(
    val path: String = "",
    val type: String = "",
    val format: String = "",
    val description: String = "",
    val enum: List<String> = emptyList(),
    val required: Boolean = false,
    val children: List<KubeExplainChild> = emptyList(),
) {
    /** An array: a field added to it is a new list item. */
    val isList: Boolean get() = type.startsWith("[]")
}

/** One field of the object explained, its description cut to the first sentence. */
@Serializable
data class KubeExplainChild(
    val name: String = "",
    val type: String = "",
    val description: String = "",
    val required: Boolean = false,
    val enum: List<String> = emptyList(),
)

/** The start of the Go error when the cluster has no schema help to give. */
const val SCHEMA_HELP_UNAVAILABLE = "schema help unavailable"

/** One line of YAML read for its structure: the columns of its list dashes and its key. */
private class YamlLine(val dashes: List<Int>, val key: String?, val column: Int, val value: String) {
    /** A key whose value is the block below it, not a scalar on the line. */
    val opensBlock: Boolean get() = key != null && (value.isEmpty() || value[0] in "|>&!")
}

private fun parseYamlLine(line: String): YamlLine? {
    var i = 0
    while (i < line.length && line[i] == ' ') i++
    if (i >= line.length || line[i] == '#') return null
    val dashes = mutableListOf<Int>()
    while (i < line.length && line[i] == '-' && (i + 1 == line.length || line[i + 1] == ' ')) {
        dashes += i
        i++
        while (i < line.length && line[i] == ' ') i++
    }
    if (i >= line.length || line[i] == '#') return YamlLine(dashes, null, i, "")
    val end = keyEnd(line, i) ?: return YamlLine(dashes, null, i, line.substring(i))
    val key = line.substring(i, end).trim().removeSurrounding("\"").removeSurrounding("'")
    var value = line.substring(end + 1).trim()
    if (value.startsWith("#")) value = ""
    return YamlLine(dashes, key, i, value)
}

/** The index of the colon ending the key starting at [start], null when the line has none. */
private fun keyEnd(line: String, start: Int): Int? {
    val first = line[start]
    if (first in "{[|>&*!%@`") return null
    if (first == '"' || first == '\'') {
        val close = line.indexOf(first, start + 1)
        if (close < 0 || close + 1 >= line.length || line[close + 1] != ':') return null
        return (close + 1).takeIf { it + 1 == line.length || line[it + 1] == ' ' }
    }
    for (j in start until line.length) {
        when {
            line[j] == '#' && j > start && line[j - 1] == ' ' -> return null
            line[j] == ':' && (j + 1 == line.length || line[j + 1] == ' ') -> return j
        }
    }
    return null
}

private fun isDocumentMarker(line: String) = line.startsWith("---") || line.startsWith("...")

/** The cursor at [offset] (a UTF-16 index, as text fields count) of [text]. */
fun yamlCursorAt(text: String, offset: Int): YamlCursor {
    val at = offset.coerceIn(0, text.length)
    val lines = text.split('\n').map { it.trimEnd('\r') }
    val lineIndex = text.substring(0, at).count { it == '\n' }
    val lineStart = text.lastIndexOf('\n', at - 1) + 1
    val current = parseYamlLine(lines[lineIndex])

    var limit: Int
    var afterDash = false
    if (current == null) {
        // A blank line counts from the cursor's column, a comment from its indentation.
        val raw = lines[lineIndex]
        limit = if (raw.isBlank()) at - lineStart else raw.takeWhile { it == ' ' }.length
    } else {
        limit = current.column
        current.dashes.firstOrNull()?.let {
            limit = it
            afterDash = true
        }
    }
    val column = current?.column ?: limit

    val parents = ArrayDeque<String>()
    for (i in lineIndex - 1 downTo 0) {
        if (isDocumentMarker(lines[i])) break
        if (limit == 0 && !afterDash) break
        val line = parseYamlLine(lines[i]) ?: continue
        val key = line.key
        val parent = key != null && line.opensBlock && (if (afterDash) line.column <= limit else line.column < limit)
        if (parent) {
            parents.addFirst(key!!)
            limit = line.column
            afterDash = false
        }
        val dash = line.dashes.firstOrNull()
        if (dash != null && dash < limit) {
            limit = dash
            afterDash = true
        }
    }
    return YamlCursor(parents.toList(), current?.key, column, current?.opensBlock == true)
}

/**
 * [text] with `name: ` added where "Add field" adds at [offset]: on the cursor's line when
 * blank, else on a new line below it; as a list item ("- name: ") when [listItem]. Returns
 * the text and the cursor after the inserted colon.
 */
fun insertYamlField(text: String, offset: Int, name: String, listItem: Boolean): Pair<String, Int> {
    val at = offset.coerceIn(0, text.length)
    val cursor = yamlCursorAt(text, at)
    val lineStart = text.lastIndexOf('\n', at - 1) + 1
    val lineEnd = text.indexOf('\n', at).let { if (it < 0) text.length else it }
    val entry = " ".repeat(cursor.addColumn) + (if (listItem) "- " else "") + "$name: "
    return if (text.substring(lineStart, lineEnd).isBlank()) {
        text.substring(0, lineStart) + entry + text.substring(lineEnd) to lineStart + entry.length
    } else {
        text.substring(0, lineEnd) + "\n" + entry + text.substring(lineEnd) to lineEnd + 1 + entry.length
    }
}
