package name.levis.ichor.ui.loadbalancing

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.KubeLoadBalancing
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory

/** The load balancer controllers of the cluster, read again on refresh. */
class LoadBalancingViewModel(private val browser: KubeBrowserRepository) : LoadingViewModel<KubeLoadBalancing>() {
    override suspend fun fetch() = browser.loadBalancing()
}

/**
 * What gives LoadBalancer Services their address, problems first: each Service that waits,
 * with why; then the pools and what is left of them, who advertises them, MetalLB's BGP
 * peers, kube-vip's ranges and leases, and every LoadBalancer Service. A Service opens its
 * summary ([onObject]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LoadBalancingScreen(
    onBack: () -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    vm: LoadBalancingViewModel = viewModel(factory = factory { LoadBalancingViewModel(app.kubeBrowser) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = { Text(stringResource(R.string.lb_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        val rest = Modifier.pageContent(padding).fillMaxSize()
        when (val s = state) {
            is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, rest)
            is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = rest) {
                if (s.data.empty) {
                    EmptyText(stringResource(R.string.lb_empty))
                } else {
                    LoadBalancingList(s.data, onService = { ns, name -> onObject(KubeObjectRef.service(ns, name)) })
                }
            }
            else -> LoadingBox(rest)
        }
    }
}

@Composable
private fun LoadBalancingList(lb: KubeLoadBalancing, onService: (namespace: String, name: String) -> Unit) {
    val pad = Modifier.padding(horizontal = 16.dp)
    LazyColumn(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (lb.partialAccess) item { MutedText(stringResource(R.string.lb_partial), modifier = pad) }
        if (lb.controllers.isEmpty()) item { MutedText(stringResource(R.string.kube_services_hint_none), modifier = pad) }
        items(lb.configErrors) { ConfigErrorCard(it, pad) }
        items(lb.pending, key = { "waiting-" + it.key }) { WaitingCard(it, pad) { onService(it.namespace, it.name) } }
        section(R.string.lb_pools, lb.pools.isNotEmpty()) { items(lb.pools, key = { "pool-" + it.controller + it.name }) { PoolRow(it, pad) } }
        section(R.string.lb_advertisements, lb.advertisements.isNotEmpty()) {
            items(lb.advertisements, key = { "adv-" + it.controller + it.kind + it.name }) { AdvertisementRow(it, pad) }
        }
        section(R.string.lb_bgp_peers, lb.bgpPeers.isNotEmpty()) { items(lb.bgpPeers, key = { "peer-" + it.name }) { PeerRow(it, pad) } }
        lb.kubeVip?.let { vip -> section(R.string.lb_kubevip, true) { item { KubeVipRows(vip, pad) } } }
        section(R.string.lb_services, lb.services.isNotEmpty()) {
            items(lb.services, key = { "svc-" + it.key }) { ServiceRow(it, pad) { onService(it.namespace, it.name) } }
        }
    }
}

private fun LazyListScope.section(title: Int, shown: Boolean, content: LazyListScope.() -> Unit) {
    if (!shown) return
    item(key = "title-$title") { SectionTitle(stringResource(title), Modifier.padding(horizontal = 16.dp)) }
    content()
}
