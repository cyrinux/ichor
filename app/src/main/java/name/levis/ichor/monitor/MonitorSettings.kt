package name.levis.ichor.monitor

import android.content.SharedPreferences
import name.levis.ichor.data.SealedValue
import name.levis.ichor.data.TalosJson
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.Serializable

/**
 * Background monitoring preferences plus the last snapshot of each watched cluster (shared with
 * the widget). They name the clusters' nodes and addresses: kept encrypted, in [snapshotFile].
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

    private val _checkupWatched = MutableStateFlow(prefs.getBoolean(KEY_CHECKUP, false))

    /** Opt-in: also run the cluster checkup through the Kubernetes API. */
    val checkupWatched: StateFlow<Boolean> = _checkupWatched.asStateFlow()

    fun setCheckupWatched(watched: Boolean) {
        prefs.edit().putBoolean(KEY_CHECKUP, watched).apply()
        _checkupWatched.value = watched
    }

    private val _alertmanagerWatched = MutableStateFlow(prefs.getBoolean(KEY_ALERTMANAGER, false))

    /** Opt-in: also read the cluster's Alertmanager alerts through the Kubernetes API (or its URL). */
    val alertmanagerWatched: StateFlow<Boolean> = _alertmanagerWatched.asStateFlow()

    fun setAlertmanagerWatched(watched: Boolean) {
        prefs.edit().putBoolean(KEY_ALERTMANAGER, watched).apply()
        _alertmanagerWatched.value = watched
    }

    private val _storageWatched = MutableStateFlow(prefs.getBoolean(KEY_STORAGE, false))

    /** Opt-in: also read every node's volume fill and disk SMART through the Talos API. */
    val storageWatched: StateFlow<Boolean> = _storageWatched.asStateFlow()

    fun setStorageWatched(watched: Boolean) {
        prefs.edit().putBoolean(KEY_STORAGE, watched).apply()
        _storageWatched.value = watched
    }

    private val _storageWarnPercent = MutableStateFlow(prefs.getInt(KEY_STORAGE_WARN, STORAGE_WARN_DEFAULT).coerceIn(STORAGE_WARN_RANGE))

    /** A volume used at this % or more is a warning. */
    val storageWarnPercent: StateFlow<Int> = _storageWarnPercent.asStateFlow()

    private val _storageCriticalPercent = MutableStateFlow(
        prefs.getInt(KEY_STORAGE_CRITICAL, STORAGE_CRITICAL_DEFAULT).coerceIn(storageCriticalRange(_storageWarnPercent.value)),
    )

    /** A volume used at this % or more is critical; always above [storageWarnPercent]. */
    val storageCriticalPercent: StateFlow<Int> = _storageCriticalPercent.asStateFlow()

    /** Moves the critical threshold up with it when it would no longer be above. */
    fun setStorageWarnPercent(percent: Int) {
        val warn = percent.coerceIn(STORAGE_WARN_RANGE)
        val critical = _storageCriticalPercent.value.coerceIn(storageCriticalRange(warn))
        prefs.edit().putInt(KEY_STORAGE_WARN, warn).putInt(KEY_STORAGE_CRITICAL, critical).apply()
        _storageWarnPercent.value = warn
        _storageCriticalPercent.value = critical
    }

    fun setStorageCriticalPercent(percent: Int) {
        val critical = percent.coerceIn(storageCriticalRange(_storageWarnPercent.value))
        prefs.edit().putInt(KEY_STORAGE_CRITICAL, critical).apply()
        _storageCriticalPercent.value = critical
    }

    fun setAlertsEnabled(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_ENABLED, enabled).apply()
        _alertsEnabled.value = enabled
    }

    fun setIntervalMinutes(minutes: Long) {
        prefs.edit().putLong(KEY_INTERVAL, minutes).apply()
        _intervalMinutes.value = minutes
    }

    private val _unreachableAlerts = MutableStateFlow(prefs.getBoolean(KEY_UNREACHABLE, false))

    /** Opt-in: alert once a cluster could not be read for [unreachableRuns] checks in a row, and once it answers again. */
    val unreachableAlerts: StateFlow<Boolean> = _unreachableAlerts.asStateFlow()

    fun setUnreachableAlerts(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_UNREACHABLE, enabled).apply()
        _unreachableAlerts.value = enabled
    }

    private val _unreachableRuns = MutableStateFlow(prefs.getInt(KEY_UNREACHABLE_RUNS, UNREACHABLE_RUNS_DEFAULT).coerceIn(UNREACHABLE_RUNS))
    val unreachableRuns: StateFlow<Int> = _unreachableRuns.asStateFlow()

    fun setUnreachableRuns(runs: Int) {
        val kept = runs.coerceIn(UNREACHABLE_RUNS)
        prefs.edit().putInt(KEY_UNREACHABLE_RUNS, kept).apply()
        _unreachableRuns.value = kept
    }

    private val _state = MutableStateFlow(readState())

    /** What the last checks saw of each cluster, observable: the widget redraws from it while its session is alive. */
    val state: StateFlow<MonitorState> = _state.asStateFlow()

    /** The last snapshot of the cluster [fingerprint]. */
    fun snapshot(fingerprint: String): ClusterSnapshot? = _state.value.clusters[fingerprint]

    /** Best effort: with the Keystore unavailable, the widget and alerts use it until the app stops. */
    fun saveState(state: MonitorState) {
        runCatching { snapshotFile.write(TalosJson.encodeToString(MonitorState.serializer(), state)) }
        _state.value = state
    }

    /** Forgets every cluster's snapshot (and how often it could not be read). */
    fun clearSnapshots() {
        snapshotFile.delete()
        _state.value = MonitorState.EMPTY
    }

    // The plaintext one is only left when it could not be moved (Keystore unavailable).
    private fun readState(): MonitorState = (snapshotFile.read() ?: prefs.getString(KEY_SNAPSHOT, null))?.let(::decodeMonitorState) ?: MonitorState.EMPTY

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
        private const val KEY_CHECKUP = "checkup_watched"
        private const val KEY_ALERTMANAGER = "alertmanager_watched"
        private const val KEY_UNREACHABLE = "unreachable_alerts"
        private const val KEY_STORAGE = "storage_watched"
        private const val KEY_STORAGE_WARN = "storage_warn_percent"
        private const val KEY_STORAGE_CRITICAL = "storage_critical_percent"
        private const val KEY_UNREACHABLE_RUNS = "unreachable_runs"
    }
}

/**
 * What the background checks keep: each watched cluster's last snapshot and [reach], by context
 * fingerprint, and [active], the fingerprint of the cluster on screen at the last check (what a
 * widget without a cluster of its own shows while the config is not loaded). [clusters] has no
 * default, so a single snapshot (an older version's file) never decodes as an empty state.
 */
@Serializable
data class MonitorState(
    val clusters: Map<String, ClusterSnapshot>,
    val reach: Map<String, Reach> = emptyMap(),
    val active: String = "",
) {
    companion object {
        val EMPTY = MonitorState(emptyMap())
    }
}

/** The stored file, of this version or of one that kept the active cluster's snapshot only (moved to its fingerprint). */
internal fun decodeMonitorState(json: String): MonitorState? {
    runCatching { TalosJson.decodeFromString(MonitorState.serializer(), json) }.getOrNull()?.let { return it }
    val legacy = runCatching { TalosJson.decodeFromString(ClusterSnapshot.serializer(), json) }.getOrNull() ?: return null
    // Without its fingerprint it cannot be told whose it was: the next check is a baseline.
    if (legacy.fingerprint.isBlank()) return MonitorState.EMPTY
    return MonitorState(clusters = mapOf(legacy.fingerprint to legacy), active = legacy.fingerprint)
}
