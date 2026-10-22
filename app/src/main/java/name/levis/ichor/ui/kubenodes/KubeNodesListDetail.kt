package name.levis.ichor.ui.kubenodes

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
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.EmptyDetailPane
import name.levis.ichor.ui.components.ListAndDetailExpanded
import name.levis.ichor.ui.components.rememberTwoPanes
import name.levis.ichor.ui.overview.KubeHomeViewModel

/**
 * The Kubernetes nodes. On a phone (or a folded foldable) it is [KubeNodesScreen] alone and a
 * tap opens the node menu. On a tablet or an unfolded foldable the picked node shows next to
 * the list, which stays, with the menu's actions at its top ([KubeNodeDetail]). The pick is a
 * name, so it survives the list's refreshes; the detail's back arrow clears it.
 */
@OptIn(ExperimentalMaterial3AdaptiveApi::class)
@Composable
fun KubeNodesListDetail(
    initialFilter: NodeFilter?,
    vm: KubeHomeViewModel,
    onBack: () -> Unit,
    onDrain: (name: String) -> Unit,
    onDebug: (name: String) -> Unit,
    onMeta: (name: String) -> Unit,
    onOwner: (KubeObjectRef) -> Unit,
    initialQuery: String = "",
) {
    if (!rememberTwoPanes()) {
        KubeNodesScreen(initialFilter, vm, onBack, onDrain, onDebug, onMeta, initialQuery)
        return
    }
    var picked by rememberSaveable { mutableStateOf<String?>(null) }
    val state by vm.state.collectAsStateWithLifecycle()
    val node = picked?.let { name -> (state as? UiState.Loaded)?.data?.nodes?.firstOrNull { it.name == name } }
    // Composed before the panes, so the detail's sheets win over it. A node gone from the list
    // shows the placeholder again, and Back then leaves.
    BackHandler(enabled = node != null) { picked = null }
    ListDetailPaneScaffold(
        directive = calculatePaneScaffoldDirective(currentWindowAdaptiveInfoV2()),
        value = ListAndDetailExpanded,
        listPane = {
            AnimatedPane {
                KubeNodesScreen(initialFilter, vm, onBack, onDrain, onDebug, onMeta, initialQuery, onNode = { picked = it.name })
            }
        },
        detailPane = {
            AnimatedPane {
                if (node == null) {
                    EmptyDetailPane(Icons.Outlined.Dns, stringResource(R.string.panes_pick_node))
                } else {
                    // A fresh screen per node: its dialogs and tabs do not carry over.
                    key(picked) {
                        KubeNodeDetail(
                            node = node,
                            onBack = { picked = null },
                            onDrain = onDrain,
                            onDebug = onDebug,
                            onMeta = onMeta,
                            onCordoned = vm::refresh,
                            onOwner = onOwner,
                        )
                    }
                }
            }
        },
    )
}
