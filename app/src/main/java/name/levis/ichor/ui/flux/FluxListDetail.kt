package name.levis.ichor.ui.flux

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
 * The Flux apps. On a phone (or a folded foldable) it is [FluxScreen] alone and a tap opens the
 * app through [onOpenApp]. On a tablet or an unfolded foldable the picked app shows next to the
 * list, which stays; an app it links to replaces it there. The app's back arrow clears the pick;
 * its diff and source open full-screen.
 */
@OptIn(ExperimentalMaterial3AdaptiveApi::class)
@Composable
fun FluxListDetail(
    onBack: () -> Unit,
    onOpenApp: (kind: String, ns: String, name: String) -> Unit,
    onNode: (NodeOverview, Int) -> Unit,
    onDiff: (kind: String, ns: String, name: String) -> Unit,
    onSource: (kind: String, ns: String, name: String, index: Int) -> Unit,
) {
    if (!rememberTwoPanes()) {
        FluxScreen(onBack = onBack, onApp = onOpenApp)
        return
    }
    // "kind/namespace/name": neither a kind nor a Kubernetes namespace holds a slash.
    var picked by rememberSaveable { mutableStateOf<String?>(null) }
    val selected = picked?.split("/", limit = 3)?.takeIf { it.size == 3 }
    // Composed before the panes, so the app's sheets win over it.
    BackHandler(enabled = selected != null) { picked = null }
    ListDetailPaneScaffold(
        directive = calculatePaneScaffoldDirective(currentWindowAdaptiveInfoV2()),
        value = ListAndDetailExpanded,
        listPane = {
            AnimatedPane {
                FluxScreen(onBack = onBack, onApp = { kind, ns, name -> picked = "$kind/$ns/$name" })
            }
        },
        detailPane = {
            AnimatedPane {
                if (selected == null) {
                    EmptyDetailPane(Icons.Outlined.Sync, stringResource(R.string.panes_pick_app))
                } else {
                    val (kind, ns, name) = selected
                    // A fresh screen per app: its one-shot sheets do not carry over.
                    key(picked) {
                        FluxAppScreen(
                            kind = kind,
                            namespace = ns,
                            name = name,
                            onBack = { picked = null },
                            onNode = onNode,
                            onDiff = { onDiff(kind, ns, name) },
                            onApp = { other -> picked = "${other.kind}/${other.namespace}/${other.name}" },
                            onSource = { index -> onSource(kind, ns, name, index) },
                        )
                    }
                }
            }
        },
    )
}
