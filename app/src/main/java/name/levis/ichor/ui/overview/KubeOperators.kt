package name.levis.ichor.ui.overview

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.argorollouts.ArgoRolloutsViewModel
import name.levis.ichor.ui.crossplane.CrossplaneViewModel
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.lifecycle.ClusterLifecycleViewModel
import name.levis.ichor.ui.tailscale.TailscaleViewModel

/** The operators whose screens the Kubernetes screens card offers: those the cluster runs. */
internal data class KubeOperators(
    val crossplane: Boolean = false,
    val tailscale: Boolean = false,
    val clusterLifecycle: Boolean = false,
    val argoRollouts: Boolean = false,
)

/**
 * Asks the cluster for Crossplane, Tailscale, Cluster API or Sidero Metal, and Argo Rollouts
 * while [active] (the Talos overview's Kubernetes screens card is shown), again on each [key];
 * each answers "not installed" quickly. The same view models as the Kubernetes home's.
 */
@Composable
internal fun rememberKubeOperators(active: Boolean, key: Any?): KubeOperators {
    val crossplaneVm: CrossplaneViewModel = viewModel(key = "overview-crossplane", factory = factory { CrossplaneViewModel(app.kubeBrowser) })
    val tailscaleVm: TailscaleViewModel = viewModel(key = "overview-tailscale", factory = factory { TailscaleViewModel(app.kubeBrowser) })
    val lifecycleVm: ClusterLifecycleViewModel = viewModel(key = "overview-cluster-lifecycle", factory = factory { ClusterLifecycleViewModel(app.kubeBrowser) })
    val rolloutsVm: ArgoRolloutsViewModel = viewModel(key = "overview-argo-rollouts", factory = factory { ArgoRolloutsViewModel(app.argoRolloutsRepository) })
    LaunchedEffect(active, key) {
        if (!active) return@LaunchedEffect
        crossplaneVm.refresh(reset = true)
        tailscaleVm.refresh(reset = true)
        lifecycleVm.refresh(reset = true)
        rolloutsVm.refresh(reset = true)
    }
    val crossplane by crossplaneVm.state.collectAsStateWithLifecycle()
    val tailscale by tailscaleVm.state.collectAsStateWithLifecycle()
    val lifecycle by lifecycleVm.state.collectAsStateWithLifecycle()
    val rollouts by rolloutsVm.state.collectAsStateWithLifecycle()
    if (!active) return KubeOperators()
    return KubeOperators(
        crossplane = (crossplane as? UiState.Loaded)?.data?.installed == true,
        tailscale = (tailscale as? UiState.Loaded)?.data?.installed == true,
        clusterLifecycle = (lifecycle as? UiState.Loaded)?.data?.installed == true,
        argoRollouts = (rollouts as? UiState.Loaded)?.data?.installed == true,
    )
}
