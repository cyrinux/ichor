package name.levis.ichor.ui.importconfig

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.KubeAuthRepository
import name.levis.ichor.data.OmniAuthRepository
import name.levis.ichor.model.TalosForm
import name.levis.ichor.model.TalosFormMode
import name.levis.ichor.model.isOmni
import name.levis.ichor.model.storedContextName
import name.levis.ichor.model.DiscoveryProvider
import name.levis.ichor.model.discoveryFields
import name.levis.ichor.model.importedContextNames
import name.levis.ichor.model.isKube
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ImportChoice
import name.levis.ichor.model.ImportConflict
import name.levis.ichor.model.initialKubeChoices
import name.levis.ichor.model.takenNameChoices
import name.levis.ichor.ui.userMessage
import name.levis.ichorgo.Ichorgo
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

sealed interface ImportState {
    data object Idle : ImportState
    data object Validating : ImportState

    /**
     * A valid talosconfig, with its contexts named like stored ones ([conflicts]) and what the
     * user picked for each ([choices], one per conflict). [error] is a failed save, kept on
     * the preview so the choices can be fixed. [omniKeys] are the service account keys entered
     * for its Omni contexts, by position, stored once the contexts are.
     */
    data class Preview(
        val yaml: String,
        val summary: ConfigSummary,
        val conflicts: List<ImportConflict> = emptyList(),
        val choices: List<ImportChoice> = conflicts.map { ImportChoice(it.index) },
        val takenNames: Set<Int> = emptySet(),
        val error: String? = null,
        val omniKeys: Map<Int, String> = emptyMap(),
    ) : ImportState {
        val canImport: Boolean get() = takenNames.isEmpty()

        // Holds client keys: never in a log line.
        override fun toString() = "Preview(${summary.contexts.size} contexts)"
    }

    /**
     * A kubeconfig: one row per context, each checked when it can be added ([choices], one per
     * context, by position; an unchecked one is skipped), with its conflicts as for a talosconfig.
     */
    data class KubePreview(
        val yaml: String,
        val summary: ConfigSummary,
        val conflicts: List<ImportConflict> = emptyList(),
        val choices: List<ImportChoice> = initialKubeChoices(summary),
        val takenNames: Set<Int> = emptySet(),
        val error: String? = null,
        /** The cloud credentials that found these clusters (discovery): the added ones sign in with them. */
        val discovery: Map<String, String>? = null,
    ) : ImportState {
        val canImport: Boolean get() = takenNames.isEmpty() && choices.any { !it.skip }

        // Holds credentials: never in a log line.
        override fun toString() = "KubePreview(${summary.contexts.size} contexts)"
    }

    /**
     * Adding clusters from a cloud account (K7): the fields each provider asks for, starting
     * on the [initial] provider; [running] while the account's clusters are listed, [error]
     * when that failed.
     */
    data class Discover(
        val fields: Map<DiscoveryProvider, List<String>>,
        val initial: DiscoveryProvider? = null,
        val running: Boolean = false,
        val error: String? = null,
    ) : ImportState

    data class Invalid(val message: String) : ImportState

    /** Stored; [warning] says what did not follow (an Omni key refused), the clusters stay. */
    data class Saved(val warning: String? = null) : ImportState
}

class ImportViewModel(
    private val configs: ConfigRepository,
    private val auth: KubeAuthRepository,
    private val omni: OmniAuthRepository,
) : ViewModel() {
    private val _state = MutableStateFlow<ImportState>(ImportState.Idle)
    val state: StateFlow<ImportState> = _state.asStateFlow()

    /**
     * Validates text from any source (file, paste, QR, a file opened with the app) and shows a
     * preview. A compressed "ichor-config:" payload is expanded first; the app then tells a
     * kubeconfig from a talosconfig, the user does not pick.
     */
    fun submit(text: String) {
        _state.value = ImportState.Validating
        viewModelScope.launch {
            _state.value = runCatching { preview(text) }.getOrElse { ImportState.Invalid(it.userMessage()) }
        }
    }

    /**
     * Builds a talosconfig from the "Enter details" [form] and previews it like any other; an
     * Omni service account key entered there is kept for once the cluster is stored.
     */
    fun submitForm(form: TalosForm) {
        _state.value = ImportState.Validating
        viewModelScope.launch {
            _state.value = runCatching {
                val yaml = withContext(Dispatchers.IO) { Ichorgo.buildTalosconfig(form.toJson()) }
                val preview = preview(yaml)
                val key = form.serviceAccountKey.trim()
                if (preview is ImportState.Preview && form.mode == TalosFormMode.OMNI && key.isNotEmpty()) {
                    preview.copy(omniKeys = mapOf(0 to key))
                } else {
                    preview
                }
            }.getOrElse { ImportState.Invalid(it.userMessage()) }
        }
    }

    private suspend fun preview(text: String): ImportState {
        val (yaml, kube) = withContext(Dispatchers.IO) {
            val decoded = Ichorgo.decodeImportText(text)
            decoded to Ichorgo.isKubeconfig(decoded)
        }
        return if (kube) {
            ImportState.KubePreview(yaml, configs.validateKube(yaml), configs.kubeImportConflicts(yaml))
        } else {
            ImportState.Preview(yaml, configs.validate(yaml), configs.importConflicts(yaml))
        }
    }

    /** Stores the conflict at [index] under [name] (blank: the suggested name). */
    fun rename(index: Int, name: String) = updateChoice(index) { it.copy(name = name) }

    /** Replaces the stored context of the same cluster with the conflict at [index], or not. */
    fun setReplace(index: Int, replace: Boolean) = updateChoice(index) { it.copy(replace = replace) }

    /** The service account [key] for the Omni context at [index]; blank: none (sign in later). */
    fun setOmniKey(index: Int, key: String) {
        val preview = _state.value as? ImportState.Preview ?: return
        _state.value = preview.copy(omniKeys = if (key.isBlank()) preview.omniKeys - index else preview.omniKeys + (index to key))
    }

    /** Adds the kubeconfig context at [index], or leaves it out. */
    fun setIncluded(index: Int, included: Boolean) = updateChoice(index) { it.copy(skip = !included) }

    private fun updateChoice(index: Int, change: (ImportChoice) -> ImportChoice) {
        when (val preview = _state.value) {
            is ImportState.Preview -> {
                val choices = preview.choices.map { if (it.index == index) change(it) else it }
                _state.value = preview.copy(choices = choices, takenNames = takenNames(preview.conflicts, choices, preview.summary), error = null)
            }
            is ImportState.KubePreview -> {
                val choices = preview.choices.map { if (it.index == index) change(it) else it }
                _state.value = preview.copy(choices = choices, takenNames = takenNames(preview.conflicts, choices, preview.summary), error = null)
            }
            else -> Unit
        }
    }

    private fun takenNames(conflicts: List<ImportConflict>, choices: List<ImportChoice>, summary: ConfigSummary): Set<Int> {
        val stored = configs.config.value?.summary?.contexts.orEmpty().map { it.name }.toSet()
        return takenNameChoices(conflicts, choices, stored, summary.contexts.map { it.name })
    }

    /** Shows the cloud providers whose clusters can be added from an account. */
    fun startDiscovery(provider: DiscoveryProvider? = null) {
        _state.value = ImportState.Validating
        viewModelScope.launch {
            _state.value = runCatching { ImportState.Discover(discoveryFields(auth.discoveryFields()), initial = provider) }
                .getOrElse { ImportState.Invalid(it.userMessage()) }
        }
    }

    /**
     * Lists the clusters the [provider] account reaches with [secrets] and shows them in the
     * kubeconfig preview; the clusters added then sign in with the same [secrets].
     */
    fun discover(provider: DiscoveryProvider, secrets: Map<String, String>) {
        val discover = _state.value as? ImportState.Discover ?: return
        if (discover.running) return
        _state.value = discover.copy(running = true, error = null)
        viewModelScope.launch {
            _state.value = runCatching {
                val yaml = auth.discover(provider.id, secrets)
                ImportState.KubePreview(yaml, configs.validateKube(yaml), configs.kubeImportConflicts(yaml), discovery = secrets)
            }.getOrElse { discover.copy(running = false, error = it.userMessage()) }
        }
    }

    fun confirm() {
        val save: suspend () -> String? = when (val preview = _state.value) {
            is ImportState.Preview -> if (preview.canImport) ({ saveTalos(preview) }) else return
            is ImportState.KubePreview -> if (preview.canImport) ({ saveKube(preview).let { null } }) else return
            else -> return
        }
        val preview = _state.value
        _state.value = ImportState.Validating
        viewModelScope.launch {
            _state.value = runCatching { save() }.fold(
                onSuccess = { ImportState.Saved(warning = it) },
                onFailure = { failed(preview, it.userMessage()) },
            )
        }
    }

    /**
     * Stores the talosconfig of [preview], then the Omni keys entered for its contexts. A key
     * refused does not undo the import: its message comes back, the cluster then asks for a
     * sign-in.
     */
    private suspend fun saveTalos(preview: ImportState.Preview): String? {
        configs.save(preview.yaml, preview.choices)
        val failures = preview.omniKeys.mapNotNull { (index, key) ->
            val ctx = preview.summary.contexts.getOrNull(index)?.takeIf { it.isOmni } ?: return@mapNotNull null
            val name = storedContextName(index, ctx.name, preview.conflicts, preview.choices)
            runCatching { omni.setServiceAccount(name, key) }.exceptionOrNull()?.userMessage()
        }
        return failures.distinct().joinToString("\n").ifEmpty { null }
    }

    /** Stores the kept contexts of [preview]; those found in a cloud account sign in with its credentials. */
    private suspend fun saveKube(preview: ImportState.KubePreview) {
        val before = configs.config.value?.summary?.contexts.orEmpty().map { it.name }.toSet()
        configs.saveKube(preview.yaml, preview.choices)
        val secrets = preview.discovery ?: return
        val after = configs.config.value?.summary?.contexts.orEmpty().filter { it.isKube }.map { it.name }
        auth.signInDiscovered(importedContextNames(before, after, preview.conflicts, preview.choices), secrets)
    }

    /** [preview] again, with the save's [error]. */
    private fun failed(preview: ImportState, error: String): ImportState = when (preview) {
        is ImportState.Preview -> preview.copy(error = error)
        is ImportState.KubePreview -> preview.copy(error = error)
        else -> ImportState.Invalid(error)
    }

    fun reset() {
        _state.value = ImportState.Idle
    }

    fun startDemo() {
        if (_state.value is ImportState.Validating || _state.value is ImportState.Saved) return
        _state.value = ImportState.Validating
        viewModelScope.launch {
            _state.value = runCatching { configs.saveDemo() }.fold(
                onSuccess = { ImportState.Saved() },
                onFailure = { ImportState.Invalid(it.userMessage()) },
            )
        }
    }
}
