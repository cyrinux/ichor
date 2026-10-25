package name.levis.ichor.ui.dataservices

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.model.GarageInstance
import name.levis.ichor.model.GarageLayout
import name.levis.ichor.model.GarageLayoutNode
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText

/** The layout sheet's state, for [instance]. */
data class GarageLayoutState(
    val instance: GarageInstance,
    /** The garage-operator declares this cluster's layout: shown, not edited from here. */
    val managed: Boolean = false,
    val layout: GarageLayout? = null,
    /** The last load failed (the layout, if any, is the previous one). */
    val error: UiText? = null,
    val loading: Boolean = true,
    /** A stage, apply or revert is in flight. */
    val busy: Boolean = false,
)

/** The role editor's state: the node whose role is edited (or given), once the layout is known. */
data class GarageRoleEditState(
    val instance: GarageInstance,
    val nodeId: String,
    val layout: GarageLayout? = null,
    val error: UiText? = null,
    val loading: Boolean = true,
    val saving: Boolean = false,
) {
    val node: GarageLayoutNode? get() = layout?.node(nodeId)
}

/** Outcome of a layout action, shown once. */
sealed interface GarageLayoutResult {
    data class Staged(val node: String, val error: UiText?) : GarageLayoutResult
    data class RemovalStaged(val node: String, val error: UiText?) : GarageLayoutResult
    data class RedundancyStaged(val value: String, val error: UiText?) : GarageLayoutResult
    data class Applied(val version: Long, val messages: List<String>, val error: UiText?) : GarageLayoutResult
    data class Reverted(val error: UiText?) : GarageLayoutResult
}

/**
 * The Garage cluster layout, run in [scope] (a ViewModel's): the layout sheet, the role editor
 * and the staging, apply and revert of changes. A write answers the fresh layout, which the
 * open sheet shows at once; [onChanged] runs after an apply (the instance's layout version moves).
 */
class GarageLayoutActions(
    private val scope: CoroutineScope,
    private val dataServices: DataServicesRepository,
    private val onChanged: () -> Unit,
) {
    private val actions = KeyedActions<GarageLayoutResult>(scope)
    val results: Flow<GarageLayoutResult> get() = actions.results

    private val _sheet = MutableStateFlow<GarageLayoutState?>(null)
    /** The open layout sheet, null when closed. */
    val sheet: StateFlow<GarageLayoutState?> = _sheet.asStateFlow()
    private var loadJob: Job? = null

    private val _editor = MutableStateFlow<GarageRoleEditState?>(null)
    /** The open role editor, null when closed. */
    val editor: StateFlow<GarageRoleEditState?> = _editor.asStateFlow()
    private var editorJob: Job? = null

    fun open(instance: GarageInstance, managed: Boolean) {
        _sheet.value = GarageLayoutState(instance, managed = managed)
        reload()
    }

    fun close() {
        loadJob?.cancel()
        _sheet.value = null
    }

    /** Loads the open sheet's layout again, keeping the previous one on screen meanwhile. */
    fun reload() {
        val instance = _sheet.value?.instance ?: return
        loadJob?.cancel()
        _sheet.update { it?.copy(loading = true, error = null) }
        loadJob = scope.launch {
            try {
                val layout = dataServices.garageLayout(instance)
                _sheet.update { it?.copy(layout = layout, loading = false) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _sheet.update { it?.copy(error = e.uiText(), loading = false) }
            }
        }
    }

    /**
     * Opens the role editor for [nodeId] of [instance]: the layout is read first (or taken from
     * the open sheet) so the editor starts from the node's current or staged role.
     */
    fun editRole(instance: GarageInstance, nodeId: String) {
        editorJob?.cancel()
        val known = _sheet.value?.takeIf { it.instance.label == instance.label }?.layout
        _editor.value = GarageRoleEditState(instance, nodeId, layout = known, loading = known == null)
        if (known != null) return
        editorJob = scope.launch {
            try {
                val layout = dataServices.garageLayout(instance)
                _editor.update { it?.copy(layout = layout, loading = false) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _editor.update { it?.copy(error = e.uiText(), loading = false) }
            }
        }
    }

    fun closeEditor() {
        editorJob?.cancel()
        _editor.value = null
    }

    /** Stages the edited role, then shows the layout sheet with the staged change and its preview. */
    fun stageRole(zone: String, capacity: Long, gateway: Boolean, tags: List<String>) {
        val edit = _editor.value ?: return
        if (edit.saving) return
        val label = edit.node?.label ?: edit.nodeId.take(16)
        actions.launch(STAGE_KEY + edit.instance.label, {
            _editor.update { it?.copy(saving = true) }
            try {
                dataServices.garageStageRole(edit.instance, edit.nodeId, zone, capacity, gateway, tags)
            } finally {
                _editor.update { it?.copy(saving = false) }
            }
        }, { outcome ->
            outcome.getOrNull()?.let { show(edit.instance, it) }
            GarageLayoutResult.Staged(label, outcome.exceptionOrNull()?.uiText())
        }) { closeEditor() }
    }

    /** Stages the removal of the node [nodeId] (shown as [label]) from [instance]'s layout, then shows the layout sheet. */
    fun stageRemoval(instance: GarageInstance, nodeId: String, label: String) {
        write(instance, { dataServices.garageStageRemoval(instance, nodeId) }) { GarageLayoutResult.RemovalStaged(label, it) }
    }

    fun setZoneRedundancy(value: String) {
        val instance = _sheet.value?.instance ?: return
        write(instance, { dataServices.garageSetZoneRedundancy(instance, value) }) { GarageLayoutResult.RedundancyStaged(value, it) }
    }

    /** Applies the staged changes as [version], the one the sheet showed. */
    fun apply(version: Long) {
        val instance = _sheet.value?.instance ?: return
        write(instance, { dataServices.garageApplyLayout(instance, version) }, after = onChanged) { layout, error ->
            GarageLayoutResult.Applied(version, layout?.messages.orEmpty(), error)
        }
    }

    fun revert() {
        val instance = _sheet.value?.instance ?: return
        write(instance, { dataServices.garageRevertLayout(instance) }) { GarageLayoutResult.Reverted(it) }
    }

    private fun write(instance: GarageInstance, action: suspend () -> GarageLayout, result: (UiText?) -> GarageLayoutResult) =
        write(instance, action, after = {}) { _, error -> result(error) }

    /** Runs one write on [instance]'s layout, the sheet busy meanwhile, and shows the layout it answers. */
    private fun write(instance: GarageInstance, action: suspend () -> GarageLayout, after: () -> Unit, result: (GarageLayout?, UiText?) -> GarageLayoutResult) {
        if (_sheet.value?.busy == true) return
        actions.launch(WRITE_KEY + instance.label, {
            _sheet.update { it?.copy(busy = true) }
            try {
                action()
            } finally {
                _sheet.update { it?.copy(busy = false) }
            }
        }, { outcome ->
            outcome.getOrNull()?.let { show(instance, it) }
            result(outcome.getOrNull(), outcome.exceptionOrNull()?.uiText())
        }, after)
    }

    /** Puts a layout a write answered in the sheet, opening it on [instance] when it is not. */
    private fun show(instance: GarageInstance, layout: GarageLayout) {
        loadJob?.cancel()
        _sheet.update { current ->
            if (current != null && current.instance.label == instance.label) {
                current.copy(layout = layout, loading = false, error = null)
            } else {
                GarageLayoutState(instance, layout = layout, loading = false)
            }
        }
    }

    companion object {
        private const val STAGE_KEY = "stage|"
        private const val WRITE_KEY = "layout|"
    }
}
