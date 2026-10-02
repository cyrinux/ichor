package name.levis.ichor.monitor

import android.content.SharedPreferences
import name.levis.ichor.data.TalosJson
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** Background monitoring preferences plus the last snapshot (shared with the widget). */
class MonitorStore(private val prefs: SharedPreferences) {
    private val _alertsEnabled = MutableStateFlow(prefs.getBoolean(KEY_ENABLED, false))
    val alertsEnabled: StateFlow<Boolean> = _alertsEnabled.asStateFlow()

    private val _intervalMinutes = MutableStateFlow(prefs.getLong(KEY_INTERVAL, INTERVALS.first()))
    val intervalMinutes: StateFlow<Long> = _intervalMinutes.asStateFlow()

    fun setAlertsEnabled(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_ENABLED, enabled).apply()
        _alertsEnabled.value = enabled
    }

    fun setIntervalMinutes(minutes: Long) {
        prefs.edit().putLong(KEY_INTERVAL, minutes).apply()
        _intervalMinutes.value = minutes
    }

    private val _snapshotState = MutableStateFlow(readSnapshot())

    /** The last snapshot, observable: the widget redraws from it while its session is alive. */
    val snapshotState: StateFlow<ClusterSnapshot?> = _snapshotState.asStateFlow()

    fun snapshot(): ClusterSnapshot? = _snapshotState.value

    fun saveSnapshot(snapshot: ClusterSnapshot) {
        prefs.edit().putString(KEY_SNAPSHOT, TalosJson.encodeToString(ClusterSnapshot.serializer(), snapshot)).apply()
        _snapshotState.value = snapshot
    }

    fun clearSnapshot() {
        prefs.edit().remove(KEY_SNAPSHOT).apply()
        _snapshotState.value = null
    }

    private fun readSnapshot(): ClusterSnapshot? = prefs.getString(KEY_SNAPSHOT, null)?.let {
        runCatching { TalosJson.decodeFromString(ClusterSnapshot.serializer(), it) }.getOrNull()
    }

    companion object {
        /** WorkManager's minimum periodic interval is 15 minutes. */
        val INTERVALS = listOf(15L, 30L, 60L)
        private const val KEY_ENABLED = "alerts_enabled"
        private const val KEY_INTERVAL = "interval_minutes"
        private const val KEY_SNAPSHOT = "snapshot"
    }
}
