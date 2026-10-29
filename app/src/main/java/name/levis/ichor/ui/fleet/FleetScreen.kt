package name.levis.ichor.ui.fleet

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.combine
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.FleetSources
import name.levis.ichor.model.FleetCounts
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.needAttention
import name.levis.ichor.monitor.syncMonitoring
import name.levis.ichor.selectCluster
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.factory

/**
 * Every cluster's health on one list, worst first (the fleet view): the last background check
 * of each, ranked by the Go core. Opening it calls no cluster; Refresh runs one check now. A
 * tap switches to that cluster and goes back to its home ([onHome]); on the demo's made-up
 * fleet it only goes back. With alerts off nothing is checked: it offers their settings ([onAlertsSettings]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FleetScreen(onBack: () -> Unit, onHome: () -> Unit, onAlertsSettings: () -> Unit) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val vm: FleetViewModel = viewModel(
        factory = factory {
            val sources = combine(
                app.monitorStore.state,
                app.configRepository.config,
                app.clusterNames.names,
                app.uiPreferences.privacyMask,
            ) { state, stored, names, mask -> FleetSources(state, stored?.summary, names, mask.enabled) }
            FleetViewModel(app.fleetRepository, sources, runCheck = { syncMonitoring(app, runNow = true) })
        },
    )
    val state by vm.state.collectAsStateWithLifecycle()
    val refreshing by vm.refreshing.collectAsStateWithLifecycle()
    val alertsOn by app.monitorStore.alertsEnabled.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val colors by app.clusterColors.colors.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val contexts = config?.summary?.contexts.orEmpty()
    val demo = contexts.isNotEmpty() && contexts.all { it.isDemo }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.fleet_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (alertsOn && !demo) {
                        TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.fleet_refresh), enabled = !refreshing, onClick = vm::refresh)
                    }
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            if (refreshing) LinearProgressIndicator(Modifier.fillMaxWidth())
            val summary = state.summary
            when {
                state.failed -> EmptyText(stringResource(R.string.fleet_failed))
                summary == null -> Unit
                summary.clusters.isEmpty() -> EmptyText(stringResource(R.string.fleet_empty))
                else -> LazyColumn(
                    contentPadding = PaddingValues(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    item { MutedText(fleetSummaryLine(summary.counts)) }
                    if (!alertsOn && !demo) item { AlertsOffCard(onAlertsSettings) }
                    if (demo) item { MutedText(stringResource(R.string.fleet_demo_hint)) }
                    items(summary.clusters, key = { it.key }) { row ->
                        val cluster = contexts.firstOrNull { it.name == row.context && !it.isDemo }
                        FleetRowCard(row, cluster, colors, masked = mask.enabled, now = state.now) {
                            cluster?.let { app.selectCluster(it.name) }
                            onHome()
                        }
                    }
                    if (summary.clusters.size == 1) item { MutedText(stringResource(R.string.fleet_one_cluster)) }
                }
            }
        }
    }
}

/** "Need attention: 2 · Not checked: 1 · Fine: 5", without the zeros. */
@Composable
private fun fleetSummaryLine(counts: FleetCounts): String = listOfNotNull(
    counts.needAttention.takeIf { it > 0 }?.let { stringResource(R.string.fleet_summary_attention, it) },
    counts.unknown.takeIf { it > 0 }?.let { stringResource(R.string.fleet_summary_unchecked, it) },
    counts.ok.takeIf { it > 0 }?.let { stringResource(R.string.fleet_summary_fine, it) },
).joinToString(" · ")

/** Without background checks nothing fills the list: their settings, one tap away. */
@Composable
private fun AlertsOffCard(onAlertsSettings: () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text(stringResource(R.string.fleet_alerts_off), style = MaterialTheme.typography.bodyMedium)
            TextButton(onClick = onAlertsSettings) { Text(stringResource(R.string.fleet_alerts_settings)) }
        }
    }
}
