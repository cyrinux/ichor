package name.levis.ichor.monitor

import android.content.SharedPreferences
import name.levis.ichor.data.SealedValue
import name.levis.ichor.data.TalosJson
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Background monitoring preferences plus the last snapshot (shared with the widget). The
 * snapshot names the cluster's nodes and addresses: it is kept encrypted, in [snapshotFile].
 */
class MonitorStore(private val prefs: SharedPreferences, private val snapshotFile: SealedValue) {
    init {
        migrateSnapshot()
    }

    private val _alertsEnabled = MutableStateFlow(prefs.getBoolean(KEY_ENABLED, false))
    val alertsEnabled: StateFlow<Boolean> = _alertsEnabled.asStateFlow()

    private val _intervalMinutes = MutableStateFlow(prefs.getLong(KEY_INTERVAL, INTERVALS.first()))
    val intervalMinutes: StateFlow<Long> = _intervalMinutes.asStateFlow()

    private val _dataServicesWatched = MutableStateFlow(prefs.getBoolean(KEY_DATA_SERVICES, false))

    /** Opt-in: also check Longhorn, Garage and CloudNativePG through the Kubernetes API. */
    val dataServicesWatched: StateFlow<Boolean> = _dataServicesWatched.asStateFlow()

    fun setDataServicesWatched(watched: Boolean) {
        prefs.edit().putBoolean(KEY_DATA_SERVICES, watched).apply()
        _dataServicesWatched.value = watched
    }

    private val _gitopsWatched = MutableStateFlow(prefs.getBoolean(KEY_GITOPS, false))

    /** Opt-in: also check Argo CD and Flux apps through the Kubernetes API. */
    val gitopsWatched: StateFlow<Boolean> = _gitopsWatched.asStateFlow()

    fun setGitopsWatched(watched: Boolean) {
        prefs.edit().putBoolean(KEY_GITOPS, watched).apply()
        _gitopsWatched.value = watched
    }

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

    /** Best effort: with the Keystore unavailable, the widget and alerts use it until the app stops. */
    fun saveSnapshot(snapshot: ClusterSnapshot) {
        runCatching { snapshotFile.write(TalosJson.encodeToString(ClusterSnapshot.serializer(), snapshot)) }
        _snapshotState.value = snapshot
    }

    fun clearSnapshot() {
        snapshotFile.delete()
        _snapshotState.value = null
    }

    // The plaintext one is only left when it could not be moved (Keystore unavailable).
    private fun readSnapshot(): ClusterSnapshot? = (snapshotFile.read() ?: prefs.getString(KEY_SNAPSHOT, null))?.let {
        runCatching { TalosJson.decodeFromString(ClusterSnapshot.serializer(), it) }.getOrNull()
    }

    /** Versions before encryption kept the snapshot in plaintext preferences: moved once, then removed. */
    private fun migrateSnapshot() {
        val legacy = prefs.getString(KEY_SNAPSHOT, null) ?: return
        if (snapshotFile.read() == null && runCatching { snapshotFile.write(legacy) }.isFailure) return
        prefs.edit().remove(KEY_SNAPSHOT).commit()
    }

    companion object {
        /** WorkManager's minimum periodic interval is 15 minutes. */
        val INTERVALS = listOf(15L, 30L, 60L)
        private const val KEY_ENABLED = "alerts_enabled"
        private const val KEY_INTERVAL = "interval_minutes"
        private const val KEY_SNAPSHOT = "snapshot"
        private const val KEY_DATA_SERVICES = "data_services_watched"
        private const val KEY_GITOPS = "gitops_watched"
    }
}
