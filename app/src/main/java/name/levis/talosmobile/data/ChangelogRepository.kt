package name.levis.talosmobile.data

import android.content.Context
import android.content.SharedPreferences
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.withContext
import name.levis.talosmobile.BuildConfig
import name.levis.talosmobile.model.Changelog
import name.levis.talosmobile.model.ChangelogRelease
import name.levis.talosmobile.model.decodeChangelog
import name.levis.talosmobile.model.releasesSince
import name.levis.talosmobile.model.whatsNew
import java.net.HttpURLConnection
import java.net.URL

/**
 * Release notes: the history bundled in the APK (assets/changelog.json, written by build.sh),
 * the one published with the latest GitHub release, and the last build that was launched
 * (to show "What's new" once after an update).
 */
class ChangelogRepository(private val context: Context, private val prefs: SharedPreferences) {

    /** The bundled history; empty when the asset is missing or corrupt. */
    suspend fun bundled(): Changelog = withContext(Dispatchers.IO) {
        decodeChangelog(runCatching { context.assets.open(ASSET).use { it.readBytes().decodeToString() } }.getOrNull())
    }

    /**
     * Releases to show in "What's new" for this launch, or null. When there is nothing to
     * show the current build is recorded at once; otherwise [markSeen] does it.
     */
    suspend fun pendingWhatsNew(): List<ChangelogRelease>? {
        val previous = if (prefs.contains(KEY_LAST_BUILD)) prefs.getInt(KEY_LAST_BUILD, 0) else null
        if (previous == BuildConfig.VERSION_CODE) return null
        val releases = whatsNew(previous, BuildConfig.VERSION_CODE, bundled())
        if (releases == null) markSeen()
        return releases
    }

    /** Records this build as launched: "What's new" is not shown for it again. */
    fun markSeen() {
        prefs.edit().putInt(KEY_LAST_BUILD, BuildConfig.VERSION_CODE).apply()
    }

    /**
     * Notes of the published releases newer than this build, for the update offer. Empty on
     * any failure (offline, no such asset, corrupt, slower than 10 s): the offer then shows without notes.
     */
    suspend fun publishedSinceThisBuild(): List<ChangelogRelease> {
        // The connection timeouts restart on each redirect and read: bound the whole fetch.
        // Blocking I/O cannot be cancelled, so it runs on its own scope and is abandoned.
        val fetch = background.async { runCatching { decodeChangelog(fetch()) }.getOrNull() }
        val published = withTimeoutOrNull(FETCH_TIMEOUT_MS.toLong()) { fetch.await() }
        if (published == null) fetch.cancel()
        return published?.releasesSince(BuildConfig.VERSION_CODE).orEmpty()
    }

    private val background = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private fun fetch(): String? {
        val connection = (URL(PUBLISHED_URL).openConnection() as HttpURLConnection).apply {
            connectTimeout = FETCH_TIMEOUT_MS
            readTimeout = FETCH_TIMEOUT_MS
            instanceFollowRedirects = true // release assets redirect to GitHub's object storage
            setRequestProperty("User-Agent", "talosdev-mobile/${BuildConfig.VERSION_NAME}")
        }
        try {
            if (connection.responseCode != HttpURLConnection.HTTP_OK) return null
            return connection.inputStream.use { input ->
                val bytes = input.readNBytesCompat(MAX_BYTES + 1)
                if (bytes.size > MAX_BYTES) null else bytes.decodeToString()
            }
        } finally {
            connection.disconnect()
        }
    }

    companion object {
        const val PREFS = "talosdev-mobile-changelog"
        private const val ASSET = "changelog.json"
        private const val KEY_LAST_BUILD = "last_launched_build"
        private const val FETCH_TIMEOUT_MS = 10_000
        private const val MAX_BYTES = 1024 * 1024
        private val PUBLISHED_URL = "$REPO_URL_BASE${BuildConfig.UPDATE_REPO}/releases/latest/download/changelog.json"
    }
}

/** Reads at most [limit] bytes (InputStream.readNBytes needs API 33). */
private fun java.io.InputStream.readNBytesCompat(limit: Int): ByteArray {
    val out = java.io.ByteArrayOutputStream()
    val buffer = ByteArray(16 * 1024)
    while (out.size() < limit) {
        val n = read(buffer, 0, minOf(buffer.size, limit - out.size()))
        if (n < 0) break
        out.write(buffer, 0, n)
    }
    return out.toByteArray()
}
