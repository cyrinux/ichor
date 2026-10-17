package name.levis.ichor.model

import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
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
data class GitHost(
    val host: String,
    @Serializable(with = GitHostKindSerializer::class) val kind: GitHostKind = GitHostKind.AUTO,
)

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
    val authority = (if (end < 0) noScheme else noScheme.substring(0, end)).substringAfterLast('@').trimEnd('.', ':')
    return if (authority.any { it.isWhitespace() }) "" else authority
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
