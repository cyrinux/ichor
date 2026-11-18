package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/node_files.go.

/** One directory of a node: directories first, then by name, at most 2000 entries ([truncated]). */
@Serializable
data class NodeDirListing(
    val dir: String,
    val roots: List<String> = emptyList(),
    val entries: List<NodeFileEntry> = emptyList(),
    val truncated: Boolean = false,
)

/** An entry; [type] is file, dir or link, [modified] RFC 3339 or "", [target] a link's target. */
@Serializable
data class NodeFileEntry(
    val name: String,
    val path: String,
    val type: String = NodeFileEntry.FILE,
    val size: Long = 0,
    val modified: String = "",
    val target: String = "",
) {
    val isDir: Boolean get() = type == DIR
    val isLink: Boolean get() = type == LINK

    companion object {
        const val FILE = "file"
        const val DIR = "dir"
        const val LINK = "link"
    }
}

/** A file's text up to 512 KiB ([truncated] past it), none when [binary]; secrets [redacted]. */
@Serializable
data class NodeFileText(
    val path: String,
    val size: Long = 0,
    val text: String = "",
    val truncated: Boolean = false,
    val binary: Boolean = false,
    val redacted: Boolean = false,
)

/** One step of the path bar: [name] ("/" for the root) opens [path]. */
data class NodePathCrumb(val name: String, val path: String)

object NodePaths {
    /** The steps from "/" to [dir]: "/var/log" → "/", "var", "log". */
    fun crumbs(dir: String): List<NodePathCrumb> {
        val parts = dir.split('/').filter { it.isNotEmpty() }
        return listOf(NodePathCrumb("/", "/")) + parts.indices.map { i ->
            NodePathCrumb(parts[i], "/" + parts.subList(0, i + 1).joinToString("/"))
        }
    }

    /**
     * A typed path the core accepts: absolute, without "..". Trailing slashes and doubled
     * slashes are fine (the core cleans them); null when it would be refused.
     */
    fun typed(input: String): String? {
        val path = input.trim()
        if (!path.startsWith("/") || path.split('/').any { it == ".." }) return null
        return path
    }

    /** The file name a shared copy of [path] gets: its last part, with .txt unless it has an extension. */
    fun shareName(path: String): String {
        val name = path.substringAfterLast('/').ifEmpty { "file" }.replace(Regex("[^A-Za-z0-9._-]"), "_")
        return if (name.contains('.')) name else "$name.txt"
    }
}
