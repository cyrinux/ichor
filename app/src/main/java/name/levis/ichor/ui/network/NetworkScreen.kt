package name.levis.ichor.ui.network

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
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
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.networkKey
import name.levis.ichor.model.AddressInfo
import name.levis.ichor.model.LinkInfo
import name.levis.ichor.model.NetworkSection
import name.levis.ichor.model.NodeNetwork
import name.levis.ichor.model.RouteInfo
import name.levis.ichor.model.hiddenVirtualCount
import name.levis.ichor.model.isDefault
import name.levis.ichor.model.isUp
import name.levis.ichor.model.visible
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

class NetworkViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<NodeNetwork>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<NodeNetwork>? = talos.cached(networkKey(node))
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.network(node)
}

/** A node's links, addresses, routes, DNS and NTP servers, plus its sockets. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NetworkScreen(node: String, hostname: String, onBack: () -> Unit) {
    var tab by rememberSaveable { mutableIntStateOf(0) }
    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.network_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            PrimaryTabRow(selectedTabIndex = tab) {
                Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.network_tab_interfaces)) })
                Tab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.network_tab_connections)) })
            }
            when (tab) {
                0 -> InterfacesTab(node)
                else -> ConnectionsTab(node)
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun InterfacesTab(
    node: String,
    vm: NetworkViewModel = viewModel(key = "network-$node", factory = factory { NetworkViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var showVirtual by rememberSaveable { mutableStateOf(false) }

    when (val s = state) {
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(s.message, vm::refresh)
        is UiState.Loaded -> Column(Modifier.fillMaxSize()) {
            PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                NetworkContent(s.data, showVirtual, onShowVirtual = { showVirtual = it })
            }
            DataFreshness(s, edgeToEdge = false)
        }
    }
}

@Composable
private fun NetworkContent(network: NodeNetwork, showVirtual: Boolean, onShowVirtual: (Boolean) -> Unit) {
    val shown = remember(network, showVirtual) { network.visible(showVirtual) }
    val hidden = remember(network) { network.hiddenVirtualCount() }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxSize()) {
        item {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.network_show_virtual), style = MaterialTheme.typography.titleSmall)
                    Text(
                        pluralStringResource(R.plurals.network_virtual_hidden, hidden, hidden),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(checked = showVirtual, onCheckedChange = onShowVirtual)
            }
        }
        item {
            Section(stringResource(R.string.network_links), network.errors[NetworkSection.LINKS], shown.links.isEmpty()) {
                shown.links.forEachIndexed { i, link ->
                    if (i > 0) HorizontalDivider()
                    LinkRow(link)
                }
            }
        }
        item {
            Section(stringResource(R.string.network_addresses), network.errors[NetworkSection.ADDRESSES], shown.addresses.isEmpty()) {
                shown.addresses.forEach { AddressRow(it) }
            }
        }
        item {
            Section(stringResource(R.string.network_routes), network.errors[NetworkSection.ROUTES], shown.routes.isEmpty()) {
                shown.routes.forEachIndexed { i, route ->
                    if (i > 0) HorizontalDivider()
                    RouteRow(route)
                }
            }
        }
        item {
            Section(stringResource(R.string.network_resolvers), network.errors[NetworkSection.RESOLVERS], network.resolvers.isEmpty()) {
                network.resolvers.forEach { Mono(it) }
            }
        }
        item {
            Section(stringResource(R.string.network_time_servers), network.errors[NetworkSection.TIME_SERVERS], network.timeServers.isEmpty()) {
                network.timeServers.forEach { Mono(it) }
            }
        }
    }
}

/** A card with a title, the section's error if it failed, "none" when empty, else [content]. */
@Composable
private fun Section(title: String, error: String?, empty: Boolean, content: @Composable () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            SectionTitle(title)
            when {
                error != null -> Text(error, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                empty -> Text(stringResource(R.string.network_none), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
                else -> content()
            }
        }
    }
}

@Composable
private fun Mono(text: String) {
    Text(text, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
}

@Composable
private fun Muted(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@Composable
private fun LinkRow(link: LinkInfo) {
    val colors = LocalStatusColors.current
    Column(Modifier.padding(vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(link.name, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Bold, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
            StatusPill(link.state.ifEmpty { "?" }, if (link.isUp) colors.ok else colors.muted)
        }
        val kind = link.kind.ifEmpty { link.type }
        val speed = if (link.speedMbit > 0) stringResource(R.string.network_speed_mbit, link.speedMbit) else null
        Muted(listOfNotNull(kind.ifEmpty { null }, stringResource(R.string.network_mtu, link.mtu.toInt()), speed).joinToString("  ·  "))
        if (link.hardwareAddr.isNotEmpty()) {
            Text(link.hardwareAddr, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun AddressRow(address: AddressInfo) {
    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
        Text(address.address, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
        Muted(listOf(address.link, address.scope).filter { it.isNotEmpty() }.joinToString(" · "))
    }
}

@Composable
private fun RouteRow(route: RouteInfo) {
    val highlight = MaterialTheme.colorScheme.primary
    Column(Modifier.padding(vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                if (route.isDefault) stringResource(R.string.network_default_route) else route.destination,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                fontWeight = if (route.isDefault) FontWeight.Bold else null,
                color = if (route.isDefault) highlight else MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.weight(1f),
            )
            Muted(route.family)
        }
        val via = if (route.gateway.isNotEmpty()) stringResource(R.string.network_via, route.gateway) else null
        val dev = if (route.link.isNotEmpty()) stringResource(R.string.network_dev, route.link) else null
        Muted(listOfNotNull(via, dev, stringResource(R.string.network_metric, route.metric.toInt())).joinToString("  ·  "))
    }
}
