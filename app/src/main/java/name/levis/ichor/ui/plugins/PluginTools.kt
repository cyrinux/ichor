package name.levis.ichor.ui.plugins

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.PluginRepository
import name.levis.ichor.model.PluginRef
import name.levis.ichor.model.PluginRow
import name.levis.ichor.model.PluginTool
import name.levis.ichor.model.PluginToolResult
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.color
import name.levis.ichor.ui.uiText

/**
 * The last tool run of a plugin screen: [running] while Go works, then its [result] or its
 * [error]. [label] is the tool's button text, for the progress and error lines.
 */
@Immutable
data class PluginToolRun(
    val toolId: String,
    val label: String,
    val running: Boolean = true,
    val result: PluginToolResult? = null,
    val error: UiText? = null,
)

/**
 * Runs plugin [pluginId]'s tools in [scope] (the screen's ViewModel's), one at a time, over every
 * namespace. The result stays until [clear] (the screen reads the plugin again).
 */
class PluginToolRuns(
    private val scope: CoroutineScope,
    private val plugins: PluginRepository,
    private val pluginId: String,
) {
    private val _run = MutableStateFlow<PluginToolRun?>(null)
    val run: StateFlow<PluginToolRun?> = _run.asStateFlow()
    private var job: Job? = null

    fun start(tool: PluginTool) {
        if (_run.value?.running == true) return
        _run.value = PluginToolRun(tool.id, tool.label)
        job = scope.launch {
            val outcome = cancellableCatching { plugins.tool(pluginId, tool.id, "", pluginLocale()) }
            _run.value = PluginToolRun(tool.id, tool.label, running = false, result = outcome.getOrNull(), error = outcome.exceptionOrNull()?.uiText())
        }
    }

    /** Drops the result, and stops a run still going. */
    fun clear() {
        job?.cancel()
        job = null
        _run.value = null
    }
}

/** The result's cards ready to draw, keyed apart from the tree's cards (they may share ids). */
internal fun toolCardLayouts(result: PluginToolResult?): List<CardLayout> =
    result?.shownCards.orEmpty().map { layoutOf(it.copy(id = "tool:${it.id}")) }

/**
 * The plugin's tool buttons, each with its hint; under them the run in progress, its error,
 * or its result: the title toned by its health, then its cards drawn as the tree's are.
 */
internal fun LazyListScope.pluginTools(
    tools: List<PluginTool>,
    run: PluginToolRun?,
    resultCards: List<CardLayout>,
    onRun: (PluginTool) -> Unit,
    expanded: Set<String>,
    onToggle: (String) -> Unit,
    onObject: (PluginRef) -> Unit,
    runner: PluginActionRunner,
) {
    item(key = "tools") { PluginToolButtons(tools, busy = run?.running == true, onRun) }
    when {
        run == null -> Unit
        run.running -> item(key = "tool-running") { PluginToolProgress(run.label) }
        run.error != null -> item(key = "tool-error") {
            InlineError(stringResource(R.string.plugin_tool_failed, run.label, run.error.asString()), Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
        }
        run.result != null -> {
            val result = run.result
            item(key = "tool-title") { PluginToolTitle(result) }
            if (resultCards.isEmpty()) item(key = "tool-empty") { EmptyText(stringResource(R.string.plugin_tool_result_empty)) }
            val noExtra: (PluginRow) -> (@Composable () -> Unit)? = { null }
            resultCards.forEach { layout -> pluginCard(layout, expanded, onToggle, onObject, noExtra, runner) }
        }
    }
}

@Composable
private fun PluginToolButtons(tools: List<PluginTool>, busy: Boolean, onRun: (PluginTool) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        tools.forEach { tool ->
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                IchorOutlinedButton(onClick = { onRun(tool) }, enabled = !busy) { Text(tool.label) }
                if (tool.hint.isNotEmpty()) MutedText(tool.hint)
            }
        }
    }
}

@Composable
private fun PluginToolProgress(label: String) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
        Spacer(Modifier.size(12.dp))
        MutedText(stringResource(R.string.plugin_tool_running, label))
    }
}

@Composable
private fun PluginToolTitle(result: PluginToolResult) {
    val health = result.serviceHealth
    Row(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        HealthDot(health)
        Spacer(Modifier.size(10.dp))
        Text(result.title, style = MaterialTheme.typography.titleSmall, color = health.color(), modifier = Modifier.weight(1f))
    }
}
