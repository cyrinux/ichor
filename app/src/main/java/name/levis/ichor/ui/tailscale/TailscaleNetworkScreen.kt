package name.levis.ichor.ui.tailscale

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.NetMap
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.argocd.ArgoNetActions
import name.levis.ichor.ui.argocd.ArgoNetDetails
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.netmap.NetMapGraph
import name.levis.ichor.ui.components.netmap.NetMapLegend
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.dataservices.CauseBanner
import name.levis.ichor.ui.factory

/** One read of the network map of the address the Tailscale operator serves for [kind] [namespace]/[name]. */
class TailscaleNetworkViewModel(
    private val browser: KubeBrowserRepository,
    private val kind: String,
    private val namespace: String,
    private val name: String,
) : LoadingViewModel<NetMap>() {
    override suspend fun fetch() = browser.tailscaleNetwork(kind, namespace, name)
}

/**
 * The network map of one exposed address: the tailnet name (or, for an egress, the Service) as
 * the title, the likely root cause on top, then the shared graph from the name through each
 * proxy to the pods and nodes (or out to the tailnet target). A tap on a box lights its path
 * and shows its details, like the Argo CD app map; nothing is changed here.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TailscaleNetworkScreen(
    kind: String,
    namespace: String,
    name: String,
    title: String,
    onBack: () -> Unit,
    vm: TailscaleNetworkViewModel = viewModel(factory = factory { TailscaleNetworkViewModel(app.kubeBrowser, kind, namespace, name) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()

    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = { Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        val rest = Modifier.pageContent(padding).fillMaxSize()
        when (val s = state) {
            is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, rest)
            is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = rest) {
                LazyColumn(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    item { NetworkMapBody(s.data) }
                }
            }
            else -> LoadingBox(rest)
        }
    }
}

/** The map with the root cause above it and the details of a tapped box under it. */
@Composable
private fun NetworkMapBody(network: NetMap) {
    if (network.nodes.isEmpty()) {
        MutedText(stringResource(R.string.tailscale_net_empty))
        return
    }
    var selected by rememberSaveable { mutableStateOf<String?>(null) }
    val shown = selected?.let { network.node(it) }

    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        network.problem?.let { p ->
            CauseBanner(listOf(p.kind, p.namespace, p.name, p.detail).filter { it.isNotEmpty() }.joinToString(" · "))
        }
        NetMapGraph(network, selected = shown?.id, onSelect = { selected = if (selected == it) null else it })
        NetMapLegend()
        AnimatedVisibility(shown != null, enter = fadeIn() + expandVertically(), exit = fadeOut() + shrinkVertically()) {
            val node = shown ?: return@AnimatedVisibility
            ArgoNetDetails(node = node, podNode = null, actions = ArgoNetActions(), onClose = { selected = null })
        }
    }
}
