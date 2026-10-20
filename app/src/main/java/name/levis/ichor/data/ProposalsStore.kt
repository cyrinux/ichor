package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.GitProposalRecord
import name.levis.ichor.model.decodeProposals
import name.levis.ichor.model.encodeProposals
import name.levis.ichor.model.withProposal

/**
 * The pull requests opened from the phone, per cluster (context fingerprint), newest first and
 * 50 at most, as JSON in its own [prefs] file. No token. Only on this device.
 */
class ProposalsStore(prefs: SharedPreferences) {
    private val map = FingerprintPrefsMap(prefs, { (it as? String)?.let(::decodeProposals) }, { fingerprint, list -> putString(fingerprint, encodeProposals(list)) })
    val proposals: StateFlow<Map<String, List<GitProposalRecord>>> get() = map.values

    /** Keeps [record] for the cluster [fingerprint], first. */
    fun add(fingerprint: String, record: GitProposalRecord) =
        map.set(fingerprint, withProposal(map.values.value[fingerprint].orEmpty(), record))

    /** Forgets [record] of the cluster [fingerprint]. */
    fun remove(fingerprint: String, record: GitProposalRecord) {
        val rest = map.values.value[fingerprint].orEmpty().filter { it.key != record.key }
        map.set(fingerprint, rest.ifEmpty { null })
    }

    /** Forgets the proposals of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) {
        val known = summary.contexts.map { it.fingerprint }.filter { it.isNotBlank() }.toSet()
        map.store(map.values.value.filterKeys { it in known })
    }

    companion object {
        const val FILE = "ichor-proposals"
    }
}
