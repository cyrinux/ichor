package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put

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
fun sourceRowLabel(source: GitSource): String {
    val path = source.path.ifEmpty { "/" }
    val at = shortRevision(source.ref)
    return if (at.isEmpty()) path else "$path @ $at"
}

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

/**
 * A change of one file proposed as a pull request: `GitForgePropose`'s options without the
 * repository (the source's). [fileSha] is the forge's sha of the file at [baseRef] when it was
 * read: the optimistic lock, "" for a new file.
 */
data class GitProposal(
    val path: String,
    val content: String,
    val fileSha: String,
    val baseRef: String,
    val message: String,
    val title: String,
    val body: String,
    val branch: String,
    val draft: Boolean = false,
)

/** `GitForgePropose`'s answer: the pull (merge) request opened and its branch. */
@Serializable
data class GitProposalResult(
    val url: String = "",
    val number: Int = 0,
    val branch: String = "",
    val commit: String = "",
    val forge: String = "",
)

/** `GitOpsProposalDefaults`'s answer: the prefilled pull request. */
@Serializable
data class GitProposalDefaults(
    val title: String = "",
    val message: String = "",
    val branch: String = "",
    val body: String = "",
)

/** `GitForgePropose`'s optionsJSON for [p] on the repository [repoURL]: every key, always. */
fun gitProposalJson(repoURL: String, p: GitProposal): String = buildJsonObject {
    put("repoURL", repoURL)
    put("baseRef", p.baseRef)
    put("path", p.path)
    put("content", p.content)
    put("fileSha", p.fileSha)
    put("message", p.message)
    put("branch", p.branch)
    put("title", p.title)
    put("body", p.body)
    put("draft", p.draft)
}.toString()

private val COMMIT_SHA = Regex("^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$")
private val TAG_LIKE = Regex("^[vV]?\\d+(\\.(\\d+|[xX*]))*([-+][0-9A-Za-z.-]+)?$")

// The characters of a branch name the Go core accepts (gitBranchName in gitforge_propose.go).
private val BRANCH_NAME = Regex("^[\\w./-]+$")

/**
 * The branch a pull request from a source following [tracked] goes to: [tracked] itself when it
 * names a branch ("refs/heads/" dropped); null when it is a commit, a tag ("v1.2.3",
 * "refs/tags/…"), a semver range, "HEAD" or nothing, since a pull request cannot target those:
 * the user names the branch then ([DEFAULT_PROPOSAL_BASE] at first).
 */
fun proposalBaseRef(tracked: String): String? {
    val ref = tracked.trim()
    if (ref.startsWith("refs/heads/")) {
        return ref.removePrefix("refs/heads/").takeIf { BRANCH_NAME.matches(it) && !it.startsWith("-") && ".." !in it }
    }
    return when {
        ref.isEmpty() || ref == "HEAD" || ref.startsWith("refs/") -> null
        COMMIT_SHA.matches(ref) || TAG_LIKE.matches(ref) -> null
        !BRANCH_NAME.matches(ref) || ref.startsWith("-") || ".." in ref -> null
        else -> ref
    }
}

/** The branch offered when [proposalBaseRef] has none. */
const val DEFAULT_PROPOSAL_BASE = "main"

/** Why the files of a source cannot be proposed as a pull request. */
sealed interface ProposeBlock {
    /** Not on a Git host Ichor talks to: no pull request can be opened from the phone. */
    data object NotEditable : ProposeBlock

    /** No token is saved for [host]. */
    data class NoToken(val host: String) : ProposeBlock
}

/**
 * What stops proposing a change to [source]'s files, null when nothing does: proposing always
 * goes through the forge, so it needs a web host and a token for it ([lacksToken], see
 * [gitHostJsonLacksToken]), even when the files are read from a Flux artifact.
 */
fun proposeBlock(source: GitSource, lacksToken: Boolean): ProposeBlock? = when {
    !source.browsable || source.isChart || source.host.isEmpty() -> ProposeBlock.NotEditable
    lacksToken -> ProposeBlock.NoToken(source.host)
    else -> null
}

/** Whether [message] is the core's refusal of a file changed upstream since it was read. */
fun isStaleFileError(message: String?): Boolean = message?.contains("since you opened it") == true

/** The tool of a source ("argocd", "flux") as the apps name it. */
fun gitOpsToolLabel(tool: String): String = if (tool == "flux") "Flux" else "Argo CD"

/** The Kubernetes type a manifest declares: its `apiVersion` split into [group] and [version], and [kind]. */
data class ManifestType(val group: String, val version: String, val kind: String)

private val KUSTOMIZATION_FILES = setOf("kustomization.yaml", "kustomization.yml")
private const val KUSTOMIZE_GROUP = "kustomize.config.k8s.io"

/**
 * The type the YAML file [path] declares in its first document, from its first top-level
 * `apiVersion:` and `kind:` lines, for the editor's schema help; null for a kustomization, a
 * values file or anything else that is not a Kubernetes manifest.
 */
fun manifestType(path: String, text: String): ManifestType? {
    if (!isYamlPath(path) || path.substringAfterLast('/').lowercase() in KUSTOMIZATION_FILES) return null
    var api: String? = null
    var kind: String? = null
    for (line in text.lineSequence()) {
        if (line.trimEnd() == "---" && (api != null || kind != null)) break
        if (api == null) api = topLevelValue(line, "apiVersion")
        if (kind == null) kind = topLevelValue(line, "kind")
        if (api != null && kind != null) break
    }
    val apiVersion = api ?: return null
    val k = kind ?: return null
    val group = if ('/' in apiVersion) apiVersion.substringBefore('/') else ""
    if (group == KUSTOMIZE_GROUP) return null
    return ManifestType(group, apiVersion.substringAfter('/'), k)
}

/** The value of the top-level [key] on [line], unquoted and without its comment; null otherwise. */
private fun topLevelValue(line: String, key: String): String? {
    if (!line.startsWith("$key:")) return null
    val value = line.substring(key.length + 1).substringBefore(" #").trim().trim('"', '\'')
    return value.ifEmpty { null }
}

/** A manifest that declares a resource: its file [path], the [line] (1-based) of its `metadata.name`. */
@Serializable
data class ManifestMatch(
    val path: String = "",
    val line: Int = 0,
    val documentIndex: Int = 0,
)

/**
 * `GitOpsSourceFind`'s answer: the manifests of a source that declare a resource, the number of
 * files [searched], and a [note] when none can match (a Helm-rendered app) or a name was rewritten.
 */
@Serializable
data class GitOpsSourceFind(
    val matches: List<ManifestMatch> = emptyList(),
    val searched: Int = 0,
    val note: String = "",
)

/**
 * The source a diff's "Open source file" searches: the first Git source whose files can be read
 * (a chart or a source read only for its value files is skipped); null when there is none.
 */
fun findableSourceIndex(source: GitOpsSource): Int? {
    val readable = source.sources.withIndex().filter { (_, s) -> !s.isValuesOnly && !s.isChart && s.readPath != SourceReadPath.NONE }
    // A Helm source only gives the "rendered by Helm" note: another readable source comes first.
    return (readable.firstOrNull { (_, s) -> s.kind != "helm" } ?: readable.firstOrNull())?.index
}

/** Whether a diff object can be looked up in Git: one about to be deleted is no longer declared there. */
fun isFindableChange(change: DiffChange): Boolean = change != DiffChange.DELETED
