package name.levis.ichor.ui.autoscaling

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.AutoscalingRepository
import name.levis.ichor.model.AutoscalingProblem
import name.levis.ichor.model.AutoscalingReport
import name.levis.ichor.model.HpaRow
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.NodeClaimRow
import name.levis.ichor.model.NodePoolRow
import name.levis.ichor.model.ScaledJobRow
import name.levis.ichor.model.ScaledObjectRow
import name.levis.ichor.model.VpaRow
import name.levis.ichor.model.autoscalingNamespaces
import name.levis.ichor.model.filtered
import name.levis.ichor.model.AUTOSCALE_NOT_READY
import name.levis.ichor.model.AUTOSCALE_OK
import name.levis.ichor.model.hpaBoundsText
import name.levis.ichor.model.problems
import name.levis.ichor.model.vpaChangeText
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors

class AutoscalingViewModel(private val autoscaling: AutoscalingRepository) : LoadingViewModel<AutoscalingReport>() {
    override suspend fun fetch() = autoscaling.autoscaling()
}

/** What a tap opens: one row of one group, by its key. */
private sealed interface Open {
    data class Hpa(val row: HpaRow) : Open
    data class ScaledObject(val row: ScaledObjectRow) : Open
    data class ScaledJob(val row: ScaledJobRow) : Open
    data class Vpa(val row: VpaRow) : Open
    data class Pool(val row: NodePoolRow, val claims: List<NodeClaimRow>) : Open
}

/**
 * What scales the cluster: the rows that need attention first, then every group's rows with a
 * detail sheet each ([onObject] opens a row's workload in the Kubernetes browser).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AutoscalingScreen(
    onBack: () -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    vm: AutoscalingViewModel = viewModel(factory = factory { AutoscalingViewModel(app.autoscalingRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var namespace by rememberSaveable { mutableStateOf<String?>(null) }
    var query by rememberSaveable { mutableStateOf("") }
    var open by remember { mutableStateOf<Open?>(null) }
    val loaded = (state as? UiState.Loaded)?.data

    loaded?.let { report ->        when (val o = open) {
            is Open.Hpa -> AutoscaleSheet(o.row, onDismiss = { open = null }, onObject = { open = null; onObject(it) })
            is Open.ScaledObject -> ScaledObjectSheet(o.row, onDismiss = { open = null }, onObject = { open = null; onObject(it) })
            is Open.ScaledJob -> ScaledJobSheet(o.row, onDismiss = { open = null })
            is Open.Vpa -> VpaSheet(o.row, onDismiss = { open = null }, onObject = { open = null; onObject(it) })
            is Open.Pool -> NodePoolSheet(o.row, o.claims, onDismiss = { open = null }, onObject = { open = null; onObject(it) })
            null -> {}
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.autoscaling_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Loaded(state, vm::refresh, Modifier.pageContent(padding)) { report ->
            AutoscalingList(report, namespace, query, onNamespace = { namespace = it }, onQuery = { query = it }, onOpen = { open = it })
        }
    }
}

@Composable
private fun AutoscalingList(
    report: AutoscalingReport,
    namespace: String?,
    query: String,
    onNamespace: (String?) -> Unit,
    onQuery: (String) -> Unit,
    onOpen: (Open?) -> Unit,
) {
    val namespaces = remember(report) { report.autoscalingNamespaces }
    val selected = namespace?.takeIf { it in namespaces }
    val view = remember(report, selected, query) { report.filtered(selected, query) }
    val attention = remember(view) { view.problems() }
    val claimsOf = remember(view) { view.karpenter.nodeClaims.groupBy { it.pool } }

    fun openHpa(row: HpaRow) = onOpen(Open.Hpa(row))
    fun openSo(row: ScaledObjectRow) = onOpen(Open.ScaledObject(row))
    fun openSj(row: ScaledJobRow) = onOpen(Open.ScaledJob(row))
    fun openVpa(row: VpaRow) = onOpen(Open.Vpa(row))
    fun openPool(row: NodePoolRow) = onOpen(Open.Pool(row, claimsOf[row.name].orEmpty()))

    LazyColumn(Modifier.fillMaxSize()) {
        item(key = "filters") {
            Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                SearchField(query, onQuery, stringResource(R.string.autoscaling_search), Modifier.fillMaxWidth().padding(horizontal = 16.dp))
                if (namespaces.isNotEmpty()) {
                    LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        item {
                            FilterChip(selected = selected == null, onClick = { onNamespace(null) }, label = { Text(stringResource(R.string.workloads_all_namespaces)) })
                        }
                        items(namespaces, key = { it }) { ns ->
                            FilterChip(selected = selected == ns, onClick = { onNamespace(ns) }, label = { Text(ns, fontFamily = FontFamily.Monospace) })
                        }
                    }
                }
            }
            HorizontalDivider()
        }

        item(key = "attention-title") { SectionTitle(stringResource(R.string.autoscaling_needs_attention), Modifier.padding(horizontal = 16.dp)) }
        if (attention.isEmpty()) {
            item(key = "attention-empty") { MutedText(stringResource(R.string.autoscaling_all_ok), Modifier.padding(horizontal = 16.dp)) }
        }
        items(attention, key = { "p-" + it.group + "/" + it.key }) { problem ->
            ProblemRow(problem) {
                when (problem.group) {
                    "hpa" -> view.hpa.items.firstOrNull { it.key == problem.key }?.let(::openHpa)
                    "keda" -> view.keda.scaledObjects.firstOrNull { it.key == problem.key }?.let(::openSo)
                        ?: view.keda.scaledJobs.firstOrNull { it.key == problem.key }?.let(::openSj)
                    "vpa" -> view.vpa.items.firstOrNull { it.key == problem.key }?.let(::openVpa)
                    else -> view.karpenter.nodePools.firstOrNull { it.key == problem.key }?.let(::openPool)
                        ?: view.karpenter.nodeClaims.firstOrNull { it.key == problem.key }?.let { claim ->
                            view.karpenter.nodePools.firstOrNull { it.name == claim.pool }?.let(::openPool)
                        }
                }
            }
            HorizontalDivider()
        }

        item(key = "h-hpa") { GroupHeader(stringResource(R.string.autoscaling_section_hpa), view.hpa.installed, view.hpa.error, view.hpa.truncated) }
        if (view.hpa.installed) {
            items(view.hpa.items, key = { "hpa-" + it.key }) { row ->
                HpaRowItem(row, onClick = { openHpa(row) })
                HorizontalDivider()
            }
        }
        item(key = "h-keda") { GroupHeader(stringResource(R.string.autoscaling_section_keda), view.keda.installed, view.keda.error, view.keda.truncated) }
        if (view.keda.installed) {
            items(view.keda.scaledObjects, key = { "so-" + it.key }) { row ->
                ScaledObjectRowItem(row, onClick = { openSo(row) })
                HorizontalDivider()
            }
            items(view.keda.scaledJobs, key = { "sj-" + it.key }) { row ->
                ScaledJobRowItem(row, onClick = { openSj(row) })
                HorizontalDivider()
            }
        }
        item(key = "h-vpa") { GroupHeader(stringResource(R.string.autoscaling_section_vpa), view.vpa.installed, view.vpa.error, view.vpa.truncated) }
        if (view.vpa.installed) {
            items(view.vpa.items, key = { "vpa-" + it.key }) { row ->
                VpaRowItem(row, onClick = { openVpa(row) })
                HorizontalDivider()
            }
        }
        item(key = "h-karpenter") { GroupHeader(stringResource(R.string.autoscaling_section_karpenter), view.karpenter.installed, view.karpenter.error, view.karpenter.truncated) }
        if (view.karpenter.installed) {
            items(view.karpenter.nodePools, key = { "pool-" + it.key }) { row ->
                NodePoolRowItem(row, claimsOf[row.name].orEmpty().size, onClick = { openPool(row) })
                HorizontalDivider()
            }
        }

        if (listOf(view.hpa.items, view.keda.scaledObjects, view.keda.scaledJobs, view.vpa.items, view.karpenter.nodePools).all { it.isEmpty() }) {
            item(key = "empty") { EmptyText(emptyOrNoMatch(query, R.string.autoscaling_empty, R.string.autoscaling_no_match)) }
        }
    }
}

/** One group's header: its title, or the one line a missing or failed group gets. */
@Composable
private fun GroupHeader(title: String, installed: Boolean, error: String, truncated: Boolean) {
    Column(Modifier.padding(top = 12.dp)) {
        SectionTitle(title, Modifier.padding(horizontal = 16.dp))
        if (!installed) {
            MutedText(stringResource(R.string.autoscaling_not_installed, title), Modifier.padding(horizontal = 16.dp))
        } else {
            if (error.isNotEmpty()) InlineError(stringResource(R.string.autoscaling_group_error, error), Modifier.padding(horizontal = 16.dp))
            if (truncated) MutedText(stringResource(R.string.autoscaling_truncated), Modifier.padding(horizontal = 16.dp))
        }
    }
}

/** One "Needs attention" row: the group it came from, its name and its state. */
@Composable
private fun ProblemRow(problem: AutoscalingProblem, onClick: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                problem.label,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            AutoscaleStateBadge(problem.state)
        }
        MutedText(groupLabel(problem.group), maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (problem.message.isNotEmpty()) {
            Text(problem.message, style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
private fun groupLabel(group: String): String = stringResource(
    when (group) {
        "hpa" -> R.string.autoscaling_section_hpa
        "keda" -> R.string.autoscaling_section_keda
        "vpa" -> R.string.autoscaling_section_vpa
        else -> R.string.autoscaling_section_karpenter
    },
)

/** Name, bounds "6 of 2–6", current/desired and the state. */
@Composable
private fun HpaRowItem(row: HpaRow, onClick: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                row.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            AutoscaleStateBadge(row.state)
        }
        MutedText(listOf(row.namespace, targetText(row.target), hpaBoundsText(row.current, row.min, row.max)).joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (row.managedBy.isNotEmpty()) {
            MutedText(stringResource(R.string.autoscaling_managed_by, row.managedBy, row.scaledObject), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** Name, its target and triggers, the state; current/desired when the HPA answers. */
@Composable
private fun ScaledObjectRowItem(row: ScaledObjectRow, onClick: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                row.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            AutoscaleStateBadge(row.state)
        }
        MutedText(listOf(row.namespace, targetText(row.target)).joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (row.triggers.isNotEmpty()) {
            MutedText(row.triggers.joinToString("  ·  ") { it.label }, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
private fun ScaledJobRowItem(row: ScaledJobRow, onClick: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                row.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            AutoscaleStateBadge(row.state)
        }
        MutedText(listOf(row.namespace, row.triggers.firstOrNull()?.label.orEmpty()).filter { it.isNotEmpty() }.joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Name, the target, and the first container's request → recommendation. */
@Composable
private fun VpaRowItem(row: VpaRow, onClick: () -> Unit) {
    val first = row.containers.firstOrNull()
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                row.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            AutoscaleStateBadge(row.state)
        }
        MutedText(listOf(row.namespace, targetText(row.target), row.updateMode).filter { it.isNotEmpty() }.joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (first != null) {
            MutedText(
                stringResource(R.string.autoscaling_vpa_change, first.name, vpaChangeText(first.cpuRequest, first.cpuTarget)),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/** Name, usage against its limits, and how many claims run in it. */
@Composable
private fun NodePoolRowItem(row: NodePoolRow, claimCount: Int, onClick: () -> Unit) {
    val colors = LocalStatusColors.current
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                row.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (row.atLimit) {
                TagBadge(stringResource(R.string.autoscaling_at_limit), colors.bad, Modifier)
            } else {
                AutoscaleStateBadge(if (row.ready) AUTOSCALE_OK else AUTOSCALE_NOT_READY)
            }
        }
        MutedText(
            listOf(
                stringResource(R.string.autoscaling_pool_usage, row.resources.cpu, row.limits.cpu),
                stringResource(R.string.autoscaling_pool_nodes, row.resources.nodes.toString()),
                stringResource(R.string.autoscaling_pool_claims, claimCount.toString()),
            ).joinToString("  ·  "),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
