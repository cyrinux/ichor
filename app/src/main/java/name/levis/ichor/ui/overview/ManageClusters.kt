package name.levis.ichor.ui.overview

import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import name.levis.ichor.TalosApp
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.ui.userMessage

/**
 * The cluster sheet as both homes (Talos overview, Kubernetes home) open it: pick, rename,
 * color, VPN only, remove, add. [onEndpoints] edits a Talos cluster's endpoints, when offered.
 */
@Composable
fun ManageClustersSheet(
    config: StoredConfig,
    colors: Map<String, Int>,
    labels: ClusterLabels,
    onClose: () -> Unit,
    onAddCluster: () -> Unit,
    onClustersCleared: () -> Unit,
    onEndpoints: ((ContextSummary) -> Unit)? = null,
) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val vpnOnly by app.vpnOnly.fingerprints.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    ClusterSheet(
        config = config,
        colors = colors,
        labels = labels,
        onSelect = {
            onClose()
            app.selectCluster(it)
        },
        onRename = { cluster, name -> app.renameCluster(cluster.fingerprint, name) },
        onColor = { cluster, color -> app.clusterColors.set(cluster.fingerprint, color) },
        vpnOnly = vpnOnly,
        onVpnOnly = { cluster, on -> app.setVpnOnly(cluster.fingerprint, on) },
        onAdd = {
            onClose()
            onAddCluster()
        },
        onRemove = { name ->
            scope.launch {
                runCatching { app.removeCluster(name) }.fold(
                    onSuccess = { remains -> if (!remains) onClustersCleared() },
                    onFailure = { Toast.makeText(context, it.userMessage(), Toast.LENGTH_LONG).show() },
                )
            }
        },
        onDismiss = onClose,
        onEndpoints = onEndpoints?.let { edit ->
            { cluster ->
                onClose()
                edit(cluster)
            }
        },
    )
}
