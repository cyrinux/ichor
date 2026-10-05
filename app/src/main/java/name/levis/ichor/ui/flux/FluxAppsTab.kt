package name.levis.ichor.ui.flux

import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.FluxApp
import name.levis.ichor.model.FluxFilter
import name.levis.ichor.model.FluxState
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.fluxFilterCounts
import name.levis.ichor.model.fluxFiltered
import name.levis.ichor.model.sortedApps
import name.levis.ichor.ui.argocd.OwnerChip
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.util.timeAgo

private val ICON = 40.dp

/** Every Kustomization and HelmRelease, worst first: filter chips with counts and a search field above the rows. */
@Composable
fun FluxAppsTab(status: FluxStatus, busy: Set<String>, onOpen: (FluxApp) -> Unit) {
    var filter by rememberSaveable { mutableStateOf(FluxFilter.ALL) }
    var query by rememberSaveable { mutableStateOf("") }
    val apps = remember(status) { status.sortedApps }
    val counts = remember(apps) { apps.fluxFilterCounts() }
    val shown = remember(apps, filter, query) { apps.fluxFiltered(filter, query) }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        item(key = "controls") { Controls(query, { query = it }, filter, { filter = it }, counts) }
        if (status.helmError.isNotEmpty()) item(key = "helm-error") {
            InlineError(stringResource(R.string.data_services_unreadable, status.helmError), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        }
        item(key = "divider") { HorizontalDivider() }
        if (shown.isEmpty()) item(key = "empty") {
            EmptyText(
                when {
                    apps.isEmpty() -> stringResource(R.string.flux_no_apps)
                    query.isNotBlank() -> stringResource(R.string.data_services_no_match, query.trim())
                    else -> stringResource(R.string.data_services_all_fine)
                },
            )
        }
        items(shown, key = { it.key }) { app ->
            FluxAppRow(app, busy = app.key in busy, onClick = { onOpen(app) })
            HorizontalDivider()
        }
    }
}

@Composable
private fun Controls(query: String, onQuery: (String) -> Unit, filter: FluxFilter, onFilter: (FluxFilter) -> Unit, counts: Map<FluxFilter, Int>) {
    Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        SearchField(query, onQuery, stringResource(R.string.argo_search), Modifier.padding(horizontal = 16.dp).fillMaxWidth())
        LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(FluxFilter.entries.filter { it != FluxFilter.ALL }, key = { it.name }) { f ->
                val count = counts[f] ?: 0
                FilterChip(
                    selected = filter == f,
                    onClick = { onFilter(if (filter == f) FluxFilter.ALL else f) },
                    enabled = count > 0 || filter == f,
                    label = {
                        Text(stringResource(f.label))
                        Text(count.toString(), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(start = 6.dp))
                    },
                )
            }
        }
    }
}

/**
 * Icon, name with the Kustomization applying it, kind and short revision (chart@version for a
 * release) and when Ready last changed, its state glyph; under it the reason when it is not ready.
 */
@Composable
fun FluxAppRow(app: FluxApp, busy: Boolean, onClick: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(
        Modifier.fillMaxWidth().clickable(onClickLabel = stringResource(R.string.common_open), onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        FluxAppIcon(app, ICON)
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(app.name, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                app.owner?.let { OwnerChip(it.asArgoOwner(), Modifier.padding(start = 6.dp)) }
            }
            Text(
                listOf(app.kind, app.versionLabel, timeAgo(app.reconciledAt)).filter { it.isNotEmpty() }.joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (app.state == FluxState.FAILING || app.state == FluxState.RECONCILING) {
                Text(
                    listOf(app.reason, app.message).filter { it.isNotEmpty() }.joinToString(": "),
                    style = MaterialTheme.typography.labelSmall,
                    color = app.state.color(),
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Spacer(Modifier.size(8.dp))
        if (busy) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) else FluxStateGlyph(app.state)
    }
}

private val FluxFilter.label: Int
    get() = when (this) {
        FluxFilter.ALL -> R.string.data_services_filter_all
        FluxFilter.FAILING -> R.string.flux_filter_failing
        FluxFilter.RECONCILING -> R.string.flux_filter_reconciling
        FluxFilter.SUSPENDED -> R.string.flux_filter_suspended
        FluxFilter.KUSTOMIZATIONS -> R.string.flux_filter_kustomizations
        FluxFilter.HELM_RELEASES -> R.string.flux_filter_helm_releases
    }
