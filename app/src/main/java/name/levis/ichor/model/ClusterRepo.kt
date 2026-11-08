package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * A cluster's own Git repository (its Talos patches, its Kubernetes tree), saved per cluster:
 * where it is, the branch to read ("" for the default one) and the two directories of a
 * monorepo. No secret: the token stays with its Git host.
 */
@Serializable
data class ClusterRepo(
    val repoURL: String = "",
    val branch: String = "",
    val talosDir: String = "",
    val kubernetesDir: String = "",
) {
    /** The Git host whose token reads it ("github.com"). */
    val host: String get() = normalizeGitHost(repoURL)
}

/** What `GitForgeRepo` tells of a repository: its default branch, the head of the branch read, the token's push right. */
@Serializable
data class GitRepoInfo(
    val defaultBranch: String = "",
    val branch: String = "",
    val headSha: String = "",
    val webURL: String = "",
    val private: Boolean = false,
    val canPush: Boolean = false,
    val forge: String = "",
)

/** The demo cluster's built-in repository (`demoClusterRepoURL` in the Go core). */
val DEMO_CLUSTER_REPO = ClusterRepo("https://git.home.example/example-org/cluster-config", talosDir = "talos", kubernetesDir = "kubernetes")

/** [repo] as typed, trimmed and its directories cleaned; null without an address (nothing to save). */
fun normalizeClusterRepo(repo: ClusterRepo): ClusterRepo? {
    val url = repo.repoURL.trim()
    if (url.isEmpty()) return null
    return ClusterRepo(url, repo.branch.trim(), cleanSourceDir(repo.talosDir), cleanSourceDir(repo.kubernetesDir))
}

/**
 * [repo] as a source of the source browser: the whole repository from its root, read through
 * the forge at [info]'s head commit (so the viewer's "at commit" and the file sha checks work).
 */
fun repoSource(repo: ClusterRepo, info: GitRepoInfo): GitSource = GitSource(
    repoURL = repo.repoURL,
    webURL = info.webURL.ifEmpty { repoWebUrl(repo.repoURL) },
    tracked = info.branch.ifEmpty { repo.branch.ifEmpty { info.defaultBranch } },
    revision = info.headSha,
    path = "",
    kind = "directory",
    browsable = true,
)

/** The crumb of a repository's root: "main @ 1a2b3c4". */
fun repoCrumb(info: GitRepoInfo): String {
    val sha = shortRevision(info.headSha)
    return if (sha.isEmpty()) info.branch else "${info.branch} @ $sha"
}

/** The https page of a repository address typed as https, ssh or scp-like (git@host:org/repo.git). */
fun repoWebUrl(repoURL: String): String {
    val host = normalizeGitHost(repoURL)
    if (host.isEmpty()) return ""
    val raw = repoURL.trim()
    val path = when {
        "://" in raw -> raw.substringAfter("://").substringAfter('/', "")
        ':' in raw -> raw.substringAfter(':')
        else -> ""
    }.trim('/').removeSuffix(".git")
    return if (path.isEmpty()) "" else "https://$host/$path"
}

private val repoJson = Json { ignoreUnknownKeys = true }

fun decodeGitRepoInfo(json: String): GitRepoInfo = repoJson.decodeFromString(GitRepoInfo.serializer(), json)

fun encodeClusterRepo(repo: ClusterRepo): String = repoJson.encodeToString(ClusterRepo.serializer(), repo)

fun decodeClusterRepo(json: String): ClusterRepo? =
    runCatching { repoJson.decodeFromString(ClusterRepo.serializer(), json) }.getOrNull()?.let(::normalizeClusterRepo)
