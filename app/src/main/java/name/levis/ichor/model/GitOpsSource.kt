package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject

/** Where an Argo CD or Flux app's files are: `GitOpsSource`, one entry per source of the app. */
@Serializable
data class GitOpsSource(
    val sources: List<GitSource> = emptyList(),
    val note: String = "",
)

/** One source of an app: the repository, the revision the cluster runs, and how to read it. */
@Serializable
data class GitSource(
    val repoURL: String = "",
    /** The repository's page, "" when it is not a Git repository on a web host. */
    val webURL: String = "",
    /** The branch, tag, semver range or commit the app follows. */
    val tracked: String = "",
    /** The commit the cluster runs, "" when unknown. */
    val revision: String = "",
    val path: String = "",
    /** kustomize, helm, directory, chart or values. */
    val kind: String = "",
    val chart: String = "",
    val valueFiles: List<String> = emptyList(),
    /** The files can be read through the Git host's API (a token for its host). */
    val browsable: Boolean = false,
    /** The files can be read from [fluxSource]'s artifact, with no token. */
    val fluxArtifact: Boolean = false,
    val fluxSource: FluxRef? = null,
) {
    /** An Argo CD source only read for its value files: not listed on its own. */
    val isValuesOnly: Boolean get() = kind == KIND_VALUES

    /** A packaged Helm chart: no tree to browse, its name and version only. */
    val isChart: Boolean get() = kind == KIND_CHART

    /** How its files are read. */
    val readPath: SourceReadPath
        get() = when {
            fluxArtifact && fluxSource != null -> SourceReadPath.FLUX
            browsable -> SourceReadPath.FORGE
            else -> SourceReadPath.NONE
        }

    /** The ref the forge reads at: the deployed commit, else what the app follows. */
    val ref: String get() = revision.ifEmpty { tracked }

    /** The Git host whose token reads it ("github.com"), "" without a web page. */
    val host: String get() = if (webURL.isEmpty()) "" else normalizeGitHost(webURL)

    companion object {
        const val KIND_VALUES = "values"
        const val KIND_CHART = "chart"
    }
}

/** Where a source's files come from: the Flux artifact, the Git host's API, or nowhere. */
enum class SourceReadPath { FLUX, FORGE, NONE }

/** One directory of a source: `{ref, sha, entries}`, directories first. */
@Serializable
data class GitTree(
    val ref: String = "",
    val sha: String = "",
    val entries: List<GitEntry> = emptyList(),
)

/** A file or directory of a [GitTree]; [path] is from the repository root. */
@Serializable
data class GitEntry(
    val path: String = "",
    /** file or dir. */
    val type: String = "",
    val size: Long = 0,
) {
    val isDir: Boolean get() = type == "dir"
    val name: String get() = path.substringAfterLast('/')
}

/** One file of a source; [truncated]: over 1 MiB, without [content]. */
@Serializable
data class GitFile(
    val path: String = "",
    val sha: String = "",
    val size: Long = 0,
    val content: String = "",
    val truncated: Boolean = false,
)

/** The label of a source row: "apps/podinfo @ 1a2b3c4" (the followed ref when no commit is known). */
fun sourceRowLabel(source: GitSource): String =
    "${source.path.ifEmpty { "/" }} @ ${shortRevision(source.ref)}"

/** A path cleaned of its leading and trailing slashes ("" is the repository root). */
fun cleanSourceDir(dir: String): String = dir.trim().trim('/')

/** The directories from the root down to [dir], each with its full path: "a/b" gives (a, a), (b, a/b). */
fun sourceBreadcrumbs(dir: String): List<Pair<String, String>> {
    val parts = cleanSourceDir(dir).split('/').filter { it.isNotEmpty() }
    return parts.indices.map { i -> parts[i] to parts.take(i + 1).joinToString("/") }
}

/**
 * The page of the file [path] at [ref] on the forge of [webURL], when its URL scheme is known
 * from the host name (GitHub, GitLab); else the repository page. "" without a [webURL].
 */
fun forgeFileUrl(webURL: String, ref: String, path: String): String {
    if (webURL.isEmpty()) return ""
    val host = normalizeGitHost(webURL)
    val base = webURL.trimEnd('/')
    val file = cleanSourceDir(path)
    if (ref.isEmpty() || file.isEmpty()) return base
    return when {
        host == "github.com" -> "$base/blob/$ref/$file"
        host.contains("gitlab") -> "$base/-/blob/$ref/$file"
        else -> base
    }
}

/**
 * Whether a forge call's host argument ([gitHostJson]) carries no token: the Git host has none
 * saved. The demo context's `{"kind":"demo"}` never needs one.
 */
fun gitHostJsonLacksToken(json: String): Boolean {
    val obj = runCatching { Json.parseToJsonElement(json).jsonObject }.getOrNull() ?: return true
    if (obj.string("kind") == "demo") return false
    return obj.string("token").isBlank()
}

private fun JsonObject.string(key: String): String = (this[key] as? JsonPrimitive)?.takeIf { it.isString }?.content.orEmpty()

/** Whether [path] reads as YAML, for the highlighting of the file view. */
fun isYamlPath(path: String): Boolean = path.endsWith(".yaml", ignoreCase = true) || path.endsWith(".yml", ignoreCase = true)
