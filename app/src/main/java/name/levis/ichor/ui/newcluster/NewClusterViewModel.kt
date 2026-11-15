package name.levis.ichor.ui.newcluster

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.JoinManager
import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.NewClusterStart
import name.levis.ichor.data.NodeLifecycleRepository
import name.levis.ichor.model.MaintenanceInspection
import name.levis.ichor.model.NewClusterDraft
import name.levis.ichor.model.NewClusterSpec
import name.levis.ichor.model.joinAddress
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.uiStateOf

/** The demo's maintenance address (go/ichorgo/join_inspect.go): previews work, the run is refused. */
internal const val DEMO_MACHINE = "demo"

/** What the person typed for the new cluster; the address is the inspected machine's. */
data class NewClusterForm(
    val name: String = "",
    val disk: String = "",
    val kubernetesVersion: String = "",
    val schedule: Boolean = true,
) {
    val complete: Boolean get() = name.isNotBlank() && disk.isNotBlank()
}

/**
 * A new cluster from a machine in maintenance mode: inspect it, prepare the draft (secrets held
 * by the Go core), then store its talosconfig and start the run that [JoinManager] follows.
 */
class NewClusterViewModel(
    private val lifecycle: NodeLifecycleRepository,
    private val configs: ConfigRepository,
    private val joins: JoinManager,
) : ViewModel() {
    private val _address = MutableStateFlow("")
    val address: StateFlow<String> = _address.asStateFlow()
    private val _inspection = MutableStateFlow<UiState<MaintenanceInspection>?>(null)
    val inspection: StateFlow<UiState<MaintenanceInspection>?> = _inspection.asStateFlow()
    private val _form = MutableStateFlow(NewClusterForm())
    val form: StateFlow<NewClusterForm> = _form.asStateFlow()
    private val _draft = MutableStateFlow<UiState<NewClusterDraft>?>(null)
    val draft: StateFlow<UiState<NewClusterDraft>?> = _draft.asStateFlow()
    /** Why the cluster could not be stored or started (a string resource and its argument). */
    private val _startError = MutableStateFlow<Pair<Int, String>?>(null)
    val startError: StateFlow<Pair<Int, String>?> = _startError.asStateFlow()
    private var working: Job? = null

    /** A new address forgets the inspection and the draft. */
    fun type(address: String) {
        if (address != _address.value) {
            working?.cancel()
            _inspection.value = null
            _draft.value = null
        }
        _address.value = address
    }

    fun inspect() {
        val address = joinAddress(_address.value).ifEmpty { return }
        working?.cancel()
        _inspection.value = UiState.Loading
        _draft.value = null
        working = viewModelScope.launch { _inspection.value = uiStateOf { lifecycle.maintenanceNodeInspect(address) } }
    }

    fun name(value: String) = edit { it.copy(name = value.trim()) }

    fun disk(value: String) = edit { it.copy(disk = value.trim()) }

    fun kubernetesVersion(value: String) = edit { it.copy(kubernetesVersion = value.trim()) }

    fun schedule(value: Boolean) = edit { it.copy(schedule = value) }

    fun prepare() {
        val machine = inspected() ?: return
        val form = _form.value.takeIf { it.complete } ?: return
        working?.cancel()
        _draft.value = UiState.Loading
        _startError.value = null
        working = viewModelScope.launch {
            _draft.value = uiStateOf {
                lifecycle.newClusterPrepare(
                    NewClusterSpec(
                        name = form.name,
                        endpoint = machine.address,
                        kubernetesVersion = form.kubernetesVersion,
                        talosVersion = machine.version,
                        installDisk = form.disk,
                        allowSchedulingOnControlPlanes = form.schedule,
                    ),
                )
            }
        }
    }

    /**
     * Stores the draft's talosconfig as the context named like the cluster, then starts the run:
     * the store comes first, so the cluster's access exists before anything is sent. The demo's
     * machine stores nothing (the Go core refuses its run).
     */
    fun create() {
        val machine = inspected() ?: return
        val draft = (_draft.value as? UiState.Loaded)?.data ?: return
        val name = _form.value.name
        if (joins.current.value?.running == true) {
            _startError.value = R.string.new_cluster_busy to ""
            return
        }
        viewModelScope.launch {
            val stored = runCatching {
                if (machine.address != DEMO_MACHINE) {
                    if (configs.importConflicts(draft.talosconfig).isNotEmpty()) {
                        _startError.value = R.string.new_cluster_name_taken to name
                        return@launch
                    }
                    configs.save(draft.talosconfig)
                }
            }
            stored.exceptionOrNull()?.let {
                _startError.value = R.string.new_cluster_store_failed to (it.message ?: it.toString())
                return@launch
            }
            _startError.value = null
            joins.start(
                JoinRequest(
                    address = machine.address, template = "", role = "controlplane", hostname = name,
                    disk = _form.value.disk, staticAddress = "",
                    newCluster = NewClusterStart(draft.draftId, name, draft.talosconfig),
                ),
            )
        }
    }

    /** Removes the context a run that sent nothing left behind. */
    fun removeEntry(name: String, then: () -> Unit) {
        viewModelScope.launch {
            runCatching { configs.removeContext(name) }
            joins.dismiss()
            then()
        }
    }

    private fun inspected(): MaintenanceInspection? =
        (_inspection.value as? UiState.Loaded)?.data?.takeIf { it.maintenance }

    /** Any change makes the prepared draft stale. */
    private fun edit(change: (NewClusterForm) -> NewClusterForm) {
        _form.update(change)
        if (_draft.value != null) {
            working?.cancel()
            _draft.value = null
        }
    }
}
