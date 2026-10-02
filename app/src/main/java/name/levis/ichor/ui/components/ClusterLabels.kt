package name.levis.ichor.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ClusterLabels

/** How clusters are called on screen: the names the user gave them, unless screenshot mode is on. */
@Composable
fun rememberClusterLabels(): ClusterLabels {
    val app = LocalContext.current.applicationContext as TalosApp
    val names by app.clusterNames.names.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    return remember(names, mask.enabled) { ClusterLabels(names, mask.enabled) }
}
