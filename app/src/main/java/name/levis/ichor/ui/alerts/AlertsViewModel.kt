package name.levis.ichor.ui.alerts

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.data.AlertmanagerRepository
import name.levis.ichor.data.AlertmanagerStore
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.AlertmanagerConfig
import name.levis.ichor.model.AmAlerts
import name.levis.ichor.model.AmFilter
import name.levis.ichor.model.AmMatcher
import name.levis.ichor.model.AmSilence
import name.levis.ichor.model.AmSilences
import name.levis.ichor.model.PromSource
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.refreshFailed
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.userMessage

data class AlertsState(
    /** The source was read from the store and, without one, searched for once. */
    val ready: Boolean = false,
    /** Where the alerts come from; null when none was chosen nor found. */
    val source: PromSource? = null,
    /** The user chose [source]; else it is the likeliest one found. */
    val chosen: Boolean = false,
    /** Alertmanagers found in the cluster; null before a search. */
    val discovered: List<PromSource>? = null,
    val discovering: Boolean = false,
    val discoveryError: String? = null,
    val alerts: UiState<AmAlerts> = UiState.Loading,
    val silences: UiState<AmSilences> = UiState.Loading,
    val withExpired: Boolean = false,
    val filter: AmFilter = AmFilter(),
    val error: String? = null,
)

/** The outcome of a silence created ([silenceId]) or expired, or why it failed. */
data class AlertActionResult(val expired: Boolean, val silenceId: String, val error: UiText?)

/**
 * The Alerts screen of the cluster [fingerprint]: its Alertmanager (the one chosen in [store],
 * else the likeliest found), its alerts (every state, filtered on the phone) and silences, and
 * silencing or expiring.
 */
class AlertsViewModel(
    private val alertmanager: AlertmanagerRepository,
    private val kube: KubeRepository,
    private val store: AlertmanagerStore,
    private val fingerprint: String,
) : ViewModel() {
    private val _state = MutableStateFlow(AlertsState())
    val state: StateFlow<AlertsState> = _state.asStateFlow()
    private var alertsJob: Job? = null
    private var silencesJob: Job? = null
    private val actions = KeyedActions<AlertActionResult>(viewModelScope)

    /** Silence IDs being expired, and "silence" while one is being created. */
    val busy: StateFlow<Set<String>> get() = actions.busy
    val results: Flow<AlertActionResult> get() = actions.results

    fun load() = viewModelScope.launch {
        if (_state.value.ready) return@launch
        try {
            val chosen = withContext(Dispatchers.IO) { store.read(fingerprint) }.source
            if (chosen != null) {
                _state.update { it.copy(ready = true, source = chosen, chosen = true) }
                refresh()
            } else {
                discover(useFirst = true).join()
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(ready = true, error = e.userMessage()) }
        }
    }

    /** Looks for Alertmanagers; [useFirst] reads the likeliest one when none was chosen. */
    fun discover(useFirst: Boolean = false) = viewModelScope.launch {
        _state.update { it.copy(discovering = true, discoveryError = null) }
        try {
            val sources = alertmanager.discover()
            _state.update { it.copy(discovered = sources, discovering = false) }
            val first = sources.firstOrNull()
            if (useFirst && !_state.value.chosen) {
                _state.update { it.copy(ready = true, source = first) }
                if (first != null) refresh()
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(ready = true, discovering = false, discoveryError = e.userMessage()) }
        }
    }

    /** Checks [source] (Go), saves it as the cluster's and reloads. Null when saved, else why not. */
    suspend fun setSource(source: PromSource): String? = try {
        val checked = kube.normalizePromSource(source.copy(kind = source.kind.ifEmpty { KIND }))
        withContext(Dispatchers.IO) { store.save(fingerprint, AlertmanagerConfig(checked)) }
        _state.update { it.copy(source = checked, chosen = true, alerts = UiState.Loading, silences = UiState.Loading) }
        refresh()
        null
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        e.userMessage()
    }

    /** Reads the alerts of [source]: null when it answers, else why not. */
    suspend fun test(source: PromSource): String? = try {
        alertmanager.alerts(kube.normalizePromSource(source))
        null
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        e.userMessage()
    }

    fun setFilter(filter: AmFilter) = _state.update { it.copy(filter = filter) }

    fun setWithExpired(withExpired: Boolean) {
        _state.update { it.copy(withExpired = withExpired) }
        loadSilences()
    }

    fun refresh() {
        loadAlerts()
        loadSilences()
    }

    private fun loadAlerts() {
        val source = _state.value.source ?: return
        alertsJob?.cancel()
        _state.update { it.copy(alerts = it.alerts.refreshing()) }
        alertsJob = viewModelScope.launch {
            val outcome = cancellableCatching { alertmanager.alerts(source) }
            if (_state.value.source != source) return@launch
            _state.update { s -> s.copy(alerts = outcome.fold({ UiState.Loaded(it) }, { s.alerts.refreshFailed(it.uiText()) })) }
        }
    }

    private fun loadSilences() {
        val source = _state.value.source ?: return
        val withExpired = _state.value.withExpired
        silencesJob?.cancel()
        _state.update { it.copy(silences = it.silences.refreshing()) }
        silencesJob = viewModelScope.launch {
            val outcome = cancellableCatching { alertmanager.silences(source, withExpired) }
            if (_state.value.source != source) return@launch
            _state.update { s -> s.copy(silences = outcome.fold({ UiState.Loaded(it) }, { s.silences.refreshFailed(it.uiText()) })) }
        }
    }

    /** The matchers that silence exactly the alert of [labels], for the silence form. */
    suspend fun matchersFor(labels: Map<String, String>): List<AmMatcher> = alertmanager.silenceMatchers(labels)

    /** Silences [matchers] for [minutes] with [comment]; the alerts and silences reload once done. */
    fun silence(matchers: List<AmMatcher>, minutes: Long, comment: String) {
        val source = _state.value.source ?: return
        actions.launch(SILENCE, { alertmanager.silence(source, matchers, minutes, comment) }, {
            AlertActionResult(expired = false, silenceId = it.getOrNull().orEmpty(), error = it.exceptionOrNull()?.uiText())
        }, ::refresh)
    }

    /** Ends [silence] now; the alerts and silences reload once done. */
    fun expire(silence: AmSilence) {
        val source = _state.value.source ?: return
        actions.launch(silence.id, { alertmanager.expire(source, silence.id) }, {
            AlertActionResult(expired = true, silenceId = silence.id, error = it.exceptionOrNull()?.uiText())
        }, ::refresh)
    }

    companion object {
        /** The busy key of a silence being created. */
        const val SILENCE = "silence"

        /** The kind a source typed in is given: Go shows it as a hint. */
        private const val KIND = "alertmanager"
    }
}

/** Loading again: the data on screen stays, marked as refreshing. */
private fun <T> UiState<T>.refreshing(): UiState<T> = if (this is UiState.Loaded) copy(refreshing = true) else UiState.Loading
