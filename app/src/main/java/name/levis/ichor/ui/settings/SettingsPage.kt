package name.levis.ichor.ui.settings

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.HelpOutline
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.Palette
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.ui.graphics.vector.ImageVector
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows

/** The groups of Settings, in the order of the top-level list; each opens its own page. */
enum class SettingsPage(val route: String, @StringRes val title: Int, @StringRes val summary: Int, val icon: ImageVector) {
    APPEARANCE("appearance", R.string.settings_section_appearance, R.string.settings_page_appearance_summary, Icons.Outlined.Palette),
    SECURITY("security", R.string.settings_section_security, R.string.settings_page_security_summary, Icons.Outlined.Lock),
    PRIVACY("privacy", R.string.settings_section_privacy, R.string.settings_page_privacy_summary, Icons.Outlined.VisibilityOff),
    ALERTS("alerts", R.string.monitor_section, R.string.settings_page_alerts_summary, Icons.Outlined.NotificationsActive),
    AI("ai", R.string.ai_title, R.string.settings_page_ai_summary, Icons.Outlined.AutoAwesome),
    GIT_HOSTS("git-hosts", R.string.git_hosts_title, R.string.git_hosts_desc, Icons.Outlined.Code),
    PLUGINS("plugins", R.string.plugins_title, R.string.settings_page_plugins_summary, Icons.Outlined.Extension),
    KUBERNETES("kubernetes", R.string.settings_kube_section, R.string.settings_page_kubernetes_summary, Icons.Outlined.Terminal),
    CLUSTERS("clusters", R.string.settings_section_clusters, R.string.settings_page_clusters_summary, Icons.Outlined.Storage),
    SUPPORT("support", R.string.settings_section_support, R.string.settings_page_support_summary, Icons.AutoMirrored.Outlined.HelpOutline),
    ABOUT("about", R.string.about_section, R.string.settings_page_about_summary, Icons.Outlined.Info),
    ;

    companion object {
        /** The page a `settings/{page}` route names, or null for an unknown one. */
        fun fromRoute(route: String?): SettingsPage? = entries.firstOrNull { it.route == route }
    }
}

/** Kubernetes needs the kubeconfig feature of the active cluster: hidden otherwise, like its section was. */
fun SettingsPage.visible(summary: ContextSummary?): Boolean = when (this) {
    SettingsPage.KUBERNETES -> summary?.allows(Feature.KUBECONFIG) == true
    else -> true
}

/** The rows of the top-level list for the active cluster. */
fun visibleSettingsPages(summary: ContextSummary?): List<SettingsPage> = SettingsPage.entries.filter { it.visible(summary) }
