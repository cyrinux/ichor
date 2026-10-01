package name.levis.talosmobile.ui.network

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.talosmobile.R
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.model.ConnectionFilter
import name.levis.talosmobile.model.ConnectionInfo
import name.levis.talosmobile.model.endpoint
import name.levis.talosmobile.model.filtered
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.DataFreshness
import name.levis.talosmobile.ui.components.ErrorBox
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.factory

/** Sockets change constantly: not cached, fetched on open and on pull-to-refresh. */
class ConnectionsViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<List<ConnectionInfo>>() {
    override suspend fun fetch() = talos.connections(node)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConnectionsTab(
    node: String,
    vm: ConnectionsViewModel = viewModel(key = "connections-$node", factory = factory { ConnectionsViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var filter by rememberSaveable { mutableStateOf(ConnectionFilter.LISTENING) }
    var query by rememberSaveable { mutableStateOf("") }

    when (val s = state) {
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(s.message, vm::refresh)
        is UiState.Loaded -> Column(Modifier.fillMaxSize()) {
            val rows = remember(s.data, filter, query) { s.data.filtered(filter, query) }
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                OutlinedTextField(
                    value = query,
                    onValueChange = { query = it },
                    placeholder = { Text(stringResource(R.string.connections_search)) },
                    leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                    FilterChip(
                        selected = filter == ConnectionFilter.LISTENING,
                        onClick = { filter = ConnectionFilter.LISTENING },
                        label = { Text(stringResource(R.string.connections_listening)) },
                    )
                    FilterChip(
                        selected = filter == ConnectionFilter.ALL,
                        onClick = { filter = ConnectionFilter.ALL },
                        label = { Text(stringResource(R.string.connections_all)) },
                    )
                    Text(
                        pluralStringResource(R.plurals.connections_count, rows.size, rows.size),
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            HorizontalDivider()
            PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                if (rows.isEmpty()) {
                    Text(
                        stringResource(R.string.connections_empty),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(16.dp),
                    )
                } else {
                    LazyColumn(Modifier.fillMaxSize()) {
                        items(rows) { c ->
                            ConnectionRow(c)
                            HorizontalDivider()
                        }
                    }
                }
            }
            DataFreshness(s, edgeToEdge = false)
        }
    }
}

@Composable
private fun ConnectionRow(c: ConnectionInfo) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                endpoint(c.localIp, c.localPort),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                fontWeight = FontWeight.Bold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(c.protocol, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace, color = muted)
        }
        if (!c.listening) {
            Text(
                stringResource(R.string.connections_remote, endpoint(c.remoteIp, c.remotePort)),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = muted,
            )
        }
        val process = when {
            c.processName.isNotEmpty() && c.pid > 0 -> stringResource(R.string.connections_process, c.processName, c.pid.toInt())
            c.processName.isNotEmpty() -> c.processName
            else -> null
        }
        Text(
            listOfNotNull(c.state.ifEmpty { null }, process).joinToString("  ·  "),
            style = MaterialTheme.typography.labelSmall,
            color = muted,
        )
    }
}
