package name.levis.talosmobile.ui.support

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.talosmobile.R
import name.levis.talosmobile.data.ConfigRepository
import name.levis.talosmobile.data.OVERVIEW
import name.levis.talosmobile.data.SupportBundleFile
import name.levis.talosmobile.data.SupportBundleRepository
import name.levis.talosmobile.data.SupportEvent
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.model.ClusterOverview
import name.levis.talosmobile.model.NodeOverview
import name.levis.talosmobile.model.BundleProgress
import name.levis.talosmobile.model.defaultBundleNodes
import name.levis.talosmobile.model.supportBundleFileName
import name.levis.talosmobile.ui.UiText
import name.levis.talosmobile.ui.uiText
import java.io.File

sealed interface SupportRun {
    data object Idle : SupportRun

    /** [nodes] in the order chosen; [progress] tells which one is being collected. */
    data class Running(val nodes: List<String>, val progress: BundleProgress) : SupportRun
    data class Done(val name: String, val size: Long) : SupportRun
    data class Failed(val message: UiText) : SupportRun
}

data class SupportBundleState(
    /** Nodes to choose from; null until the overview is known. */
    val nodes: List<NodeOverview>? = null,
    val nodesError: UiText? = null,
    val selected: Set<String> = emptySet(),
    val run: SupportRun = SupportRun.Idle,
    val files: List<SupportBundleFile> = emptyList(),
) {
    fun hostname(node: String): String = nodes?.firstOrNull { it.node == node }?.hostname ?: node
}

/**
 * Collects a support bundle into the app's private `support/` directory. Leaving the screen
 * clears this view model, which cancels a running collection and removes its partial file.
 */
class SupportBundleViewModel(
    private val bundles: SupportBundleRepository,
    private val talos: TalosRepository,
    val configs: ConfigRepository,
) : ViewModel() {
    private val _state = MutableStateFlow(SupportBundleState())
    val state: StateFlow<SupportBundleState> = _state.asStateFlow()
    private var job: Job? = null

    /** Lists the saved bundles and, once, the nodes (all selected). */
    fun load() {
        viewModelScope.launch { refreshFiles() }
        if (_state.value.nodes != null) return
        _state.update { it.copy(nodesError = null) }
        viewModelScope.launch {
            try {
                val nodes = (talos.cached<ClusterOverview>(OVERVIEW)?.value ?: talos.overview()).nodes
                _state.update { it.copy(nodes = nodes, selected = defaultBundleNodes(nodes)) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _state.update { it.copy(nodesError = e.uiText()) }
            }
        }
    }

    fun toggle(node: String) {
        _state.update { it.copy(selected = if (node in it.selected) it.selected - node else it.selected + node) }
    }

    fun start() {
        val current = _state.value
        val nodes = current.nodes.orEmpty().map { it.node }.filter { it in current.selected }
        if (current.run is SupportRun.Running || nodes.isEmpty()) return
        val context = configs.config.value?.activeContext.orEmpty()
        val dest = File(bundles.dir, supportBundleFileName(context, System.currentTimeMillis()))
        _state.update { it.copy(run = SupportRun.Running(nodes, BundleProgress())) }
        job = viewModelScope.launch {
            var kept = false
            try {
                var result: SupportEvent? = null
                bundles.collect(nodes, dest).collect { event ->
                    if (event is SupportEvent.Progress) {
                        _state.update { s ->
                            val run = s.run as? SupportRun.Running ?: return@update s
                            s.copy(run = run.copy(progress = run.progress.with(event.progress)))
                        }
                    } else {
                        result = event
                    }
                }
                val outcome = when (val r = result) {
                    is SupportEvent.Done -> {
                        kept = true
                        SupportRun.Done(File(r.path).name, r.size)
                    }
                    is SupportEvent.Failed -> SupportRun.Failed(UiText.Raw(r.message))
                    else -> SupportRun.Failed(UiText.Res(R.string.support_bundle_interrupted))
                }
                _state.update { it.copy(run = outcome) }
            } catch (e: CancellationException) {
                _state.update { it.copy(run = SupportRun.Idle) }
                throw e
            } catch (e: Throwable) {
                _state.update { it.copy(run = SupportRun.Failed(e.uiText())) }
            } finally {
                withContext(NonCancellable) {
                    // A partial zip must not pass for a bundle.
                    if (!kept) bundles.discard(dest)
                    refreshFiles()
                }
            }
        }
    }

    fun cancel() {
        job?.cancel()
    }

    fun delete(file: File) {
        viewModelScope.launch {
            bundles.delete(file)
            refreshFiles()
        }
    }

    private suspend fun refreshFiles() {
        val files = bundles.list()
        _state.update { it.copy(files = files) }
    }
}
