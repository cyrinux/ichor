package name.levis.ichor.ui.argocd

import androidx.activity.compose.BackHandler
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Sync
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
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.ui.components.EmptyDetailPane
import name.levis.ichor.ui.components.ListAndDetailExpanded
import name.levis.ichor.ui.components.rememberTwoPanes

/**
 * The Argo CD apps. On a phone (or a folded foldable) it is [ArgoAppsScreen] alone and a tap
 * opens the app through [onOpenApp]. On a tablet or an unfolded foldable the picked app shows
 * next to the list, which stays; both read this back-stack entry's ArgoViewModel, so one
 * refresh serves both. The app's back arrow clears the pick; its diff opens full-screen.
 */
@OptIn(ExperimentalMaterial3AdaptiveApi::class)
@Composable
fun ArgoListDetail(
    onBack: () -> Unit,
    onOpenApp: (ns: String, name: String) -> Unit,
    onWindows: () -> Unit,
    onNode: (NodeOverview, Int) -> Unit,
    onDiff: (ns: String, name: String) -> Unit,
) {
    if (!rememberTwoPanes()) {
        ArgoAppsScreen(onBack = onBack, onApp = onOpenApp, onWindows = onWindows)
        return
    }
    // "namespace/name": a Kubernetes namespace never holds a slash.
    var picked by rememberSaveable { mutableStateOf<String?>(null) }
    val selected = picked?.split("/", limit = 2)?.takeIf { it.size == 2 }
    // Composed before the panes, so the list's multi-select and the app's sheets win over it.
    BackHandler(enabled = selected != null) { picked = null }
    ListDetailPaneScaffold(
        directive = calculatePaneScaffoldDirective(currentWindowAdaptiveInfoV2()),
        value = ListAndDetailExpanded,
        listPane = {
            AnimatedPane {
                ArgoAppsScreen(onBack = onBack, onApp = { ns, name -> picked = "$ns/$name" }, onWindows = onWindows)
            }
        },
        detailPane = {
            AnimatedPane {
                if (selected == null) {
                    EmptyDetailPane(Icons.Outlined.Sync, stringResource(R.string.panes_pick_app))
                } else {
                    val (ns, name) = selected
                    // A fresh screen per app: its one-shot sheets do not carry over.
                    key(picked) {
                        ArgoAppScreen(
                            namespace = ns,
                            name = name,
                            onBack = { picked = null },
                            onNode = onNode,
                            onWindows = onWindows,
                            onDiff = { onDiff(ns, name) },
                        )
                    }
                }
            }
        },
    )
}
