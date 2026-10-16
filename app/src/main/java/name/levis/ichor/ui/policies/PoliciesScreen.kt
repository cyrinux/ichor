package name.levis.ichor.ui.policies

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
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.PolicyRepository
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KyvernoPolicy
import name.levis.ichor.model.POLICY_ENGINE_GATEKEEPER
import name.levis.ichor.model.PolicyEngine
import name.levis.ichor.model.PolicyReport
import name.levis.ichor.model.WEBHOOK_FAIL
import name.levis.ichor.model.WEBHOOK_IGNORE
import name.levis.ichor.model.bothEngines
import name.levis.ichor.model.filtered
import name.levis.ichor.model.policyNamespaces
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

class PoliciesViewModel(private val policies: PolicyRepository) : LoadingViewModel<PolicyReport>() {
    override suspend fun fetch() = policies.policies()
}

private const val KYVERNO_SITE = "https://kyverno.io"

/**
 * Kyverno's admission policies and Gatekeeper's constraints: whether each engine's controllers and
 * webhook are up, then every policy (not ready first, then Enforce before Audit) with its findings;
 * one opens its rules and the resources it fails, each of a known kind opening its object screen
 * ([onObject]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PoliciesScreen(
    onBack: () -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    vm: PoliciesViewModel = viewModel(factory = factory { PoliciesViewModel(app.policyRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var namespace by rememberSaveable { mutableStateOf<String?>(null) }
    var query by rememberSaveable { mutableStateOf("") }
    var open by rememberSaveable { mutableStateOf<String?>(null) }
    val loaded = (state as? UiState.Loaded)?.data

    loaded?.policies?.firstOrNull { it.key == open }?.let {
        PolicyDetailSheet(it, onDismiss = { open = null }, onObject = { ref -> open = null; onObject(ref) })
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.kpol_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Loaded(state, vm::refresh, Modifier.pageContent(padding)) { report ->
            if (report.installed) {
                PolicyList(report, namespace, query, onNamespace = { namespace = it }, onQuery = { query = it }, onOpen = { open = it.key })
            } else {
                NotInstalled(report.error)
            }
        }
    }
}

/** No Kyverno here: what it would do, and where to read about it; not an error. */
@Composable
private fun NotInstalled(error: String) {
    val uriHandler = LocalUriHandler.current
    Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(R.string.kpol_not_installed), style = MaterialTheme.typography.titleMedium)
        MutedText(stringResource(R.string.kpol_not_installed_hint))
        TextButton(onClick = { runCatching { uriHandler.openUri(KYVERNO_SITE) } }) { Text("kyverno.io") }
        if (error.isNotEmpty()) InlineError(stringResource(R.string.kpol_read_error, error))
    }
}

@Composable
private fun PolicyList(
    report: PolicyReport,
    namespace: String?,
    query: String,
    onNamespace: (String?) -> Unit,
    onQuery: (String) -> Unit,
    onOpen: (KyvernoPolicy) -> Unit,
) {
    val namespaces = remember(report) { report.policyNamespaces }
    val selected = namespace?.takeIf { it in namespaces }
    val rows = remember(report, selected, query) { report.filtered(selected, query) }
    LazyColumn(Modifier.fillMaxSize()) {
        items(report.engines, key = { "engine/" + it.name }) { EngineCard(report, it) }
        item(key = "filters") {
            Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                SearchField(query, onQuery, stringResource(R.string.kpol_search), Modifier.fillMaxWidth().padding(horizontal = 16.dp))
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
                report.namespaces.firstOrNull { it.namespace == selected }?.let {
                    ResultCounts(it.fail, it.warn, it.pass, Modifier.padding(horizontal = 16.dp))
                }
            }
            HorizontalDivider()
        }
        if (rows.isEmpty()) {
            val empty = if (report.kyverno) R.string.kpol_empty else R.string.kpol_empty_gatekeeper
            item(key = "empty") { EmptyText(emptyOrNoMatch(query, empty, R.string.kpol_no_match)) }
        }
        items(rows, key = { it.key }) { policy ->
            PolicyRow(policy, selected, showEngine = report.bothEngines, onClick = { onOpen(policy) })
            HorizontalDivider()
        }
        if (report.reportsTruncated) {
            item(key = "truncated") {
                MutedText(stringResource(R.string.kpol_reports_truncated, report.reportsRead.toString()), Modifier.padding(16.dp))
            }
        }
    }
}

/** One engine: its controllers as ready/desired chips, what its webhook does while it is down, and the reads that failed. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun EngineCard(report: PolicyReport, engine: PolicyEngine) {
    val colors = LocalStatusColors.current
    val gatekeeper = engine.name == POLICY_ENGINE_GATEKEEPER
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        SectionTitle(stringResource(if (gatekeeper) R.string.kpol_engine_gatekeeper else R.string.kpol_engine))
        val details = if (gatekeeper) {
            listOf(engine.namespace)
        } else {
            listOf(engine.namespace, report.version, stringResource(R.string.kpol_reports_read, report.reportsRead.toString()))
        }
        MutedText(details.filter { it.isNotEmpty() }.joinToString("  ·  "))
        if (engine.controllers.isNotEmpty()) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                engine.controllers.forEach { c ->
                    val description = stringResource(R.string.kpol_controller_ready, c.name, c.ready.toString(), c.desired.toString())
                    TagBadge(
                        "${c.name} ${c.ready}/${c.desired}",
                        if (c.healthy) colors.ok else colors.bad,
                        Modifier.semantics { contentDescription = description },
                        mono = true,
                    )
                }
            }
        }
        WebhookLine(engine)
        if (gatekeeper) {
            if (report.gatekeeperError.isNotEmpty()) InlineError(stringResource(R.string.kpol_read_error, report.gatekeeperError))
        } else {
            if (report.reportsError.isNotEmpty()) InlineError(stringResource(R.string.kpol_reports_unavailable, report.reportsError))
            if (report.error.isNotEmpty()) InlineError(stringResource(R.string.kpol_read_error, report.error))
        }
    }
}

/** Fail refuses requests while the engine is down (an outage risk), Ignore lets them through unchecked. */
@Composable
private fun WebhookLine(engine: PolicyEngine) {
    val colors = LocalStatusColors.current
    val webhook = engine.webhook
    val gatekeeper = engine.name == POLICY_ENGINE_GATEKEEPER
    val fail = if (gatekeeper) R.string.kpol_webhook_fail_gatekeeper else R.string.kpol_webhook_fail
    val ignore = if (gatekeeper) R.string.kpol_webhook_ignore_gatekeeper else R.string.kpol_webhook_ignore
    val (text, color) = when {
        !webhook.present -> stringResource(R.string.kpol_webhook_missing) to colors.bad
        webhook.failurePolicy == WEBHOOK_FAIL -> stringResource(fail) to colors.warn
        webhook.failurePolicy == WEBHOOK_IGNORE -> stringResource(ignore) to MaterialTheme.colorScheme.onSurfaceVariant
        else -> return
    }
    Text(text, style = MaterialTheme.typography.bodySmall, color = color)
}

/** Name and mode (and its engine when both run), its scope, not-ready reason and counts (in [namespace] when one is picked). */
@Composable
private fun PolicyRow(policy: KyvernoPolicy, namespace: String?, showEngine: Boolean, onClick: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                policy.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (showEngine) EngineBadge(policy)
            ActionBadge(policy.action)
        }
        MutedText(policyScopeText(policy), maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (!policy.ready) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                NotReadyBadge(policy)
                if (policy.readyReason.isNotEmpty()) MutedText(policy.readyReason, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        val counts = policy.counts(namespace)
        ResultCounts(counts.fail, counts.warn, counts.pass, Modifier.padding(top = 2.dp))
    }
}
