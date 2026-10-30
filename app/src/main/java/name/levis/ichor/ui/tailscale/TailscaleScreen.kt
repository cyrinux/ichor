package name.levis.ichor.ui.tailscale

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.Tailscale
import name.levis.ichor.model.TailscaleKind
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

/** One read of the cluster's Tailscale operator: connectors, proxy groups, proxy classes, DNS. */
class TailscaleViewModel(private val browser: KubeBrowserRepository) : LoadingViewModel<Tailscale>() {
    override suspend fun fetch() = browser.tailscale()
}

/**
 * Tailscale (os:admin): the operator, its connectors (subnet routers, exit nodes, app
 * connectors), proxy groups, proxy classes and the tailnet nameserver, problems first in each
 * section. A tap opens the object's summary ([onObject]); nothing is changed here.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TailscaleScreen(
    onBack: () -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    vm: TailscaleViewModel = viewModel(factory = factory { TailscaleViewModel(app.kubeBrowser) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()

    IchorScaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.tailscale_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        val rest = Modifier.pageContent(padding).fillMaxSize()
        when (val s = state) {
            is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, rest)
            is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = rest) {
                TailscaleList(s.data, onObject)
            }
            else -> LoadingBox(rest)
        }
    }
}

@Composable
private fun TailscaleList(ts: Tailscale, onObject: (KubeObjectRef) -> Unit) {
    LazyColumn(Modifier.fillMaxSize()) {
        item(key = "header") { TailscaleHeader(ts) }
        if (!ts.installed || ts.isEmpty) {
            item(key = "empty") { EmptyText(stringResource(R.string.tailscale_empty)) }
            return@LazyColumn
        }
        section("connectors", R.string.tailscale_connectors, ts.connectors) { c ->
            ConnectorRow(c, onClick = { onObject(ts.ref(TailscaleKind.CONNECTOR, c.name)) })
        }
        section("proxy-groups", R.string.tailscale_proxy_groups, ts.proxyGroups) { g ->
            ProxyGroupRow(g, onClick = { onObject(ts.ref(TailscaleKind.PROXY_GROUP, g.name)) })
        }
        section("proxy-classes", R.string.tailscale_proxy_classes, ts.proxyClasses) { pc ->
            ProxyClassRow(pc, onClick = { onObject(ts.ref(TailscaleKind.PROXY_CLASS, pc.name)) })
        }
        section("dns", R.string.tailscale_dns, listOfNotNull(ts.dnsConfig)) { d ->
            DNSConfigRow(d, onClick = { onObject(ts.ref(TailscaleKind.DNS_CONFIG, d.name)) })
        }
    }
}

/** A titled section of [rows] (problems first, as the Go core sorted them); nothing when empty. */
private fun <T> LazyListScope.section(key: String, title: Int, rows: List<T>, row: @Composable (T) -> Unit) {
    if (rows.isEmpty()) return
    item(key = "$key-title") { SectionTitle(stringResource(title), Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
    items(rows.size, key = { "$key:$it" }) { i ->
        row(rows[i])
        HorizontalDivider()
    }
}

/** The operator (ready/desired, image), the served version and the counts. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun TailscaleHeader(ts: Tailscale) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (ts.error.isNotEmpty()) MutedText(stringResource(R.string.tailscale_partial, ts.error))
        val operator = ts.operator
        if (operator != null) {
            OperatorLine(operator)
        } else if (ts.installed) {
            MutedText(stringResource(R.string.tailscale_operator_missing))
        }
        if (ts.version.isNotEmpty()) MutedText(stringResource(R.string.tailscale_api_version, ts.version))
        if (ts.installed && !ts.isEmpty) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                CountText(stringResource(R.string.tailscale_count_critical, ts.counts.critical), critical = ts.counts.critical > 0)
                CountText(stringResource(R.string.tailscale_count_warning, ts.counts.warning), critical = false)
                CountText(stringResource(R.string.tailscale_count_ok, ts.counts.ok), critical = false)
            }
        }
        if (ts.truncated) MutedText(stringResource(R.string.tailscale_truncated))
    }
}
