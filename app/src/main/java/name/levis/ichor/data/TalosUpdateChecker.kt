package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import name.levis.talosmobile.Talosmobile
import name.levis.ichor.model.TalosUpdateCheck
import name.levis.ichor.model.talosUpdateFresh

/**
 * Whether a newer Talos release exists than what the nodes run. Asks GitHub (through the Go
 * core) at most every 6 hours per set of versions; the result is kept in memory only and a
 * failure is silent (the banner just does not show).
 */
class TalosUpdateChecker {
    /** Last result and the versions it was computed for (another cluster has other versions). */
    private val _result = MutableStateFlow<Pair<String, TalosUpdateCheck>?>(null)
    val result: StateFlow<Pair<String, TalosUpdateCheck>?> = _result.asStateFlow()
    private var lastAt = 0L
    private var lastKey: String? = null

    suspend fun check(versionsCsv: String) {
        if (versionsCsv.isEmpty()) return
        val now = System.currentTimeMillis()
        synchronized(this) {
            if (talosUpdateFresh(lastAt, lastKey, versionsCsv, now)) return
            // Counts as a check even if it fails: no retry storm while offline.
            lastAt = now
            lastKey = versionsCsv
        }
        val check = runCatching {
            withContext(Dispatchers.IO) { TalosJson.decodeFromString(TalosUpdateCheck.serializer(), Talosmobile.talosUpdateCheck(versionsCsv)) }
        }.getOrNull() ?: return
        _result.value = versionsCsv to check
    }
}
