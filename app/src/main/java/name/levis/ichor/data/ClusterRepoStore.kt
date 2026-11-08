package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ClusterRepo
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.decodeClusterRepo
import name.levis.ichor.model.encodeClusterRepo
import name.levis.ichor.model.normalizeClusterRepo

/**
 * The repository saved for each cluster, by context fingerprint (so a renamed cluster keeps
 * it), as JSON in its own [prefs] file. No secret: the token stays in [GitHostStore]. Only on
 * this device, and in backups.
 */
class ClusterRepoStore(prefs: SharedPreferences) {
    private val map = FingerprintPrefsMap(prefs, { (it as? String)?.let(::decodeClusterRepo) }, { fingerprint, repo -> putString(fingerprint, encodeClusterRepo(repo)) })
    val repos: StateFlow<Map<String, ClusterRepo>> get() = map.values

    /** Saves [repo] for the cluster [fingerprint]; null or one without an address forgets it. */
    fun set(fingerprint: String, repo: ClusterRepo?) = map.set(fingerprint, repo?.let(::normalizeClusterRepo))

    /** Forgets the repositories of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) {
        val known = summary.contexts.map { it.fingerprint }.filter { it.isNotBlank() }.toSet()
        map.store(map.values.value.filterKeys { it in known })
    }

    companion object {
        const val FILE = "ichor-cluster-repo"
    }
}
