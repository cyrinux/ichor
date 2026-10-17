package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/** Which forge API a Git host speaks; [AUTO] lets the Go core tell from the host name. */
enum class GitHostKind(val wire: String, val label: String?) {
    AUTO("auto", null),
    GITHUB("github", "GitHub"),
    GITLAB("gitlab", "GitLab"),
    GITEA("gitea", "Gitea"),
    ;

    companion object {
        fun fromWire(wire: String?): GitHostKind = entries.firstOrNull { it.wire == wire } ?: AUTO
    }
}

/**
 * A Git host the phone has a token for. The token itself is never here (it lives in its own
 * Keystore file, see GitHostStore), so neither toString nor a serialized list can carry it.
 */
@Serializable
data class GitHost(val host: String, val kind: GitHostKind = GitHostKind.AUTO)

/** What `GitForgeCheck` answers: who the token signs in as, and the scopes it lacks. */
@Serializable
data class GitHostCheck(
    val login: String = "",
    val host: String = "",
    val scopesOK: Boolean = false,
    val missing: List<String> = emptyList(),
)

/**
 * The host as typed or pasted, reduced to what the list keys on: no scheme, no trailing slash,
 * no API path, the host part in lower case ("https://GitHub.com/" is "github.com"). A path is
 * kept for a forge served under one (`gitlab.example.org/gitlab`). Blank when nothing is left.
 */
fun normalizeGitHost(input: String): String {
    val noScheme = input.trim().replace(SCHEME, "")
    val trimmed = API_PATHS.fold(noScheme.trimEnd('/')) { acc, api -> acc.removeSuffix(api) }.trimEnd('/')
    val slash = trimmed.indexOf('/')
    val authority = if (slash < 0) trimmed else trimmed.substring(0, slash)
    val path = if (slash < 0) "" else trimmed.substring(slash)
    return authority.substringAfterLast('@').lowercase() + path
}

/**
 * The argument of the Go forge calls: `{"kind","baseURL","token"}` for [host], or
 * `{"kind":"demo"}` in the demo context (no network, no token). The token only ever travels
 * inside this string.
 */
fun gitHostJson(host: GitHost, token: String, demo: Boolean): String =
    if (demo) {
        buildJsonObject { put("kind", "demo") }.toString()
    } else {
        buildJsonObject {
            put("kind", host.kind.wire)
            put("baseURL", "https://${normalizeGitHost(host.host)}")
            put("token", token.trim())
        }.toString()
    }

private val SCHEME = Regex("^[a-zA-Z][a-zA-Z0-9+.-]*://")
private val API_PATHS = listOf("/api/v1", "/api/v3", "/api/v4")
