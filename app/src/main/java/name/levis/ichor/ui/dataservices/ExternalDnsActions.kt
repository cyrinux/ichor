package name.levis.ichor.ui.dataservices

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.model.DnsCheckEntry
import name.levis.ichor.model.DnsCheckResult
import name.levis.ichor.model.ExternalDnsRecord
import name.levis.ichor.model.ExternalDnsStatus
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText

/** The DNS check's state: running, the verdicts, or why it could not run. */
data class DnsCheckState(
    val running: Boolean = false,
    val result: DnsCheckResult? = null,
    val error: UiText? = null,
    /** The records the check was sent, in order: the results come back in the same order. */
    val records: List<ExternalDnsRecord> = emptyList(),
) {
    /**
     * The verdicts by [ExternalDnsRecord.rowKey], paired by position, so the A and AAAA rows
     * of a dual-stack name each get their own verdict.
     */
    val byRecord: Map<String, DnsCheckEntry> =
        records.zip(result?.results.orEmpty()).associate { (record, entry) -> record.rowKey to entry }
}

/**
 * The DNS check of the ExternalDNS records, run in [scope] (a ViewModel's). The verdicts stay
 * on screen until the next check; nothing runs without [check].
 */
class ExternalDnsActions(private val scope: CoroutineScope, private val dataServices: DataServicesRepository) {
    private val _state = MutableStateFlow<DnsCheckState?>(null)
    /** The last check, null before the first one. */
    val state: StateFlow<DnsCheckState?> = _state.asStateFlow()
    private var job: Job? = null

    /** Resolves [status]'s records from this phone; a check already running is left alone. */
    fun check(status: ExternalDnsStatus) {
        if (_state.value?.running == true) return
        job?.cancel()
        _state.value = DnsCheckState(running = true)
        job = scope.launch {
            try {
                val result = dataServices.dnsCheck(status.records, status.controllers)
                _state.value = DnsCheckState(result = result, records = status.records)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _state.value = DnsCheckState(error = e.uiText())
            }
        }
    }
}
