package name.levis.ichor.ui.alerts

import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.AmAlert
import name.levis.ichor.model.AmAlerts
import name.levis.ichor.model.AmFilter
import name.levis.ichor.model.AmGroup
import name.levis.ichor.model.AmSeverity
import name.levis.ichor.model.filtered
import name.levis.ichor.model.where
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/**
 * The alerts, grouped by alertname, worst severity first: the state chips (firing, silenced,
 * inhibited), the severity chips and a search above, pull to refresh. Tapping an alert opens it.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AlertsTab(
    state: UiState<AmAlerts>,
    filter: AmFilter,
    onFilter: (AmFilter) -> Unit,
    onAlert: (AmAlert) -> Unit,
    onRefresh: () -> Unit,
) {
    Column(Modifier.fillMaxSize()) {
        FilterBar(filter, onFilter)
        when (state) {
            UiState.Loading -> LoadingBox()
            is UiState.Failed -> ErrorBox(state.message, onRefresh)
            is UiState.Loaded -> PullToRefreshBox(isRefreshing = state.refreshing, onRefresh = onRefresh, modifier = Modifier.fillMaxSize()) {
                val groups = remember(state.data, filter) { state.data.filtered(filter) }
                LazyColumn(
                    Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    if (state.data.truncated) item { MutedText(stringResource(R.string.alerts_truncated, state.data.total)) }
                    if (groups.isEmpty()) {
                        val empty = if (state.data.total == 0) R.string.alerts_none else R.string.alerts_none_match
                        item { MutedText(stringResource(empty), Modifier.padding(vertical = 16.dp)) }
                    }
                    items(groups, key = { it.alertname }) { group -> GroupCard(group, onAlert) }
                }
            }
        }
    }
}

@Composable
private fun FilterBar(filter: AmFilter, onFilter: (AmFilter) -> Unit) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        SearchField(filter.query, { onFilter(filter.copy(query = it)) }, stringResource(R.string.alerts_search), Modifier.fillMaxWidth())
        // One scrolling row: the states, then the severities (none picked: all of them).
        Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(filter.active, { onFilter(filter.copy(active = !filter.active)) }, { Text(stringResource(R.string.alerts_state_firing)) })
            FilterChip(filter.silenced, { onFilter(filter.copy(silenced = !filter.silenced)) }, { Text(stringResource(R.string.alerts_state_silenced)) })
            FilterChip(filter.inhibited, { onFilter(filter.copy(inhibited = !filter.inhibited)) }, { Text(stringResource(R.string.alerts_state_inhibited)) })
            AmSeverity.ALL.forEach { severity ->
                val on = severity in filter.severities
                FilterChip(
                    selected = on,
                    onClick = { onFilter(filter.copy(severities = if (on) filter.severities - severity else filter.severities + severity)) },
                    label = { Text(severityLabel(severity)) },
                    leadingIcon = { SeverityDot(severity) },
                )
            }
        }
    }
}

/** An alertname: its severity, how many fire of how many, then each alert. */
@Composable
private fun GroupCard(group: AmGroup, onAlert: (AmAlert) -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 8.dp)) {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                SeverityDot(group.severity)
                Text(
                    group.alertname,
                    style = MaterialTheme.typography.titleSmall,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(start = 10.dp).weight(1f),
                )
                Text(
                    pluralStringResource(R.plurals.alerts_group_count, group.count, group.active, group.count),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            group.alerts.forEachIndexed { i, alert ->
                if (i > 0) HorizontalDivider(Modifier.padding(horizontal = 16.dp))
                AlertRow(alert, onClick = { onAlert(alert) })
            }
        }
    }
}

@Composable
private fun AlertRow(alert: AmAlert, onClick: () -> Unit) {
    val muted = LocalStatusColors.current.muted
    Column(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp)) {
        Text(
            alert.summary.ifEmpty { alert.where.ifEmpty { alert.alertname } },
            style = MaterialTheme.typography.bodyMedium,
            color = if (alert.suppressed) muted else MaterialTheme.colorScheme.onSurface,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        val parts = listOfNotNull(
            alert.where.takeIf { it.isNotEmpty() && alert.summary.isNotEmpty() },
            timeAgo(alert.startsAt).takeIf { it.isNotEmpty() },
            stateLabel(alert).takeIf { alert.suppressed },
        )
        if (parts.isNotEmpty()) MutedText(parts.joinToString(" · "), maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}
