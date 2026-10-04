package name.levis.ichor.ui.flows

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.CiliumRepository
import name.levis.ichor.data.StreamItem
import name.levis.ichor.model.CiliumStatus
import name.levis.ichor.model.HubbleFilter
import name.levis.ichor.model.HubbleSnapshot
import name.levis.ichor.model.NetPolicyReport
import name.levis.ichor.model.namespaces
import name.levis.ichor.model.policyNamespaces
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.userMessage

/** The stream as last seen: [snapshot] null until the first one arrives. */
data class FlowsState(
    val snapshot: HubbleSnapshot? = null,
    val streaming: Boolean = false,
    val error: String? = null,
)

/**
 * Cilium's status (loaded first: no stream without Hubble), then its live flows under [filter],
 * and the policies, read once a drop's policy is opened or the namespaces are picked from.
 */
class FlowsViewModel(private val cilium: CiliumRepository, initial: HubbleFilter) : LoadingViewModel<CiliumStatus>() {
    override suspend fun fetch() = cilium.status()

    private val _filter = MutableStateFlow(initial)
    val filter: StateFlow<HubbleFilter> = _filter.asStateFlow()

    private val _flows = MutableStateFlow(FlowsState())
    val flows: StateFlow<FlowsState> = _flows.asStateFlow()

    private val _policies = MutableStateFlow<UiState<NetPolicyReport>?>(null)
    val policies: StateFlow<UiState<NetPolicyReport>?> = _policies.asStateFlow()
    private var policiesJob: Job? = null

    /** Every namespace seen so far, in flows or with policies, to filter on. */
    private val _namespaces = MutableStateFlow(listOfNotNull(initial.namespace).toSet())
    val namespaces: StateFlow<Set<String>> = _namespaces.asStateFlow()

    /** A new filter: the screen restarts the stream with it. */
    fun setFilter(filter: HubbleFilter) {
        _filter.value = filter
    }

    /** Follows the flows under the current filter until cancelled or the stream ends. */
    suspend fun stream(filter: HubbleFilter) {
        _flows.value = FlowsState(streaming = true)
        try {
            cilium.flows(filter).collect { item ->
                when (item) {
                    is StreamItem.Item -> {
                        _flows.update { it.copy(snapshot = item.value) }
                        _namespaces.update { it + item.value.namespaces() }
                    }
                    is StreamItem.Done -> _flows.update { it.copy(streaming = false, error = item.error) }
                }
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            _flows.update { it.copy(error = e.userMessage()) }
        } finally {
            _flows.update { it.copy(streaming = false) }
        }
    }

    /** Reads the policies once (again after a failure). */
    fun loadPolicies() {
        if (_policies.value is UiState.Loaded || policiesJob?.isActive == true) return
        _policies.value = UiState.Loading
        policiesJob = viewModelScope.launch {
            val state = uiStateOf { cilium.policies() }
            _policies.value = state
            (state as? UiState.Loaded)?.let { loaded -> _namespaces.update { it + loaded.data.policyNamespaces } }
        }
    }
}

/** Whether Cilium runs, to offer its live flows where it does. */
class CiliumViewModel(private val cilium: CiliumRepository) : LoadingViewModel<CiliumStatus>() {
    override suspend fun fetch() = cilium.status()
}
