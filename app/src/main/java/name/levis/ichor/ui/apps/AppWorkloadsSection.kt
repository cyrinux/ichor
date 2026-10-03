package name.levis.ichor.ui.apps

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.SectionTitle

/** What the app sheet needs to offer rollout restarts; null where the role cannot run them. */
data class AppRestartUi(
    val state: UiState<List<KubeWorkload>>,
    /** Keys of the workloads whose restart is in flight. */
    val restarting: Set<String>,
    val onRestart: (KubeWorkload) -> Unit,
)

/** The app's workloads, each with a restart button; a short note while loading, on failure or with none. */
fun LazyListScope.appWorkloadsSection(restart: AppRestartUi) {
    item { SectionTitle(stringResource(R.string.apps_detail_workloads)) }
    when (val s = restart.state) {
        UiState.Loading -> item { Note(stringResource(R.string.apps_detail_workloads_loading)) }
        is UiState.Failed -> item { Note(stringResource(R.string.apps_detail_workloads_failed, s.message.asString())) }
        is UiState.Loaded -> if (s.data.isEmpty()) {
            item { Note(stringResource(R.string.apps_detail_workloads_none)) }
        } else {
            items(s.data, key = { it.key }) { w -> WorkloadRestartRow(w, w.key in restart.restarting) { restart.onRestart(w) } }
        }
    }
}

@Composable
private fun Note(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** Name, "Deployment · 2/3 ready", and the restart button (a spinner while it runs). */
@Composable
private fun WorkloadRestartRow(workload: KubeWorkload, restarting: Boolean, onRestart: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(
                workload.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                workload.kind + " · " + stringResource(R.string.workloads_ready_count, workload.ready, workload.desired),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (restarting) {
            CircularProgressIndicator(Modifier.padding(horizontal = 24.dp).size(24.dp), strokeWidth = 2.dp)
        } else {
            FilledTonalButton(onClick = onRestart, enabled = workload.canRestart, modifier = Modifier.padding(start = 12.dp)) {
                Icon(Icons.Outlined.RestartAlt, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.workloads_restart_confirm), modifier = Modifier.padding(start = 6.dp))
            }
        }
    }
}
