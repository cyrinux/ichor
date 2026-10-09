package name.levis.ichor.ui.argocd

import android.widget.Toast
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoNetNode
import name.levis.ichor.model.ArgoNetProblem
import name.levis.ichor.model.ArgoNetWording
import name.levis.ichor.model.ArgoNetwork
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.dataservices.CauseBanner
import name.levis.ichor.ui.factory

/**
 * The app detail's "Network" section: how traffic reaches the app, as a layered graph with the
 * likely root cause above it. Loads with the section and reloads whenever the app does
 * ([fetchedAt]: pull to refresh, the polling while a sync runs). The nodes Talos reports down
 * ([downNodes]) are drawn critical; [talosNodes] are the nodes [onNode] can open.
 */
@Composable
fun ArgoNetworkSection(
    app: ArgoApp,
    fetchedAt: Long,
    downNodes: Set<String>,
    talosNodes: List<NodeOverview>,
    onNode: ((NodeOverview, Int) -> Unit)?,
) {
    val talos = LocalContext.current.applicationContext as TalosApp
    val vm: ArgoNetworkViewModel = viewModel(key = "argo-network/${app.key}", factory = factory { ArgoNetworkViewModel(talos.gitOpsRepository, talos.kubeRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(app.key, fetchedAt) { vm.load(app, app.key to fetchedAt) }
    DeleteToasts(vm)

    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        SectionTitle(stringResource(R.string.argo_net_title))
        when (val s = state) {
            UiState.Loading -> NetworkSkeleton()
            is UiState.Failed -> MutedText(stringResource(R.string.argo_net_failed, s.message.asString()))
            is UiState.Loaded -> {
                val network = remember(s.data, downNodes) { s.data.withDownNodes(downNodes) }
                NetworkBody(app, network, talosNodes, onNode, vm)
            }
        }
    }
}

@Composable
private fun NetworkBody(
    app: ArgoApp,
    network: ArgoNetwork,
    talosNodes: List<NodeOverview>,
    onNode: ((NodeOverview, Int) -> Unit)?,
    vm: ArgoNetworkViewModel,
) {
    if (network.takesNoTraffic) {
        MutedText(stringResource(R.string.argo_net_empty))
        return
    }
    var selected by rememberSaveable(app.key) { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<ArgoNetNode?>(null) }
    val inFlight by vm.deleting.collectAsStateWithLifecycle()
    // A box gone after a reload (a deleted pod) takes its highlight with it.
    val node = selected?.let { network.node(it) }

    network.problem?.let { CauseBanner(problemText(it)) }
    ArgoNetworkGraph(network, selected = node?.id, onSelect = { selected = it })
    ArgoNetLegend()
    AnimatedVisibility(node != null, enter = fadeIn() + expandVertically(), exit = fadeOut() + shrinkVertically()) {
        val shown = node ?: return@AnimatedVisibility
        val podNode = if (shown.kind == ArgoNetNode.POD) network.nodeOf(shown)?.name else null
        val hostname = if (shown.kind == ArgoNetNode.NODE) shown.name else podNode
        val actions = ArgoNetActions(
            node = hostname?.let { h -> talosNodes.firstOrNull { it.hostname == h } },
            onNode = onNode,
            onDeletePod = if (shown.canDelete && "${shown.namespace}/${shown.name}" !in inFlight) ({ deleting = shown }) else null,
        )
        ArgoNetDetails(shown, podNode, actions, onClose = { selected = null })
    }

    deleting?.let { pod ->
        ConfirmDialog(
            title = stringResource(R.string.pods_delete_title, pod.name),
            text = stringResource(R.string.argo_net_pod_delete_text, pod.namespace),
            confirm = stringResource(R.string.pods_delete_confirm),
            onConfirm = { deleting = null; vm.deletePod(app, pod.namespace, pod.name) },
            onDismiss = { deleting = null },
            destructive = true,
        )
    }
}

/** The banner's sentence for the likely root cause. */
@Composable
private fun problemText(problem: ArgoNetProblem): String = when (problem.wording) {
    ArgoNetWording.NODE_NOT_READY -> stringResource(R.string.argo_net_problem_node, problem.name)
    ArgoNetWording.NODE_CORDONED -> stringResource(R.string.argo_net_problem_cordoned, problem.name)
    ArgoNetWording.POD_STATUS -> stringResource(R.string.argo_net_problem_pod, problem.name, problem.detail)
    ArgoNetWording.NO_READY_PODS -> stringResource(R.string.argo_net_problem_service, problem.name)
    ArgoNetWording.NO_HEALTHY_BACKEND -> stringResource(R.string.argo_net_problem_backend, problem.kind, problem.name)
}

@Composable
private fun DeleteToasts(vm: ArgoNetworkViewModel) {
    val context = LocalContext.current
    LaunchedEffect(vm) {
        vm.results.collect { r ->
            val text = r.error?.resolve(context)?.let { context.getString(R.string.pods_delete_failed, r.pod.name, it) }
                ?: context.getString(R.string.pods_delete_done, r.pod.name)
            Toast.makeText(context, text, Toast.LENGTH_SHORT).show()
        }
    }
}

/** Grey columns of card shapes while the graph loads: the shape of what is coming. */
@Composable
private fun NetworkSkeleton() {
    val shade = MaterialTheme.colorScheme.surfaceContainerHigh
    Row(horizontalArrangement = Arrangement.spacedBy(24.dp), verticalAlignment = Alignment.CenterVertically) {
        listOf(1, 1, 2, 1).forEach { rows ->
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                repeat(rows) { Box(Modifier.width(CARD_WIDTH * 0.6f).height(CARD_HEIGHT).background(shade, RoundedCornerShape(12.dp))) }
            }
        }
    }
}

/** A pod not already on its way out: deleting a terminating one would change nothing. */
private val ArgoNetNode.canDelete: Boolean
    get() = kind == ArgoNetNode.POD && !detail.startsWith("Terminating")
