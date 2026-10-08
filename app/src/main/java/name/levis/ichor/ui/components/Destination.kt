package name.levis.ichor.ui.components

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.HealthAndSafety
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Stream
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.ui.graphics.vector.ImageVector
import name.levis.ichor.R

/**
 * A screen several app bars and tool cards lead to (the Talos overview's, the Kubernetes
 * home's, the Kubernetes screen's): one icon and one label wherever it is offered.
 */
enum class Destination(val icon: ImageVector, @StringRes val label: Int) {
    WORKLOADS(Icons.Outlined.Widgets, R.string.overview_action_workloads),
    METRICS(Icons.Outlined.QueryStats, R.string.metrics_title),
    RESOURCES(Icons.Outlined.Category, R.string.kb_title),
    HELM(Icons.Outlined.Inventory2, R.string.kb_helm_title),
    DATA_SERVICES(Icons.Outlined.Storage, R.string.data_services_title),
    CHECKUP(Icons.Outlined.HealthAndSafety, R.string.checkup_title),
    API_HEALTH(Icons.Outlined.MonitorHeart, R.string.apihealth_title),
    NETWORK_POLICIES(Icons.Outlined.Policy, R.string.netpol_title),
    FLOWS(Icons.Outlined.Stream, R.string.flows_title),
    EVENTS(Icons.Outlined.Timeline, R.string.overview_action_events),
    SETTINGS(Icons.Outlined.Settings, R.string.overview_action_settings),
}
