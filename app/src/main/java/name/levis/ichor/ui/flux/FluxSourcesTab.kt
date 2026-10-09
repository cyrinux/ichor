package name.levis.ichor.ui.flux

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Cloud
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.Pause
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Source
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.rememberFirstKubeDenial
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.model.FluxAction
import name.levis.ichor.model.FluxSource
import name.levis.ichor.model.FluxState
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.shortFluxRevision
import name.levis.ichor.model.sortedSources
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.util.timeAgo

/** The Git, OCI and Helm repositories and buckets, worst first, each with Reconcile and Suspend/Resume ([onAct]). */
@Composable
fun FluxSourcesTab(status: FluxStatus, busy: Set<String>, onAct: (FluxSource, FluxAction) -> Unit) {
    val sources = status.sortedSources
    // Each kind has its own permission, in each namespace: the first one refused stands for the list.
    val checks = remember(sources) { sources.map { KubeAction.fluxReconcile(it.kind) to it.namespace }.distinct() }
    val listDenial = rememberFirstKubeDenial(checks)
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        if (status.sourcesError.isNotEmpty()) item(key = "sources-error") {
            InlineError(stringResource(R.string.data_services_unreadable, status.sourcesError), Modifier.padding(16.dp))
        }
        if (listDenial != null) item(key = "denied") { KubeDenialNote(listDenial, Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) }
        if (sources.isEmpty() && status.sourcesError.isEmpty()) item(key = "sources-empty") { EmptyText(stringResource(R.string.flux_no_sources)) }
        items(sources, key = { it.key }) { src ->
            SourceRow(src, src.key in busy) { onAct(src, it) }
            HorizontalDivider()
        }
    }
}

/**
 * Kind icon, name and kind, URL, ref and short revision, when it was last fetched and how many
 * apps use it; its reason when not ready; Reconcile (not while suspended) and Suspend or Resume.
 */
@Composable
private fun SourceRow(src: FluxSource, busy: Boolean, onAct: (FluxAction) -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val state = FluxState.of(src.serviceHealth, src.suspended, src.isBusy)
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 10.dp, bottom = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(src.kindIcon, contentDescription = src.kind, tint = muted, modifier = Modifier.size(24.dp))
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(src.name, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                Spacer(Modifier.size(6.dp))
                FluxStateGlyph(state, 16.dp)
            }
            Text(src.kind, style = MaterialTheme.typography.labelSmall, color = muted)
            if (src.url.isNotEmpty()) {
                Text(src.url, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Text(
                listOf(
                    src.ref,
                    shortFluxRevision(src.revision),
                    timeAgo(src.fetchedAt).ifEmpty { stringResource(R.string.flux_never_fetched) },
                    pluralStringResource(R.plurals.argo_apps, src.apps, src.apps),
                ).filter { it.isNotEmpty() }.joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                color = muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (state == FluxState.FAILING && (src.reason.isNotEmpty() || src.message.isNotEmpty())) {
                Text(
                    listOf(src.reason, src.message).filter { it.isNotEmpty() }.joinToString(": "),
                    style = MaterialTheme.typography.labelSmall,
                    color = state.color(),
                    maxLines = 3,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        if (busy) {
            CircularProgressIndicator(Modifier.padding(horizontal = 14.dp).size(20.dp), strokeWidth = 2.dp)
        } else {
            val allowed = rememberKubeDenial(KubeAction.fluxReconcile(src.kind), src.namespace) == null
            TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.flux_reconcile), onClick = { onAct(FluxAction.RECONCILE) }, enabled = !src.suspended && allowed)
            if (src.suspended) {
                TooltipIconButton(Icons.Outlined.PlayArrow, stringResource(R.string.flux_resume), onClick = { onAct(FluxAction.RESUME) }, enabled = allowed)
            } else {
                TooltipIconButton(Icons.Outlined.Pause, stringResource(R.string.flux_suspend), onClick = { onAct(FluxAction.SUSPEND) }, enabled = allowed)
            }
        }
    }
}

private val FluxSource.kindIcon: ImageVector
    get() = when (kind) {
        "GitRepository" -> Icons.Outlined.Source
        "OCIRepository" -> Icons.Outlined.Inventory2
        "HelmRepository" -> Icons.Outlined.Storage
        else -> Icons.Outlined.Cloud
    }
