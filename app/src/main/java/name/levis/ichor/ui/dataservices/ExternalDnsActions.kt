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
import name.levis.ichor.model.ExternalDnsStatus
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText

/** The DNS check's state: running, the verdicts, or why it could not run. */
data class DnsCheckState(
    val running: Boolean = false,
    val result: DnsCheckResult? = null,
    val error: UiText? = null,
) {
    /** The verdicts by hostname, for the record rows. */
    val byName: Map<String, DnsCheckEntry> = result?.results.orEmpty().associateBy { it.dnsName }
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
                _state.value = DnsCheckState(result = result)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _state.value = DnsCheckState(error = e.uiText())
            }
        }
    }
}
