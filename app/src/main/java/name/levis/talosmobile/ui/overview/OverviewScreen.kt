package name.levis.talosmobile.ui.overview

import androidx.annotation.PluralsRes
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import name.levis.talosmobile.R
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
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
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.compose.ui.platform.LocalContext
import name.levis.talosmobile.TalosApp
import androidx.compose.material3.TextButton
import name.levis.talosmobile.data.ConfigRepository
import name.levis.talosmobile.data.SPONSOR_URL
import name.levis.talosmobile.ui.settings.openUrl
import name.levis.talosmobile.data.activeSummary
import name.levis.talosmobile.model.Feature
import name.levis.talosmobile.model.accessLabel
import name.levis.talosmobile.model.allows
import name.levis.talosmobile.update.UpdateState
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.data.OVERVIEW
import name.levis.talosmobile.model.ClusterOverview
import name.levis.talosmobile.model.ClusterTime
import name.levis.talosmobile.model.ContextSummary
import name.levis.talosmobile.monitor.CERT_WARN_DAYS
import name.levis.talosmobile.util.daysUntil
import name.levis.talosmobile.model.NodeHealth
import name.levis.talosmobile.model.NodeOverview
import name.levis.talosmobile.model.health
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.DataFreshness
import name.levis.talosmobile.ui.components.ErrorBox
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.components.NodeHealthPill
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalStatusColors

class OverviewViewModel(
    val talos: TalosRepository,
    val configs: ConfigRepository,
) : LoadingViewModel<ClusterOverview>() {
    override fun cached(): TalosRepository.Timed<ClusterOverview>? = talos.cached(OVERVIEW)
    override suspend fun fetch() = talos.overview()
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OverviewScreen(
    onNode: (NodeOverview) -> Unit,
    onNodeAction: (NodeOverview, NodeAction) -> Unit,
    onEtcd: () -> Unit,
    onKubeSpan: () -> Unit,
    onHealth: () -> Unit,
    onEvents: () -> Unit,
    onSettings: () -> Unit,
    onIssueConfig: () -> Unit,
    onUpgrade: (NodeOverview, String) -> Unit,
    vm: OverviewViewModel = viewModel(factory = factory { OverviewViewModel(app.talosRepository, app.configRepository) }),
    timeVm: ClusterTimeViewModel = viewModel(factory = factory { ClusterTimeViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val timeState by timeVm.state.collectAsStateWithLifecycle()
    val config by vm.configs.config.collectAsStateWithLifecycle()
    val invalidations by vm.talos.invalidations.collectAsStateWithLifecycle()

    // Reload whenever the active context changes or cached data was dropped (including first composition).
    LaunchedEffect(config?.activeContext, invalidations) {
        vm.refresh(reset = true)
        timeVm.refresh(reset = true)
    }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.overview_title))
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            ScreenshotModeIcon()
                            config?.let { stored ->
                                val access = stored.activeSummary?.accessLabel?.let { stringResource(it) }
                                Text(
                                    listOfNotNull(stored.activeContext, access).joinToString(" · "),
                                    style = MaterialTheme.typography.labelMedium,
                                    color = MaterialTheme.colorScheme.primary,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.weight(1f, fill = false).padding(end = 6.dp),
                                )
                            }
                        }
                    }
                },
                actions = {
                    // Only offered when the config's role can run it.
                    if (config?.activeSummary?.allows(Feature.HEALTH) == true) {
                        IconButton(onClick = onHealth) { Icon(Icons.Outlined.Favorite, stringResource(R.string.overview_action_health)) }
                    }
                    IconButton(onClick = onEvents) { Icon(Icons.Outlined.Timeline, stringResource(R.string.overview_action_events)) }
                    IconButton(onClick = onKubeSpan) { Icon(Icons.Outlined.Hub, "KubeSpan") }
                    IconButton(onClick = onEtcd) { Icon(Icons.Outlined.Storage, "etcd") }
                    IconButton(onClick = onSettings) { Icon(Icons.Outlined.Settings, stringResource(R.string.overview_action_settings)) }
                },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.padding(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.padding(padding))
            is UiState.Loaded -> PullToRefreshBox(
                isRefreshing = s.refreshing,
                onRefresh = {
                    vm.refresh()
                    timeVm.refresh()
                },
                modifier = Modifier.padding(padding).fillMaxSize(),
            ) {
                NodeList(
                    overview = s.data,
                    time = timeState,
                    certificate = config?.activeSummary,
                    onIssueConfig = onIssueConfig,
                    onNode = onNode,
                    onSettings = onSettings,
                    onNodeAction = onNodeAction,
                    canPower = config?.activeSummary?.allows(Feature.POWER) == true,
                    canShell = config?.activeSummary?.allows(Feature.DEBUG_SHELL) == true,
                    canUpgrade = config?.activeSummary?.allows(Feature.UPGRADE) == true,
                    onUpgrade = onUpgrade,
                )
            }
        }
    }
}

@Composable
private fun NodeList(
    overview: ClusterOverview,
    time: UiState<ClusterTime>,
    certificate: ContextSummary?,
    onIssueConfig: () -> Unit,
    onNode: (NodeOverview) -> Unit,
    onSettings: () -> Unit,
    onNodeAction: (NodeOverview, NodeAction) -> Unit,
    canPower: Boolean,
    canShell: Boolean,
    canUpgrade: Boolean,
    onUpgrade: (NodeOverview, String) -> Unit,
) {
    var sheetFor by remember { mutableStateOf<NodeOverview?>(null) }
    sheetFor?.let { node ->
        NodeActionsSheet(
            node = node,
            canPower = canPower,
            canShell = canShell,
            onAction = { onNodeAction(node, it) },
            onDismiss = { sheetFor = null },
        )
    }
    val nodes = overview.nodes.sortedWith(compareBy({ it.role != "controlplane" }, { it.hostname }))
    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        item { UpdateBanner(onClick = onSettings) }
        item { SupportCard() }
        certificate?.let { item { CertificateBanner(it, onIssueConfig) } }
        item { TalosUpdateBanner(overview.nodes, canUpgrade, onUpgrade) }
        item { Summary(overview.nodes) }
        items(nodes, key = { it.node }) { node ->
            SwipeableNode(node, onLive = { onNodeAction(node, NodeAction.LIVE) }, onMore = { sheetFor = node }) {
                NodeCard(node, onClick = { onNode(node) }, onLongClick = { sheetFor = node })
            }
        }
        // After the nodes: they come first, the clocks are a secondary check.
        item { TimeDriftCard(time, overview.nodes.associate { it.node to it.hostname }) }
    }
}

@Composable
private fun Summary(nodes: List<NodeOverview>) {
    val colors = LocalStatusColors.current
    val counts = nodes.groupingBy { it.health }.eachCount()
    Row(Modifier.fillMaxWidth().padding(bottom = 6.dp), horizontalArrangement = Arrangement.spacedBy(20.dp)) {
        SummaryCount(counts[NodeHealth.READY] ?: 0, R.plurals.overview_summary_ready, colors.ok)
        SummaryCount(counts[NodeHealth.NOT_READY] ?: 0, R.plurals.overview_summary_not_ready, colors.warn)
        SummaryCount(counts[NodeHealth.UNREACHABLE] ?: 0, R.plurals.overview_summary_unreachable, colors.bad)
    }
}

@Composable
private fun SummaryCount(count: Int, @PluralsRes label: Int, color: androidx.compose.ui.graphics.Color) {
    Column {
        Text(
            "$count",
            style = MaterialTheme.typography.headlineMedium,
            color = if (count > 0) color else MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(pluralStringResource(label, count), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun NodeCard(node: NodeOverview, onClick: () -> Unit, onLongClick: () -> Unit) {
    Card(
        Modifier.fillMaxWidth().combinedClickable(
            onClick = { if (node.reachable) onClick() },
            onLongClick = onLongClick,
            onLongClickLabel = stringResource(R.string.overview_node_actions),
        ),
    ) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(node.hostname, style = MaterialTheme.typography.titleMedium)
                    Text(
                        node.node,
                        style = MaterialTheme.typography.bodySmall,
                        fontFamily = FontFamily.Monospace,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                NodeHealthPill(node.health)
            }
            if (node.reachable) {
                Spacer(Modifier.width(4.dp))
                Text(
                    listOf(roleLabel(node.role), node.version, node.stage, node.arch)
                        .filter { it.isNotBlank() }
                        .joinToString("  ·  "),
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(top = 8.dp),
                )
            }
            node.unmetConditions.forEach {
                Text(
                    "${it.name}: ${it.reason}",
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalStatusColors.current.warn,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
            node.error?.let {
                Text(
                    it,
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalStatusColors.current.bad,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
    }
}

@Composable
private fun roleLabel(role: String) = when (role) {
    "controlplane" -> stringResource(R.string.overview_role_control_plane)
    else -> role
}

/**
 * Shown when the client certificate expires within [CERT_WARN_DAYS] days (like the alert).
 * An os:admin can renew it right away; anyone else needs a new talosconfig from an admin.
 */
@Composable
private fun CertificateBanner(summary: ContextSummary, onIssueConfig: () -> Unit) {
    if (summary.certNotAfter <= 0) return
    val days = daysUntil(summary.certNotAfter)
    if (days > CERT_WARN_DAYS) return
    val canRenew = summary.allows(Feature.ISSUE_CONFIG)
    val count = kotlin.math.abs(days).toInt()
    val text = if (days < 0) {
        pluralStringResource(R.plurals.overview_cert_expired, count, count)
    } else {
        pluralStringResource(R.plurals.overview_cert_expires, count, count)
    }
    val color = if (days < 0) LocalStatusColors.current.bad else LocalStatusColors.current.warn
    val content: @Composable () -> Unit = {
        Column(Modifier.padding(16.dp)) {
            Text(text, style = MaterialTheme.typography.bodyMedium, color = color)
            Text(
                stringResource(if (canRenew) R.string.overview_cert_renew else R.string.overview_cert_ask_admin),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
    if (canRenew) Card(onClick = onIssueConfig, modifier = Modifier.fillMaxWidth()) { content() }
    else Card(Modifier.fillMaxWidth()) { content() }
}

/**
 * Reminds that screenshot mode is on, i.e. names and addresses on screen are not the real
 * ones. A small icon rather than a label, so it stays out of the way in the screenshots.
 */
@Composable
private fun ScreenshotModeIcon() {
    val prefs = (LocalContext.current.applicationContext as TalosApp).uiPreferences
    val mask by prefs.privacyMask.collectAsStateWithLifecycle()
    if (!mask.enabled) return
    Icon(
        Icons.Outlined.VisibilityOff,
        contentDescription = stringResource(R.string.settings_screenshot_mode),
        tint = MaterialTheme.colorScheme.primary,
        modifier = Modifier.padding(end = 4.dp).size(14.dp),
    )
}

/** Shown when the daily check found a newer release; opens Settings → Updates. */
@Composable
private fun UpdateBanner(onClick: () -> Unit) {
    val updates = (LocalContext.current.applicationContext as TalosApp).updateManager
    val state by updates.state.collectAsStateWithLifecycle()
    val available = state as? UpdateState.Available ?: return
    Card(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Text(
            stringResource(R.string.overview_update_banner, available.info.version),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(16.dp),
        )
    }
}

/** Occasional, dismissable ask to support the project (see SupportPrompt for the timing). */
@Composable
private fun SupportCard() {
    val context = LocalContext.current
    val prompt = (context.applicationContext as TalosApp).supportPrompt
    val visible by prompt.visible.collectAsStateWithLifecycle()
    if (!visible) return
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text(stringResource(R.string.overview_support_title), style = MaterialTheme.typography.titleSmall)
            Text(
                stringResource(R.string.overview_support_body),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                TextButton(onClick = prompt::never) { Text(stringResource(R.string.overview_support_never)) }
                TextButton(onClick = prompt::later) { Text(stringResource(R.string.overview_support_later)) }
                TextButton(onClick = {
                    prompt.later()
                    openUrl(context, SPONSOR_URL)
                }) { Text(stringResource(R.string.overview_support_sponsor)) }
            }
        }
    }
}
