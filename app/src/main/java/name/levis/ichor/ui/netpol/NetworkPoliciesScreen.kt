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
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
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
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.ReadProgress
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
import name.levis.ichor.ui.components.CardListPadding
import name.levis.ichor.ui.components.RowCard
import name.levis.ichor.ui.components.RowPosition
import name.levis.ichor.ui.components.cardGroupTitle
import name.levis.ichor.ui.components.cardRows
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.components.pageContent

/** The ReadProgress op of the network policies read (Go KubeNetworkPolicies). */
const val NETPOL_PROGRESS_OP = "netpol"

class NetPoliciesViewModel(private val cilium: CiliumRepository, private val kube: KubeRepository) : LoadingViewModel<NetPolicyReport>() {
    override suspend fun fetch() = cilium.policies()
    override val progressReader: suspend () -> ReadProgress = { kube.readProgress(NETPOL_PROGRESS_OP) }
}

/** A network policies progress step by name: the policies of every CNI, the running pods, the namespaces. */
@Composable
private fun netpolStepLabel(id: String): String = when (id) {
    "policies" -> stringResource(R.string.read_progress_step_policies)
    "pods" -> stringResource(R.string.read_progress_step_pods)
    "namespaces" -> stringResource(R.string.read_progress_step_namespaces)
    else -> id
}

/**
 * The cluster's network policies, whatever the CNI: Kubernetes NetworkPolicies and, with
 * Cilium, CiliumNetworkPolicies and CiliumClusterwideNetworkPolicies, with Calico its
 * NetworkPolicies and GlobalNetworkPolicies. How isolated each namespace is on top, then
 * the policies by namespace; one opens its rules.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NetworkPoliciesScreen(
    onBack: () -> Unit,
    vm: NetPoliciesViewModel = viewModel(factory = factory { NetPoliciesViewModel(app.ciliumRepository, app.kubeRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val progress by vm.readProgress.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var namespace by rememberSaveable { mutableStateOf<String?>(null) }
    var query by rememberSaveable { mutableStateOf("") }
    var open by rememberSaveable { mutableStateOf<String?>(null) }
    val loaded = (state as? UiState.Loaded)?.data

    loaded?.policies?.firstOrNull { it.key == open }?.let { PolicyDetailSheet(it, onDismiss = { open = null }) }

    IchorScaffold(
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
        val modifier = Modifier.pageContent(padding)
        Loaded(state, vm::refresh, modifier, progress = progress, stepLabel = { netpolStepLabel(it) }) { data ->
            PolicyList(data, namespace, query, onNamespace = { namespace = it }, onQuery = { query = it }, onOpen = { open = it.key })
            
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
    val clusterWide = stringResource(R.string.netpol_cluster_wide)
    LazyColumn(Modifier.fillMaxSize(), contentPadding = CardListPadding) {
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
            cardGroupTitle("ns-$ns", ns.ifEmpty { clusterWide })
            cardRows(policies, key = { it.key }) { policy -> PolicyRow(policy, onClick = { onOpen(policy) }) }
        }
    }
}

/** How many policies of each kind, then a card of how isolated each namespace's pods are. */
@Composable
private fun Summary(report: NetPolicyReport, selected: String?) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        val kinds = remember(report) { report.policies.groupingBy { it.kindShort }.eachCount().toList().sortedBy { it.first } }
        Column(Modifier.padding(horizontal = 20.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            if (kinds.isNotEmpty()) MutedText(kinds.joinToString("  ·  ") { (kind, n) -> "$n $kind" })
            if (report.error.isNotEmpty()) InlineError(stringResource(R.string.netpol_read_error, report.error))
        }
        val rows = report.namespaces.filter { selected == null || it.namespace == selected }.filter { it.pods > 0 }
        if (rows.isNotEmpty()) {
            SectionTitle(stringResource(R.string.netpol_namespaces), Modifier.padding(horizontal = 20.dp))
            RowCard(RowPosition.ONLY) {
                Column(Modifier.padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    rows.forEach { NamespaceRow(it) }
                }
            }
        }
    }
}

@Composable
private fun NamespaceRow(row: NetPolicyNamespace) {
    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Column(Modifier.weight(1f)) {
            Text(row.namespace, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            MutedText(pluralStringResource(R.plurals.netpol_pods, row.pods, row.pods))
        }
        Tag(stringResource(R.string.netpol_ns_ingress, row.ingressIsolated, row.pods), isolation(row.ingressIsolated, row.pods).color())
        Tag(stringResource(R.string.netpol_ns_egress, row.egressIsolated, row.pods), isolation(row.egressIsolated, row.pods).color())
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
    Tag(label, color)
}
