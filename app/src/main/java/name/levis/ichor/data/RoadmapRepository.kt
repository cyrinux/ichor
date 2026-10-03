package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.BuildConfig
import name.levis.ichor.R
import name.levis.ichor.model.Roadmap
import name.levis.ichor.model.decodeRoadmap
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText
import java.net.HttpURLConnection
import java.net.URL

/**
 * The features open for funding, from the website (docs/roadmap.json), so the list changes
 * without an app release. The last good copy is kept for offline use.
 */
class RoadmapRepository(
    private val prefs: SharedPreferences,
    private val download: () -> String? = ::fetch,
) {
    /** The published roadmap, else the last one downloaded; fails when there is neither. */
    suspend fun load(): Roadmap = withContext(Dispatchers.IO) {
        val fetched = runCatching { download() }
        fetched.getOrNull()?.let { json ->
            decodeRoadmap(json)?.let { roadmap ->
                prefs.edit().putString(KEY_CACHE, json).apply()
                return@withContext roadmap
            }
        }
        decodeRoadmap(prefs.getString(KEY_CACHE, null))
            ?: throw LocalizedException(
                UiText.Res(R.string.funding_load_failed, fetched.exceptionOrNull()?.uiText() ?: UiText.Res(R.string.funding_load_invalid)),
            )
    }

    companion object {
        const val FILE = "ichor-roadmap"
        private const val KEY_CACHE = "roadmap"
        private const val URL_ROADMAP = "https://cyrinux.github.io/ichor/roadmap.json"
        private const val TIMEOUT_MS = 10_000
        private const val MAX_BYTES = 256 * 1024

        private fun fetch(): String? {
            val connection = (URL(URL_ROADMAP).openConnection() as HttpURLConnection).apply {
                connectTimeout = TIMEOUT_MS
                readTimeout = TIMEOUT_MS
                setRequestProperty("User-Agent", "ichor/${BuildConfig.VERSION_NAME}")
            }
            try {
                if (connection.responseCode != HttpURLConnection.HTTP_OK) {
                    throw LocalizedException(UiText.Res(R.string.update_err_http, connection.responseCode))
                }
                return connection.inputStream.use { input ->
                    val bytes = input.readNBytesCompat(MAX_BYTES + 1)
                    if (bytes.size > MAX_BYTES) null else bytes.decodeToString()
                }
            } finally {
                connection.disconnect()
            }
        }
    }
}
