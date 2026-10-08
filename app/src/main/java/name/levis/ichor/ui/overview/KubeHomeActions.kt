package name.levis.ichor.ui.overview

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.KubeHomeAction
import name.levis.ichor.model.KubeHomeBar
import name.levis.ichor.ui.components.ActionBarActions
import name.levis.ichor.ui.components.ActionLook
import name.levis.ichor.ui.components.Destination

/** The screen each of the Kubernetes home's actions leads to. */
val KubeHomeAction.destination: Destination
    get() = when (this) {
        KubeHomeAction.WORKLOADS -> Destination.WORKLOADS
        KubeHomeAction.RESOURCES -> Destination.RESOURCES
        KubeHomeAction.METRICS -> Destination.METRICS
        KubeHomeAction.HELM -> Destination.HELM
        KubeHomeAction.DATA_SERVICES -> Destination.DATA_SERVICES
        KubeHomeAction.CHECKUP -> Destination.CHECKUP
        KubeHomeAction.API_HEALTH -> Destination.API_HEALTH
        KubeHomeAction.NETWORK_POLICIES -> Destination.NETWORK_POLICIES
        KubeHomeAction.EVENTS -> Destination.EVENTS
        KubeHomeAction.SETTINGS -> Destination.SETTINGS
    }

/** How the Kubernetes home's actions look, in its bar and its editor. */
val kubeHomeActionLook = ActionLook<KubeHomeAction>({ it.destination.icon }) { stringResource(it.destination.label) }

/** Where the Kubernetes home's app-bar actions lead. */
fun KubeHomeNavigation.open(action: KubeHomeAction): () -> Unit = when (action) {
    KubeHomeAction.WORKLOADS -> onWorkloads
    KubeHomeAction.RESOURCES -> onResources
    KubeHomeAction.METRICS -> onMetrics
    KubeHomeAction.HELM -> onHelm
    KubeHomeAction.DATA_SERVICES -> { { onDataServices(null) } }
    KubeHomeAction.CHECKUP -> onCheckup
    KubeHomeAction.API_HEALTH -> onApiHealth
    KubeHomeAction.NETWORK_POLICIES -> onNetworkPolicies
    KubeHomeAction.EVENTS -> onEvents
    KubeHomeAction.SETTINGS -> onSettings
}

/**
 * The Kubernetes home's app-bar actions as arranged in [bar]: its icons, then the rest behind a
 * menu, so the cluster name keeps room on a phone. The menu also leads to arranging them.
 */
@Composable
fun KubeHomeActions(bar: KubeHomeBar, nav: KubeHomeNavigation, onCustomize: () -> Unit) {
    ActionBarActions(
        bar = bar,
        look = kubeHomeActionLook,
        onClick = { nav.open(it)() },
        customizeLabel = stringResource(R.string.kube_home_edit_title),
        onCustomize = onCustomize,
    )
}
