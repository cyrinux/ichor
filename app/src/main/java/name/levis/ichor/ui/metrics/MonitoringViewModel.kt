package name.levis.ichor.ui.metrics

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.PromOperatorStatus
import name.levis.ichor.model.PromRules
import name.levis.ichor.model.PromSource
import name.levis.ichor.model.PromTargets
import name.levis.ichor.ui.userMessage

/** One read of the Monitoring tab: its [value], or why it failed; the previous value stays on a failed reload. */
data class MonitoringPart<T>(val value: T? = null, val error: String? = null)

data class MonitoringState(
    val loading: Boolean = false,
    /** The source the parts were read from; null before the first load. */
    val source: PromSource? = null,
    val targets: MonitoringPart<PromTargets> = MonitoringPart(),
    val rules: MonitoringPart<PromRules> = MonitoringPart(),
    val operator: MonitoringPart<PromOperatorStatus> = MonitoringPart(),
)

/**
 * The Monitoring tab of the Metrics screen: down targets and rules from the source the
 * panels use, and the Prometheus Operator's objects from the Kubernetes API, read side by side.
 */
class MonitoringViewModel(private val kube: KubeRepository) : ViewModel() {
    private val _state = MutableStateFlow(MonitoringState())
    val state: StateFlow<MonitoringState> = _state.asStateFlow()
    private var job: Job? = null

    /** Reads everything again from [source]; a new source drops what the previous one said. */
    fun load(source: PromSource) {
        job?.cancel()
        val previous = _state.value.takeIf { it.source == source } ?: MonitoringState(source = source)
        _state.value = previous.copy(loading = true)
        job = viewModelScope.launch {
            val targets = async { read(previous.targets) { kube.promTargets(source) } }
            val rules = async { read(previous.rules) { kube.promRules(source) } }
            val operator = async { read(previous.operator) { kube.promOperatorStatus() } }
            _state.value = MonitoringState(
                loading = false,
                source = source,
                targets = targets.await(),
                rules = rules.await(),
                operator = operator.await(),
            )
        }
    }

    private suspend fun <T> read(previous: MonitoringPart<T>, fetch: suspend () -> T): MonitoringPart<T> = try {
        MonitoringPart(fetch())
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        previous.copy(error = e.userMessage())
    }
}
