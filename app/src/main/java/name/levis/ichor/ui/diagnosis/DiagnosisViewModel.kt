package name.levis.ichor.ui.diagnosis

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.ui.uiStateOf
import name.levis.ichorgo.Diagnosis
import name.levis.ichor.data.AiPreferences
import name.levis.ichor.data.AiToolCall
import name.levis.ichor.data.finished
import name.levis.ichor.data.AnswerEvent
import name.levis.ichor.data.DiagnosisRepository
import name.levis.ichor.model.RunbookProposal
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.userMessage

/** The collected report: [text] is exactly what would be sent. */
class Report(val diagnosis: Diagnosis, val text: String, val anonymized: Boolean)

data class AnswerState(
    val asking: Boolean = false,
    val text: String = "",
    /** Why the answer stopped early; [text] may still hold the part received. */
    val error: String? = null,
    /** What the model asked for, oldest first. */
    val tools: List<AiToolCall> = emptyList(),
    /** The server takes no tools: the answer comes from the report alone. */
    val toolsUnsupported: Boolean = false,
)

/** The runbook the model wrote from the answer: asked for, then the [proposal] or why there is none. */
data class ProposalState(
    val proposing: Boolean = false,
    val proposal: RunbookProposal? = null,
    val error: String? = null,
    /** The proposal was kept with the cluster's runbooks. */
    val kept: Boolean = false,
)

class DiagnosisViewModel(
    private val repository: DiagnosisRepository,
    private val preferences: AiPreferences,
) : ViewModel() {
    private val _report = MutableStateFlow<UiState<Report>>(UiState.Loading)
    val report: StateFlow<UiState<Report>> = _report.asStateFlow()

    private val _answer = MutableStateFlow(AnswerState())
    val answer: StateFlow<AnswerState> = _answer.asStateFlow()

    private val _proposal = MutableStateFlow(ProposalState())
    val proposal: StateFlow<ProposalState> = _proposal.asStateFlow()

    private var collectJob: Job? = null
    private var askJob: Job? = null
    private var proposeJob: Job? = null

    /** What the current report was (or is being) collected for, see [ensureCollected]. */
    private var collectedFor: String? = null

    /**
     * Collects the report when there is none yet, or when [source] changed since: it names
     * everything the report depends on (context, screenshot mode, anonymize setting), so a
     * report is never shown or sent for another cluster or with the wrong names. Showing the
     * screen again unchanged (rotation, back from Settings) keeps report and answer.
     */
    fun ensureCollected(source: String) {
        if (collectedFor == source) return
        collectedFor = source
        collect()
    }

    /** Reads the cluster again; a previous answer was about the previous report, so it goes. */
    fun collect() {
        val anonymize = preferences.settings.value.anonymize
        collectJob?.cancel()
        stop()
        _answer.value = AnswerState()
        clearProposal()
        _report.value = UiState.Loading
        collectJob = viewModelScope.launch {
            _report.value = uiStateOf {
                val diagnosis = repository.collect(anonymize)
                Report(diagnosis, diagnosis.report(), diagnosis.anonymized())
            }
        }
    }

    fun ask(note: String, language: String) {
        val report = (_report.value as? UiState.Loaded)?.data ?: return
        val settings = preferences.settings.value
        askJob?.cancel()
        _answer.value = AnswerState(asking = true)
        clearProposal()
        askJob = viewModelScope.launch {
            repository.ask(report.diagnosis, settings, preferences.apiKey(settings.provider), language, note)
                .catch { e -> _answer.update { it.copy(asking = false, error = e.userMessage()) } }
                .collect { event ->
                    _answer.update { s ->
                        when (event) {
                            is AnswerEvent.Text -> s.copy(text = event.text)
                            is AnswerEvent.Tool -> s.copy(tools = s.tools + AiToolCall(event.tool, event.args))
                            is AnswerEvent.ToolDone ->
                                if (event.tool.isEmpty()) s.copy(toolsUnsupported = true) else s.copy(tools = s.tools.finished(event))
                            is AnswerEvent.Done -> s.copy(asking = false, error = event.error)
                        }
                    }
                }
        }
    }

    /** Asks the model to turn the finished answer into a runbook, planned on the report's cluster; nothing runs. */
    fun proposeRunbook(language: String) {
        val report = (_report.value as? UiState.Loaded)?.data ?: return
        val answer = _answer.value.text.takeIf { it.isNotBlank() && !_answer.value.asking } ?: return
        val settings = preferences.settings.value
        proposeJob?.cancel()
        _proposal.value = ProposalState(proposing = true)
        proposeJob = viewModelScope.launch {
            _proposal.value = try {
                ProposalState(proposal = repository.proposeRunbook(report.diagnosis, settings, preferences.apiKey(settings.provider), language, answer))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                ProposalState(error = e.userMessage())
            }
        }
    }

    fun markKept() = _proposal.update { it.copy(kept = true) }

    /** Drops the proposal: nothing of it was stored unless it was kept. */
    fun clearProposal() {
        proposeJob?.cancel()
        _proposal.value = ProposalState()
    }

    /** Stops waiting for the answer, keeping what was received. */
    fun stop() {
        askJob?.cancel()
        _answer.update { it.copy(asking = false) }
    }

    /** The whole question as one text, for another app; null when there is no usable report. */
    fun prompt(note: String, language: String): String? =
        (_report.value as? UiState.Loaded)?.data?.diagnosis?.prompt(language, note)?.takeIf { it.isNotEmpty() }
}
