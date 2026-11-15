package name.levis.ichor.ui.components

import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import name.levis.ichor.R
import name.levis.ichor.data.AiPreferences
import name.levis.ichor.data.AnswerEvent
import name.levis.ichor.data.DiagnosisRepository
import name.levis.ichor.data.MaskScoped
import name.levis.ichor.ui.userMessage
import name.levis.ichorgo.Diagnosis

/** What "Explain with AI" is about, as Go's CollectExplanation names it, and what its card says it sends. */
enum class ExplainKind(val id: String, @StringRes val hint: Int) {
    POD("pod", R.string.ai_explain_pod_hint),
    ALERT("alert", R.string.ai_explain_alert_hint),
    FINDING("finding", R.string.ai_explain_finding_hint),
}

/** The answer of an Explain with AI card. */
data class ExplainState(
    /** Reading what the report needs. */
    val collecting: Boolean = false,
    val asking: Boolean = false,
    val text: String = "",
    /** Why it stopped early; [text] may still hold the part received. */
    val error: String? = null,
) {
    val running: Boolean get() = collecting || asking
}

/** The report an Explain with AI card shows before anything is sent. */
data class ExplainReport(
    val loading: Boolean = true,
    val text: String = "",
    val anonymized: Boolean = false,
    val error: String? = null,
)

/**
 * One subject's Explain with AI: reads its report as soon as the card shows (nothing is sent),
 * then sends it on a tap and streams the answer. A screenshot mode switch drops both.
 */
class ExplainViewModel(
    private val kind: ExplainKind,
    private val subject: JsonObject,
    private val diagnosis: DiagnosisRepository,
    private val preferences: AiPreferences,
) : ViewModel() {
    private val _report = MutableStateFlow(ExplainReport())
    val report: StateFlow<ExplainReport> = _report.asStateFlow()

    private val _explain = MutableStateFlow(ExplainState())
    val explain: StateFlow<ExplainState> = _explain.asStateFlow()

    private var collected: Diagnosis? = null
    private var collectedAnonymized: Boolean? = null
    private var collectJob: Job? = null
    private var askJob: Job? = null

    init {
        MaskScoped().watch(viewModelScope) { switching ->
            askJob?.cancel()
            collectJob?.cancel()
            collected = null
            collectedAnonymized = null
            _explain.value = ExplainState()
            _report.value = ExplainReport()
            // Read again once the switch is over, under the new setting.
            if (!switching) load()
        }
    }

    /** Reads the report, again when the anonymize setting changed since. */
    fun load() {
        val anonymize = preferences.settings.value.anonymize
        if (collectedAnonymized == anonymize && collected != null || collectJob?.isActive == true) return
        _report.value = ExplainReport()
        collectJob = viewModelScope.launch {
            runCatching { diagnosis.collectExplanation(kind.id, subject, anonymize) }
                .onSuccess {
                    collected = it
                    collectedAnonymized = anonymize
                    _report.value = ExplainReport(loading = false, text = it.report(), anonymized = it.anonymized())
                }
                .onFailure { e -> _report.value = ExplainReport(loading = false, error = e.userMessage()) }
        }
    }

    /** Sends the report shown, with [note], to the configured model. */
    fun explain(language: String, note: String) {
        val report = collected ?: return
        val settings = preferences.settings.value
        askJob?.cancel()
        _explain.value = ExplainState(asking = true)
        askJob = viewModelScope.launch {
            diagnosis.ask(report, settings, preferences.apiKey(settings.provider), language, note)
                .catch { e -> _explain.update { it.copy(asking = false, error = e.userMessage()) } }
                .collect { event ->
                    _explain.update { s ->
                        when (event) {
                            is AnswerEvent.Text -> s.copy(text = event.text)
                            // This report takes no tools: Go never asks for any here.
                            is AnswerEvent.Tool, is AnswerEvent.ToolDone -> s
                            is AnswerEvent.Done -> s.copy(asking = false, error = event.error)
                        }
                    }
                }
        }
    }

    /** Stops waiting for the answer, keeping what was received. */
    fun stop() {
        askJob?.cancel()
        _explain.update { it.copy(collecting = false, asking = false) }
    }
}
