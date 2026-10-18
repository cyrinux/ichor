package name.levis.ichor.model

import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/** Which forge API a Git host speaks; [AUTO] lets the Go core tell from the host name. */
enum class GitHostKind(val wire: String, val label: String?) {
    AUTO("auto", null),
    GITHUB("github", "GitHub"),
    GITLAB("gitlab", "GitLab"),
    GITEA("gitea", "Gitea/Forgejo"),
    ;

    companion object {
        fun fromWire(wire: String?): GitHostKind = entries.firstOrNull { it.wire == wire } ?: AUTO
    }
}

/**
 * A Git host the phone has a token for. The token itself is never here (it lives in its own
 * Keystore file, see GitHostStore), so neither toString nor a serialized list can carry it.
 * [aliases] are the other names Argo CD or Flux reach the same server by (an SSH or in-cluster
 * address): a source on one of them uses this host's token, still sent only to [host].
 */
@Serializable
data class GitHost(
    val host: String,
    @Serializable(with = GitHostKindSerializer::class) val kind: GitHostKind = GitHostKind.AUTO,
    val aliases: List<String> = emptyList(),
) {
    /**
     * Whether a source on [host] is this Git host: its own name or one of its [aliases]. Ports
     * are ignored: a source's host comes from its web link, which has none, and this only picks
     * whose token to use; the Go core still checks the exact host and port of the repository.
     */
    fun matches(host: String): Boolean {
        val key = gitHostName(normalizeGitHost(host))
        if (key.isEmpty()) return false
        return (listOf(this.host) + aliases).any { gitHostName(normalizeGitHost(it)) == key }
    }
}

/** [GitHostKind] by its wire name; one a newer app wrote reads as [GitHostKind.AUTO]. */
object GitHostKindSerializer : KSerializer<GitHostKind> {
    override val descriptor = PrimitiveSerialDescriptor("GitHostKind", PrimitiveKind.STRING)

    override fun serialize(encoder: Encoder, value: GitHostKind) = encoder.encodeString(value.wire)

    override fun deserialize(decoder: Decoder): GitHostKind = GitHostKind.fromWire(decoder.decodeString())
}

/** What `GitForgeCheck` answers: who the token signs in as, and the scopes it lacks. */
@Serializable
data class GitHostCheck(
    val login: String = "",
    val host: String = "",
    val scopesOK: Boolean = false,
    val missing: List<String> = emptyList(),
)

/**
 * The host as typed or pasted, reduced to what the list keys on and the Go core matches a
 * repository's host against: the host and its port, in lower case, without scheme, user, path
 * or query ("https://GitHub.com/" and a pasted repository URL both give "github.com"). Blank
 * when nothing is left or it holds a space. The same rule as iOS's normalizedGitHost.
 */
fun normalizeGitHost(input: String): String {
    val noScheme = input.trim().lowercase().replace(SCHEME, "")
    val end = noScheme.indexOfFirst { it == '/' || it == '?' || it == '#' }
    val raw = (if (end < 0) noScheme else noScheme.substring(0, end)).substringAfterLast('@').trimEnd('.', ':')
    val authority = withoutScpPath(raw)
    return if (authority.any { it.isWhitespace() }) "" else authority
}

/** "host:org" of a pasted scp address (git@host:org/repo) is the host; a numeric port is kept. */
private fun withoutScpPath(authority: String): String {
    val colon = authority.lastIndexOf(':')
    if (authority.startsWith("[") || colon < 0) return authority
    val name = authority.substring(0, colon).trimEnd('.')
    val port = authority.substring(colon + 1)
    return if (port.all { it.isDigit() }) "$name:$port" else name
}

/** The host name of a normalised host, without its port. */
fun gitHostName(host: String): String =
    if (host.startsWith("[")) host.substringBefore("]") + "]" else host.substringBeforeLast(':')

/**
 * The aliases typed in the Git host sheet: split on commas, spaces and new lines, each
 * normalised like a host, without blanks, [host] itself or duplicates, at most
 * [MAX_GIT_HOST_ALIASES].
 */
fun parseGitHostAliases(text: String, host: String = ""): List<String> {
    val own = normalizeGitHost(host)
    return text.split(ALIAS_SEPARATORS)
        .map(::normalizeGitHost)
        .filter { it.isNotEmpty() && it != own }
        .distinct()
        .take(MAX_GIT_HOST_ALIASES)
}

/** How many aliases a Git host keeps. */
const val MAX_GIT_HOST_ALIASES = 16

/**
 * The argument of the Go forge calls: `{"kind","baseURL","aliases","token"}` for [host], or
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
            // Other names a repository of this host may be reached by; requests still only go to baseURL.
            put("aliases", JsonArray(host.aliases.map { JsonPrimitive(it) }))
            put("token", token.trim())
        }.toString()
    }

private val SCHEME = Regex("^[a-zA-Z][a-zA-Z0-9+.-]*://")

private val ALIAS_SEPARATORS = Regex("[,;\\s]+")
