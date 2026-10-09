package name.levis.ichor.ui.apps

import androidx.compose.foundation.clickable
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
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.podSelection
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.components.SectionTitle

/** What the app sheet needs to offer rollout restarts; null where the role cannot run them. */
data class AppRestartUi(
    val state: UiState<List<KubeWorkload>>,
    /** Keys of the workloads whose restart is in flight. */
    val restarting: Set<String>,
    val onRestart: (KubeWorkload) -> Unit,
    /** Opens the list of a workload's pods (a tap on its row). */
    val onPods: (KubeWorkload) -> Unit = {},
)

/** The app's workloads, each with a restart button; a short note while loading, on failure or with none. */
fun LazyListScope.appWorkloadsSection(restart: AppRestartUi) {
    item { SectionTitle(stringResource(R.string.apps_detail_workloads)) }
    when (val s = restart.state) {
        UiState.Loading -> item { MutedText(stringResource(R.string.apps_detail_workloads_loading)) }
        is UiState.Failed -> item { MutedText(stringResource(R.string.apps_detail_workloads_failed, s.message.asString())) }
        is UiState.Loaded -> if (s.data.isEmpty()) {
            item { MutedText(stringResource(R.string.apps_detail_workloads_none)) }
        } else {
            items(s.data, key = { it.key }) { w ->
                val onPods = if (w.podSelection != null) ({ restart.onPods(w) }) else null
                WorkloadRestartRow(w, w.key in restart.restarting, onPods) { restart.onRestart(w) }
            }
        }
    }
}

/** Name, "Deployment · 2/3 ready", and the restart button (a spinner while it runs); a tap opens its pods ([onPods]). */
@Composable
private fun WorkloadRestartRow(workload: KubeWorkload, restarting: Boolean, onPods: (() -> Unit)?, onRestart: () -> Unit) {
    val open = onPods?.let { Modifier.clickable(onClickLabel = stringResource(R.string.pods_title), onClick = it) } ?: Modifier
    Row(Modifier.fillMaxWidth().then(open).padding(vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
        val denial = rememberKubeDenial(KubeAction.restart(workload.kind), workload.namespace)
        Column(Modifier.weight(1f)) {
            Text(
                workload.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            MutedText(workload.kind + " · " + stringResource(R.string.workloads_ready_count, workload.ready, workload.desired))
            KubeDenialNote(denial)
        }
        if (restarting) {
            CircularProgressIndicator(Modifier.padding(horizontal = 24.dp).size(24.dp), strokeWidth = 2.dp)
        } else {
            FilledTonalButton(onClick = onRestart, enabled = workload.canRestart && denial == null, modifier = Modifier.padding(start = 12.dp)) {
                Icon(Icons.Outlined.RestartAlt, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.workloads_restart_confirm), modifier = Modifier.padding(start = 6.dp))
            }
        }
    }
}
