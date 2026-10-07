package name.levis.ichor.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.CloudContext
import name.levis.ichor.model.ClusterLabels

/** How clusters are called on screen: the names the user gave them, unless screenshot mode is on. */
@Composable
fun rememberClusterLabels(): ClusterLabels {
    val app = LocalContext.current.applicationContext as TalosApp
    val names by app.clusterNames.names.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    return remember(names, mask.enabled) { ClusterLabels(names, mask.enabled) }
}

/** A cloud context's location and owner, under its cluster name: "EKS · eu-north-1 · account 12…12". */
@Composable
fun cloudDetail(cloud: CloudContext): String {
    val owner = if (cloud.provider == CloudContext.EKS) R.string.kube_eks_account else R.string.kube_gke_project
    return listOf(cloud.provider, cloud.location, stringResource(owner, cloud.shortOwner)).joinToString(" · ")
}
