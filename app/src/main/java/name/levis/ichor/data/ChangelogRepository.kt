package name.levis.ichor.data

import android.content.Context
import android.content.SharedPreferences
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.withContext
import name.levis.ichor.BuildConfig
import name.levis.ichor.model.Changelog
import name.levis.ichor.model.ChangelogRelease
import name.levis.ichor.model.decodeChangelog
import name.levis.ichor.model.releasesSince
import name.levis.ichor.model.whatsNew
import name.levis.ichor.util.httpGetBounded

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

    // Follows redirects: release assets redirect to GitHub's object storage.
    private fun fetch(): String? =
        httpGetBounded(PUBLISHED_URL, MAX_BYTES, FETCH_TIMEOUT_MS, "ichor/${BuildConfig.VERSION_NAME}")?.decodeToString()

    companion object {
        const val PREFS = "ichor-changelog"
        private const val ASSET = "changelog.json"
        private const val KEY_LAST_BUILD = "last_launched_build"
        private const val FETCH_TIMEOUT_MS = 10_000
        private const val MAX_BYTES = 1024 * 1024
        private val PUBLISHED_URL = "$REPO_URL_BASE${BuildConfig.UPDATE_REPO}/releases/latest/download/changelog.json"
    }
}
