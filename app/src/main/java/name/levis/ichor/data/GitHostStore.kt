package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.model.GitHost
import name.levis.ichor.model.GitHostKind
import name.levis.ichor.model.gitHostJson
import name.levis.ichor.model.normalizeGitHost
import name.levis.ichor.model.parseGitHostAliases
import java.security.MessageDigest

/**
 * The Git hosts the phone has a token for (Settings → Git hosts). The list and the login of
 * each host's last check are in preferences; each token is in its own Keystore-encrypted file
 * ([tokenStore], named from a hash of the host), read and written on [tokenWrites], never logged.
 */
class GitHostStore(
    private val prefs: SharedPreferences,
    private val tokenStore: (host: String) -> SealedValue,
    private val scope: CoroutineScope,
    private val demo: () -> Boolean,
    // One at a time, in the order asked: the last token saved is the one stored.
    private val tokenWrites: CoroutineDispatcher = Dispatchers.IO.limitedParallelism(1),
) {
    private val _hosts = MutableStateFlow(readHosts())
    val hosts: StateFlow<List<GitHost>> = _hosts.asStateFlow()

    private val _logins = MutableStateFlow(readLogins())

    /** The login of each host's last successful check; a host without one shows "Not checked". */
    val logins: StateFlow<Map<String, String>> = _logins.asStateFlow()

    private val tokens = HashMap<String, String>()

    /** The token of [host], "" when none is stored (or it can no longer be decrypted). */
    suspend fun token(host: String): String {
        val key = normalizeGitHost(host)
        synchronized(tokens) { tokens[key] }?.let { return it }
        val read = withContext(tokenWrites) { runCatching { tokenStore(key).read() }.getOrNull().orEmpty() }
        return synchronized(tokens) { tokens.getOrPut(key) { read } }
    }

    /**
     * Adds [host] or changes its kind and [aliases] (null keeps the saved ones); a non-blank
     * [token] replaces the stored one (blank keeps it). The list changes at once, the token is
     * encrypted off the caller's thread, still keyed by [host] alone.
     */
    fun set(host: String, kind: GitHostKind, token: String, aliases: List<String>? = null) {
        val key = normalizeGitHost(host)
        if (key.isEmpty()) return
        val current = _hosts.value
        val kept = aliases ?: current.firstOrNull { it.host == key }?.aliases.orEmpty()
        val entry = GitHost(key, kind, parseGitHostAliases(kept.joinToString(","), key))
        val next = if (current.any { it.host == key }) current.map { if (it.host == key) entry else it } else current + entry
        writeHosts(next)
        val trimmed = token.trim()
        if (trimmed.isEmpty()) return
        synchronized(tokens) { tokens[key] = trimmed }
        forgetLogin(key)
        scope.launch(tokenWrites) { runCatching { tokenStore(key).write(trimmed) } }
    }

    /** Drops [host] from the list and deletes its token file and Keystore key. */
    fun remove(host: String) {
        val key = normalizeGitHost(host)
        writeHosts(_hosts.value.filterNot { it.host == key })
        synchronized(tokens) { tokens.remove(key) }
        forgetLogin(key)
        scope.launch(tokenWrites) { runCatching { tokenStore(key).delete() } }
    }

    /**
     * The listed Git host a source on [host] belongs to: the one named [host], else the first
     * with [host] among its aliases; a bare, unlisted [GitHost] for [host] when none is.
     */
    fun hostFor(host: String): GitHost {
        val key = normalizeGitHost(host)
        val hosts = _hosts.value
        return hosts.firstOrNull { it.host == key } ?: hosts.firstOrNull { it.matches(key) } ?: GitHost(key)
    }

    /**
     * The Go argument for [host]: its stored token, or [token] when given (the add sheet checks
     * what is typed before saving it); `{"kind":"demo"}` in the demo context.
     */
    suspend fun hostJson(host: GitHost, token: String? = null): String {
        if (demo()) return gitHostJson(host, "", demo = true)
        return gitHostJson(host, token?.takeIf { it.isNotBlank() } ?: token(host.host), demo = false)
    }

    /**
     * Remembers who a listed [host] signed in as (a host still being added is remembered once
     * saved); never in the demo context, whose login is made up.
     */
    fun rememberLogin(host: String, login: String) {
        val key = normalizeGitHost(host)
        if (demo() || login.isBlank() || _hosts.value.none { it.host == key }) return
        writeLogins(_logins.value + (key to login))
    }

    private fun forgetLogin(host: String) {
        if (host in _logins.value) writeLogins(_logins.value - host)
    }

    private fun writeHosts(hosts: List<GitHost>) {
        prefs.edit().putString(KEY_HOSTS, TalosJson.encodeToString(HOSTS, hosts)).apply()
        _hosts.value = hosts
    }

    private fun writeLogins(logins: Map<String, String>) {
        prefs.edit().putString(KEY_LOGINS, TalosJson.encodeToString(LOGINS, logins)).apply()
        _logins.value = logins
    }

    private fun readHosts(): List<GitHost> =
        prefs.getString(KEY_HOSTS, null)?.let { runCatching { TalosJson.decodeFromString(HOSTS, it) }.getOrNull() }.orEmpty()

    private fun readLogins(): Map<String, String> =
        prefs.getString(KEY_LOGINS, null)?.let { runCatching { TalosJson.decodeFromString(LOGINS, it) }.getOrNull() }.orEmpty()

    companion object {
        const val FILE = "ichor-git-hosts"
        private const val KEY_HOSTS = "hosts"
        private const val KEY_LOGINS = "logins"
        private val HOSTS = ListSerializer(GitHost.serializer())
        private val LOGINS = MapSerializer(String.serializer(), String.serializer())

        /** The token file and Keystore alias of [host]: a hash, so no host name shows on disk. */
        fun tokenName(host: String): String {
            val digest = MessageDigest.getInstance("SHA-256").digest(normalizeGitHost(host).encodeToByteArray())
            return "git-host-" + digest.take(TOKEN_NAME_BYTES).joinToString("") { "%02x".format(it) }
        }

        private const val TOKEN_NAME_BYTES = 12
    }
}
