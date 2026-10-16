package name.levis.ichor.ui.lifecycle

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
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.ClusterLifecycle
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.LifecycleKind
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
import name.levis.ichor.ui.tailscale.CountText

/** One read of the management cluster's Cluster API and Sidero Metal objects. */
class ClusterLifecycleViewModel(private val browser: KubeBrowserRepository) : LoadingViewModel<ClusterLifecycle>() {
    override suspend fun fetch() = browser.clusterLifecycle()
}

/**
 * Cluster lifecycle (os:admin): the workload clusters a management cluster runs with Cluster
 * API, their machines (by cluster, control plane first) and machine deployments, and the
 * Sidero Metal servers and server classes, problems first in each section. A tap opens the
 * object's summary ([onObject]); nothing is changed here.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ClusterLifecycleScreen(
    onBack: () -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    vm: ClusterLifecycleViewModel = viewModel(factory = factory { ClusterLifecycleViewModel(app.kubeBrowser) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.cluster_lifecycle_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        val rest = Modifier.pageContent(padding).fillMaxSize()
        when (val s = state) {
            is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, rest)
            is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = rest) {
                LifecycleList(s.data, onObject)
            }
            else -> LoadingBox(rest)
        }
    }
}

@Composable
private fun LifecycleList(r: ClusterLifecycle, onObject: (KubeObjectRef) -> Unit) {
    LazyColumn(Modifier.fillMaxSize()) {
        item(key = "header") { LifecycleHeader(r) }
        if (!r.installed || r.isEmpty) {
            item(key = "empty") { EmptyText(stringResource(R.string.cluster_lifecycle_empty)) }
            return@LazyColumn
        }
        section("clusters", R.string.cluster_lifecycle_clusters, r.clusters) { c ->
            ClusterRow(c, onClick = { onObject(r.ref(LifecycleKind.CLUSTER, c.namespace, c.name)) })
        }
        if (r.machines.isNotEmpty()) {
            item(key = "machines-title") { Title(R.string.cluster_lifecycle_machines) }
            r.machinesByCluster.forEach { (cluster, machines) ->
                item(key = "machines-of:$cluster") { ClusterSubtitle(cluster) }
                items(machines.size, key = { "machine:$cluster:$it" }) { i ->
                    val m = machines[i]
                    MachineRow(m, onClick = { onObject(r.ref(LifecycleKind.MACHINE, m.namespace, m.name)) })
                    HorizontalDivider()
                }
            }
        }
        section("deployments", R.string.cluster_lifecycle_machine_deployments, r.machineDeployments) { d ->
            MachineDeploymentRow(d, onClick = { onObject(r.ref(LifecycleKind.MACHINE_DEPLOYMENT, d.namespace, d.name)) })
        }
        section("servers", R.string.cluster_lifecycle_servers, r.servers) { s ->
            ServerRow(s, onClick = { onObject(r.ref(LifecycleKind.SERVER, "", s.name)) })
        }
        section("server-classes", R.string.cluster_lifecycle_server_classes, r.serverClasses) { sc ->
            ServerClassRow(sc, onClick = { onObject(r.ref(LifecycleKind.SERVER_CLASS, "", sc.name)) })
        }
    }
}

@Composable
private fun Title(title: Int) = SectionTitle(stringResource(title), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))

/** The cluster a group of machines belongs to. */
@Composable
private fun ClusterSubtitle(cluster: String) {
    Text(
        cluster.ifEmpty { "-" },
        style = MaterialTheme.typography.labelLarge,
        fontFamily = FontFamily.Monospace,
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 8.dp),
    )
}

/** A titled section of [rows] (problems first, as the Go core sorted them); nothing when empty. */
private fun <T> LazyListScope.section(key: String, title: Int, rows: List<T>, row: @Composable (T) -> Unit) {
    if (rows.isEmpty()) return
    item(key = "$key-title") { Title(title) }
    items(rows.size, key = { "$key:$it" }) { i ->
        row(rows[i])
        HorizontalDivider()
    }
}

/** What the cluster serves (Cluster API version, Sidero Metal, the Talos providers) and the counts. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun LifecycleHeader(r: ClusterLifecycle) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (r.error.isNotEmpty()) MutedText(stringResource(R.string.tailscale_partial, r.error))
        val served = listOfNotNull(
            r.version.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.cluster_lifecycle_api_version, it) },
            stringResource(R.string.cluster_lifecycle_sidero).takeIf { r.sidero },
            stringResource(R.string.cluster_lifecycle_talos_providers).takeIf { r.talosControlPlane || r.talosBootstrap },
        )
        if (served.isNotEmpty()) MutedText(served.joinToString(" · "))
        if (r.installed && !r.isEmpty) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                CountText(stringResource(R.string.tailscale_count_critical, r.counts.critical), critical = r.counts.critical > 0)
                CountText(stringResource(R.string.tailscale_count_warning, r.counts.warning), critical = false)
                CountText(stringResource(R.string.tailscale_count_ok, r.counts.ok), critical = false)
            }
        }
        if (r.truncated) MutedText(stringResource(R.string.tailscale_truncated))
    }
}
