package name.levis.ichor.ui.netpol

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.CiliumRepository
import name.levis.ichor.model.NetPolicy
import name.levis.ichor.model.NetPolicyNamespace
import name.levis.ichor.model.NetPolicyReport
import name.levis.ichor.model.grouped
import name.levis.ichor.model.isolation
import name.levis.ichor.model.policyNamespaces
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.factory

class NetPoliciesViewModel(private val cilium: CiliumRepository) : LoadingViewModel<NetPolicyReport>() {
    override suspend fun fetch() = cilium.policies()
}

/**
 * The cluster's network policies, whatever the CNI: Kubernetes NetworkPolicies and, with
 * Cilium, CiliumNetworkPolicies and CiliumClusterwideNetworkPolicies. How isolated each
 * namespace is on top, then the policies by namespace; one opens its rules.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NetworkPoliciesScreen(
    onBack: () -> Unit,
    vm: NetPoliciesViewModel = viewModel(factory = factory { NetPoliciesViewModel(app.ciliumRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var namespace by rememberSaveable { mutableStateOf<String?>(null) }
    var query by rememberSaveable { mutableStateOf("") }
    var open by rememberSaveable { mutableStateOf<String?>(null) }
    val loaded = (state as? UiState.Loaded)?.data

    loaded?.policies?.firstOrNull { it.key == open }?.let { PolicyDetailSheet(it, onDismiss = { open = null }) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.netpol_title))
                        loaded?.let {
                            Text(
                                pluralStringResource(R.plurals.netpol_count, it.policies.size, it.policies.size),
                                style = MaterialTheme.typography.labelMedium,
                            )
                        }
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        val modifier = Modifier.padding(padding)
        when (val s = state) {
            UiState.Loading -> LoadingBox(modifier)
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
            is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = modifier.fillMaxSize()) {
                PolicyList(s.data, namespace, query, onNamespace = { namespace = it }, onQuery = { query = it }, onOpen = { open = it.key })
            }
        }
    }
}

@Composable
private fun PolicyList(
    report: NetPolicyReport,
    namespace: String?,
    query: String,
    onNamespace: (String?) -> Unit,
    onQuery: (String) -> Unit,
    onOpen: (NetPolicy) -> Unit,
) {
    val namespaces = remember(report) { report.policyNamespaces }
    val selected = namespace?.takeIf { it in namespaces }
    val groups = remember(report, selected, query) { report.grouped(selected, query) }
    LazyColumn(Modifier.fillMaxSize()) {
        item(key = "summary") { Summary(report, selected) }
        item(key = "filters") {
            Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                SearchField(query, onQuery, stringResource(R.string.netpol_search), Modifier.fillMaxWidth().padding(horizontal = 16.dp))
                LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    item {
                        FilterChip(selected = selected == null, onClick = { onNamespace(null) }, label = { Text(stringResource(R.string.workloads_all_namespaces)) })
                    }
                    items(namespaces, key = { it }) { ns ->
                        FilterChip(selected = selected == ns, onClick = { onNamespace(ns) }, label = { Text(ns, fontFamily = FontFamily.Monospace) })
                    }
                }
            }
            HorizontalDivider()
        }
        if (groups.isEmpty()) {
            item(key = "empty") { EmptyText(emptyOrNoMatch(query, R.string.netpol_empty, R.string.netpol_no_match)) }
        }
        groups.forEach { (ns, policies) ->
            item(key = "ns-$ns") {
                SectionTitle(ns.ifEmpty { stringResource(R.string.netpol_cluster_wide) }, Modifier.padding(horizontal = 16.dp))
            }
            items(policies, key = { it.key }) { policy ->
                PolicyRow(policy, onClick = { onOpen(policy) })
                HorizontalDivider()
            }
        }
    }
}

/** How many policies of each kind, then how isolated each namespace's pods are. */
@Composable
private fun Summary(report: NetPolicyReport, selected: String?) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        val kinds = remember(report) { report.policies.groupingBy { it.kindShort }.eachCount().toList().sortedBy { it.first } }
        if (kinds.isNotEmpty()) MutedText(kinds.joinToString("  ·  ") { (kind, n) -> "$n $kind" })
        if (report.error.isNotEmpty()) InlineError(stringResource(R.string.netpol_read_error, report.error))
        val rows = report.namespaces.filter { selected == null || it.namespace == selected }.filter { it.pods > 0 }
        if (rows.isNotEmpty()) SectionTitle(stringResource(R.string.netpol_namespaces))
        rows.forEach { NamespaceRow(it) }
    }
}

@Composable
private fun NamespaceRow(row: NetPolicyNamespace) {
    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Column(Modifier.weight(1f)) {
            Text(row.namespace, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            MutedText(pluralStringResource(R.plurals.netpol_pods, row.pods, row.pods))
        }
        TagBadge(stringResource(R.string.netpol_ns_ingress, row.ingressIsolated, row.pods), isolation(row.ingressIsolated, row.pods).color())
        TagBadge(stringResource(R.string.netpol_ns_egress, row.egressIsolated, row.pods), isolation(row.egressIsolated, row.pods).color())
    }
}

@Composable
private fun PolicyRow(policy: NetPolicy, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        KindBadge(policy)
        Column(Modifier.weight(1f)) {
            Text(policy.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            MutedText(subjectText(policy), maxLines = 1, overflow = TextOverflow.Ellipsis)
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), modifier = Modifier.padding(top = 2.dp)) {
                DirectionBadge(stringResource(R.string.netpol_ingress), policy.ingress)
                DirectionBadge(stringResource(R.string.netpol_egress), policy.egress)
            }
        }
        if (!policy.nodes) MutedText(pluralStringResource(R.plurals.netpol_pods, policy.podCount, policy.podCount))
    }
}

/** A direction the policy isolates stands out; one it leaves open is muted. */
@Composable
private fun DirectionBadge(label: String, isolated: Boolean) {
    val color = if (isolated) MaterialTheme.colorScheme.tertiary else MaterialTheme.colorScheme.outline
    TagBadge(label, color)
}
