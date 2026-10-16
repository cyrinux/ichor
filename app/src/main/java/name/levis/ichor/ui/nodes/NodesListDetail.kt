package name.levis.ichor.ui.nodes

import androidx.activity.compose.BackHandler
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi
import androidx.compose.material3.adaptive.currentWindowAdaptiveInfoV2
import androidx.compose.material3.adaptive.layout.AnimatedPane
import androidx.compose.material3.adaptive.layout.ListDetailPaneScaffold
import androidx.compose.material3.adaptive.layout.calculatePaneScaffoldDirective
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.ui.components.EmptyDetailPane
import name.levis.ichor.ui.components.ListAndDetailExpanded
import name.levis.ichor.ui.components.rememberTwoPanes
import name.levis.ichor.ui.node.NodeDetailScreen
import name.levis.ichor.ui.node.NodeMenuEntry
import name.levis.ichor.ui.overview.NodeAction
import name.levis.ichor.ui.overview.OverviewViewModel

/** The node picked in the list pane, saved as "address|hostname|role" (survives rotation). */
private fun encodePick(n: NodeOverview) = listOf(n.node, n.hostname, n.role).joinToString("|")

/** Back to a node with what its screen and routes need: address, hostname and role. */
private fun decodePick(s: String?): NodeOverview? =
    s?.split("|", limit = 3)?.takeIf { it.size == 3 }?.let { NodeOverview(node = it[0], hostname = it[1], reachable = true, role = it[2]) }

/**
 * The nodes list. On a phone (or a folded foldable) it is [NodesScreen] alone and a tap opens
 * the node's screen through [onOpenNode]. On a tablet or an unfolded foldable the picked node's
 * screen shows next to the list, which stays; its back arrow clears the pick, its sections and
 * tabs still open their full-screen routes through [onTab] and [onMenu].
 */
@OptIn(ExperimentalMaterial3AdaptiveApi::class)
@Composable
fun NodesListDetail(
    initialFilter: NodeFilter?,
    vm: OverviewViewModel,
    onBack: () -> Unit,
    onNodeAction: (NodeOverview, NodeAction) -> Unit,
    onOpenNode: (NodeOverview) -> Unit,
    onTab: (NodeOverview, Int) -> Unit,
    onMenu: (NodeOverview, NodeMenuEntry) -> Unit,
) {
    if (!rememberTwoPanes()) {
        NodesScreen(initialFilter = initialFilter, vm = vm, onBack = onBack, onNode = onOpenNode, onNodeAction = onNodeAction)
        return
    }
    var picked by rememberSaveable { mutableStateOf<String?>(null) }
    val selected = decodePick(picked)
    // Composed before the panes, so the detail's own handlers (an open sheet) win over it.
    BackHandler(enabled = selected != null) { picked = null }
    ListDetailPaneScaffold(
        directive = calculatePaneScaffoldDirective(currentWindowAdaptiveInfoV2()),
        value = ListAndDetailExpanded,
        listPane = {
            AnimatedPane {
                NodesScreen(
                    initialFilter = initialFilter,
                    vm = vm,
                    onBack = onBack,
                    onNode = { picked = encodePick(it) },
                    onNodeAction = onNodeAction,
                )
            }
        },
        detailPane = {
            AnimatedPane {
                if (selected == null) {
                    EmptyDetailPane(Icons.Outlined.Dns, stringResource(R.string.panes_pick_node))
                } else {
                    // A fresh screen per node: its one-shot confirmations do not carry over.
                    key(picked) {
                        NodeDetailScreen(
                            node = selected.node,
                            hostname = selected.hostname,
                            role = selected.role,
                            onBack = { picked = null },
                            onTab = { tab -> onTab(selected, tab) },
                            onMenu = { entry -> onMenu(selected, entry) },
                        )
                    }
                }
            }
        },
    )
}
