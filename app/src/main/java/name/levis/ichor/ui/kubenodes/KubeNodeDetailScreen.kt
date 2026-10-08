package name.levis.ichor.ui.kubenodes

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.Card
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.PodSelection
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.healthy
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.checkup.ageSince
import name.levis.ichor.ui.components.AppTab
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.SwipeTabPager
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.overview.KubeCordonDialog
import name.levis.ichor.ui.overview.KubeHomeViewModel
import name.levis.ichor.ui.overview.coresLabel
import name.levis.ichor.ui.overview.nodeCapacityLabel
import name.levis.ichor.ui.overview.nodePoolLabel
import name.levis.ichor.ui.share.ShareLinkMenuItem
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.workloads.KubeEventsList
import name.levis.ichor.ui.workloads.SelectedPodsList
import name.levis.ichor.util.formatBytes

/** The tabs of [KubeNodeDetailScreen]: the node itself, the pods it runs, its events. */
private val KUBE_NODE_TABS = listOf(0, 1, 2)

/** The Pods tab of [KubeNodeDetailScreen], where a pod's node opens it. */
const val KUBE_NODE_PODS_TAB = 1

/**
 * A node of a cluster added from a kubeconfig, as the Kubernetes API describes it: its status,
 * roles, addresses, software and capacity, the pods it runs and its events, with the cordon,
 * the drain, its YAML and a share link in the menu. The data is the Kubernetes home's ([vm]),
 * refreshed from here too. No Talos tab: the cluster has no Talos API.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubeNodeDetailScreen(
    name: String,
    initialTab: Int,
    vm: KubeHomeViewModel,
    onBack: () -> Unit,
    onDrain: (name: String) -> Unit,
    onYaml: (name: String) -> Unit,
) {
    val state by vm.state.collectAsStateWithLifecycle()
    var tab by rememberSaveable { mutableStateOf(initialTab.coerceIn(0, KUBE_NODE_TABS.lastIndex)) }
    var menuOpen by remember { mutableStateOf(false) }
    var cordoning by remember { mutableStateOf<KubeNodeInfo?>(null) }
    val snackbar = remember { SnackbarHostState() }
    val node = (state as? UiState.Loaded)?.data?.nodes?.firstOrNull { it.name == name }
    KubeCordonDialog(cordoning, snackbar, onCordoned = vm::refresh, onDismiss = { cordoning = null })

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = { Text(name, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() })
                    TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { menuOpen = true })
                    DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                        node?.let { n ->
                            DropdownMenuItem(
                                text = { Text(stringResource(if (n.cordoned) R.string.node_menu_uncordon else R.string.node_menu_cordon)) },
                                leadingIcon = { Icon(Icons.Outlined.Block, null) },
                                onClick = {
                                    menuOpen = false
                                    cordoning = n
                                },
                            )
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.node_menu_drain)) },
                                leadingIcon = { Icon(Icons.AutoMirrored.Outlined.Logout, null) },
                                onClick = {
                                    menuOpen = false
                                    onDrain(n.name)
                                },
                            )
                        }
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.kube_node_yaml)) },
                            leadingIcon = { Icon(Icons.Outlined.Code, null) },
                            onClick = {
                                menuOpen = false
                                onYaml(name)
                            },
                        )
                        // The link names the node as Kubernetes does and by its address: a phone holding a
                        // talosconfig for the cluster opens the Talos node of the same name.
                        ShareLinkMenuItem(ShareTarget.node(node?.internalIP.orEmpty(), name, kubeNodeShareTab(tab)), onClick = { menuOpen = false })
                    }
                },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            PrimaryTabRow(selectedTabIndex = tab) {
                AppTab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.kube_node_tab_details)) })
                AppTab(selected = tab == KUBE_NODE_PODS_TAB, onClick = { tab = KUBE_NODE_PODS_TAB }, text = { Text(stringResource(R.string.node_tab_pods)) })
                AppTab(selected = tab == 2, onClick = { tab = 2 }, text = { Text(stringResource(R.string.events_title)) })
            }
            SwipeTabPager(KUBE_NODE_TABS, tab, onSelect = { tab = it }) { page ->
                when (page) {
                    0 -> DetailsTab(state, node, vm::refresh)
                    KUBE_NODE_PODS_TAB -> SelectedPodsList(PodSelection.OnKubeNode(name))
                    else -> KubeEventsList(namespace = "", kind = "Node", name = name, modifier = Modifier.verticalScroll(rememberScrollState()).padding(16.dp))
                }
            }
        }
    }
}

/** The share link's node tab: the Kubernetes pods tab of the Talos node screen for the Pods tab here. */
private fun kubeNodeShareTab(tab: Int): Int = if (tab == KUBE_NODE_PODS_TAB) ShareTarget.NODE_TABS.indexOf("kube-pods") else 0

@Composable
private fun DetailsTab(state: UiState<KubeNodesOverview>, node: KubeNodeInfo?, onRetry: () -> Unit) {
    when {
        node != null -> NodeDetails(node)
        state is UiState.Loading -> LoadingBox(Modifier.fillMaxSize())
        state is UiState.Failed -> ErrorBox(state.message, onRetry, Modifier.fillMaxSize())
        else -> EmptyText(stringResource(R.string.kube_node_gone), Modifier.fillMaxSize())
    }
}

/** Status pills, then the node's identity, software, capacity and, when the cloud says, its provenance. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun NodeDetails(node: KubeNodeInfo) {
    val colors = LocalStatusColors.current
    val now = remember(node) { System.currentTimeMillis() }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxSize()) {
        item(key = "identity") {
            DetailsCard {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    if (node.ready) {
                        StatusPill(stringResource(R.string.common_status_ready), if (node.healthy) colors.ok else colors.warn)
                    } else {
                        StatusPill(stringResource(R.string.common_status_not_ready), colors.bad)
                    }
                    if (node.cordoned) StatusPill(stringResource(R.string.kube_node_cordoned), colors.warn)
                    // Kubernetes' own condition names: what kubectl shows too.
                    node.pressure.forEach { StatusPill(it, colors.warn) }
                }
                InfoRow(stringResource(R.string.kube_node_roles), node.roles.joinToString(", ").ifEmpty { stringResource(R.string.kube_node_no_role) })
                if (node.internalIP.isNotEmpty()) InfoRow(stringResource(R.string.kube_node_internal_ip), node.internalIP, mono = true)
                if (node.externalIP.isNotEmpty()) InfoRow(stringResource(R.string.kube_node_external_ip), node.externalIP, mono = true)
                if (node.created > 0) InfoRow(stringResource(R.string.kube_node_created), stringResource(R.string.kube_events_ago, ageSince(node.created * 1000, now)))
            }
        }
        item(key = "software") {
            DetailsCard {
                Text(stringResource(R.string.kube_node_software), style = MaterialTheme.typography.titleSmall)
                if (node.kubelet.isNotEmpty()) InfoRow(stringResource(R.string.kube_node_kubelet), node.kubelet, mono = true)
                if (node.osImage.isNotEmpty()) InfoRow(stringResource(R.string.kube_node_os_image), node.osImage)
                if (node.kernel.isNotEmpty()) InfoRow(stringResource(R.string.kube_node_kernel), node.kernel, mono = true)
                if (node.runtime.isNotEmpty()) InfoRow(stringResource(R.string.kube_node_runtime), node.runtime, mono = true)
                if (node.arch.isNotEmpty()) InfoRow(stringResource(R.string.kube_node_arch), node.arch)
            }
        }
        item(key = "capacity") {
            DetailsCard {
                Text(stringResource(R.string.kube_node_capacity), style = MaterialTheme.typography.titleSmall)
                InfoRow(stringResource(R.string.overview_stat_cpu), coresLabel(node.cpu))
                InfoRow(stringResource(R.string.overview_stat_memory), formatBytes(node.memory.toLong()))
                InfoRow(stringResource(R.string.kube_node_pod_limit), node.podLimit.toString())
            }
        }
        val pool = node.pool.isNotEmpty() || node.instanceType.isNotEmpty() || node.capacity.isNotEmpty()
        if (pool) item(key = "cloud") {
            DetailsCard {
                Text(stringResource(R.string.kube_node_cloud), style = MaterialTheme.typography.titleSmall)
                nodePoolLabel(node)?.let { InfoRow(stringResource(R.string.kube_node_pool), it) }
                if (node.instanceType.isNotEmpty()) InfoRow(stringResource(R.string.kube_node_instance_type), node.instanceType, mono = true)
                nodeCapacityLabel(node)?.let { InfoRow(stringResource(R.string.kube_node_capacity_type), it) }
            }
        }
    }
}

@Composable
private fun DetailsCard(content: @Composable () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) { content() }
    }
}
