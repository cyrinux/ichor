package name.levis.ichor.ui.alerts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KUBE_NODES
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.data.activeIsKube
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.AM_SILENCE_PRESETS
import name.levis.ichor.model.AmAlert
import name.levis.ichor.model.AmSilenceState
import name.levis.ichor.model.AmSilences
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.PromSource
import name.levis.ichor.monitor.SILENCE_ACTION_MINUTES
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.AppTab
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.SwipeTabPager
import name.levis.ichor.ui.components.TWO_TABS
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.components.rememberKubeCanDenial
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.metrics.SourceDialog

/**
 * The alerts of the cluster's Alertmanager (found in the cluster through the service proxy,
 * or at a URL set by the user), and its silences. An alert opens its sheet, where it can be
 * silenced; a silence can be expired. Opened on the Alerts tab by default. [silenceFingerprint]:
 * the alert whose silence form opens once read (a notification's Silence 1 h), with a comment saying so.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AlertsScreen(onBack: () -> Unit, links: AlertObjectLinks, silenceFingerprint: String = "") {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    val fingerprint = config?.activeSummary?.fingerprint ?: return
    val key = "alerts-$fingerprint-$invalidations"
    val vm: AlertsViewModel = viewModel(
        key = key,
        factory = factory { AlertsViewModel(app.alertmanagerRepository, app.kubeRepository, app.alertmanagerStore, fingerprint) },
    )
    val state by vm.state.collectAsStateWithLifecycle()
    val busy by vm.busy.collectAsStateWithLifecycle()
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var sourceOpen by remember { mutableStateOf(false) }
    var opened by remember { mutableStateOf<AmAlert?>(null) }
    var silencing by remember { mutableStateOf<AmAlert?>(null) }
    LaunchedEffect(key) { vm.load() }
    ActionToasts(vm)
    // Asked from a notification: once the alerts are read, if it still fires (not again on rotation).
    var silenceAsked by rememberSaveable { mutableStateOf(silenceFingerprint.isEmpty()) }
    val firing = (state.alerts as? UiState.Loaded)?.data?.groups
    LaunchedEffect(firing) {
        if (silenceAsked || firing == null) return@LaunchedEffect
        silenceAsked = true
        silencing = firing.flatMap { it.alerts }.firstOrNull { it.fingerprint == silenceFingerprint }
    }

    // The nodes an alert's `node` or `instance` label may name: the home's last list.
    val kube = config?.activeIsKube == true
    val nodes = remember(kube, state.alerts) {
        if (kube) app.kubeRepository.cached<KubeNodesOverview>(KUBE_NODES)?.value?.nodes.orEmpty().map { it.name }.toSet()
        else app.talosRepository.cached<ClusterOverview>(OVERVIEW)?.value?.nodes.orEmpty().flatMap { listOf(it.hostname, it.node) }.toSet()
    }
    val source = state.source
    val silenceDenial = rememberProxyDenial(source, "create")
    val expireDenial = rememberProxyDenial(source, "delete")

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.alerts_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = vm::refresh)
                    TooltipIconButton(Icons.Outlined.Tune, stringResource(R.string.alerts_source), onClick = { sourceOpen = true })
                },
            )
        },
    ) { padding ->
        val modifier = Modifier.pageContent(padding)
        when {
            !state.ready -> LoadingBox(modifier)
            source == null -> NoSource(state, modifier, onSetUp = { sourceOpen = true }, onSearch = { vm.discover(useFirst = true) })
            else -> Column(modifier.fillMaxSize()) {
                MutedText(stringResource(R.string.alerts_source_line, source.label), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
                PrimaryTabRow(selectedTabIndex = tab) {
                    AppTab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.alerts_tab_alerts)) })
                    AppTab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(silencesTitle(state.silences)) })
                }
                SwipeTabPager(TWO_TABS, tab, onSelect = { tab = it }) { page ->
                    when (page) {
                        0 -> AlertsTab(state.alerts, state.filter, vm::setFilter, onAlert = { opened = it }, onRefresh = vm::refresh)
                        else -> SilencesTab(
                            state.silences,
                            state.withExpired,
                            vm::setWithExpired,
                            busy = busy,
                            denial = expireDenial,
                            onExpire = vm::expire,
                            onRefresh = vm::refresh,
                        )
                    }
                }
            }
        }
    }

    opened?.let { alert ->
        AlertSheet(
            alert = alert,
            nodes = nodes,
            links = links,
            silenceDenial = silenceDenial,
            onSilence = { opened = null; silencing = alert },
            onDismiss = { opened = null },
        )
    }
    silencing?.let { alert ->
        val asked = alert.fingerprint == silenceFingerprint
        SilenceSheet(
            alert = alert,
            matchersFor = vm::matchersFor,
            denial = silenceDenial,
            onSilence = { matchers, minutes, comment ->
                silencing = null
                vm.silence(matchers, minutes, comment)
            },
            onDismiss = { silencing = null },
            initialMinutes = if (asked) SILENCE_ACTION_MINUTES else AM_SILENCE_PRESETS.first(),
            initialComment = if (asked) stringResource(R.string.alert_action_silence_comment) else "",
        )
    }
    if (sourceOpen) {
        SourceDialog(
            current = state.source,
            discovered = state.discovered,
            discovering = state.discovering,
            discoveryError = state.discoveryError,
            onDiscover = { vm.discover() },
            onTest = vm::test,
            onSave = vm::setSource,
            onDismiss = { sourceOpen = false },
            title = stringResource(R.string.alerts_source),
            noneFound = stringResource(R.string.alerts_none_found),
            reachable = stringResource(R.string.alerts_reachable),
        )
    }
}

/** "Silences (2)": the active and pending ones, once read. */
@Composable
private fun silencesTitle(state: UiState<AmSilences>): String {
    val silences = (state as? UiState.Loaded)?.data?.silences ?: return stringResource(R.string.alerts_tab_silences)
    val live = silences.count { it.state != AmSilenceState.EXPIRED }
    return if (live == 0) stringResource(R.string.alerts_tab_silences) else stringResource(R.string.alerts_tab_silences_count, live)
}

/**
 * Through the service proxy, a silence is a POST and an expiry a DELETE on the Service's
 * `services/proxy` ([verb] create or delete): what refuses it for sure, if anything. A URL
 * source is not the Kubernetes API's to refuse: never gated.
 */
@Composable
private fun rememberProxyDenial(source: PromSource?, verb: String): KubePermission? {
    val proxied = source?.takeIf { it.mode == PromSource.MODE_PROXY && it.service.isNotEmpty() }
    return rememberKubeCanDenial(verb, "", "services/proxy", proxied?.namespace.orEmpty(), proxied?.service.orEmpty())
        .takeIf { proxied != null }
}

@Composable
private fun NoSource(state: AlertsState, modifier: Modifier, onSetUp: () -> Unit, onSearch: () -> Unit) {
    Column(modifier.fillMaxSize().padding(24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (state.discovering) {
            Text(stringResource(R.string.alerts_searching))
            LinearProgressIndicator(Modifier.fillMaxWidth())
            return@Column
        }
        Text(stringResource(R.string.alerts_none_found), style = MaterialTheme.typography.titleMedium)
        MutedText(state.discoveryError ?: state.error ?: stringResource(R.string.alerts_none_found_hint))
        Button(onClick = onSetUp) { Text(stringResource(R.string.metrics_set_up)) }
        TextButton(onClick = onSearch) { Text(stringResource(R.string.metrics_search_again)) }
    }
}

/** A toast for each silence created or expired, or why it failed. */
@Composable
private fun ActionToasts(vm: AlertsViewModel) = ResultToasts(vm.results) { context, r ->
    val error = r.error?.resolve(context)
    val text = when {
        error != null && r.expired -> context.getString(R.string.alerts_expire_failed, error)
        error != null -> context.getString(R.string.alerts_silence_failed, error)
        r.expired -> context.getString(R.string.alerts_expired)
        else -> context.getString(R.string.alerts_silenced)
    }
    text to (error != null)
}
