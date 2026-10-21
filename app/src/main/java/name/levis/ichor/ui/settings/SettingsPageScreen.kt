package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.TalosApp
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.UiPreferences
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.security.AppLock
import name.levis.ichor.security.lockRequired
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.pageContent

/** Where the settings pages lead: the screens they open, and what follows deleting every cluster. */
class SettingsActions(
    val onReimport: () -> Unit,
    val onIssueConfig: () -> Unit,
    val onSupportBundle: () -> Unit,
    val onActivity: () -> Unit,
    val onIntegrations: () -> Unit,
    val onChangelog: () -> Unit,
    val onLicenses: () -> Unit,
    val onSupportedIntegrations: () -> Unit,
    val onFunding: () -> Unit,
    val onCleared: () -> Unit,
    /** Shows the demo tour again on the Talos demo home (adding the demo if it was removed). */
    val onReplayTour: suspend () -> Unit,
)

/** What a settings page reads and writes: the same stores the single Settings screen used. */
class SettingsStores(
    val configs: ConfigRepository,
    val uiPreferences: UiPreferences,
    val appLock: AppLock,
    val talos: TalosRepository,
)

/** One group of Settings on its own page, titled like its row in the list. */
@Composable
fun SettingsPageScreen(page: SettingsPage, stores: SettingsStores, actions: SettingsActions, onBack: () -> Unit) {
    val config by stores.configs.config.collectAsStateWithLifecycle()
    val summary = config?.activeSummary
    val app = LocalContext.current.applicationContext as TalosApp
    val snackbar = remember { SnackbarHostState() }
    SettingsPageScaffold(page, snackbar, onBack) {
        when (page) {
            SettingsPage.APPEARANCE -> AppearanceSection(stores.uiPreferences)
            SettingsPage.SECURITY -> {
                KeyFallbackNotice(stores.configs, snackbar)
                SecuritySection(stores.appLock, stores.uiPreferences, required = lockRequired(config?.summary?.contexts.orEmpty()))
            }
            SettingsPage.PRIVACY -> PrivacySection(app)
            SettingsPage.ALERTS -> MonitoringSection(app)
            SettingsPage.AI -> AiSection(app)
            SettingsPage.GIT_HOSTS -> GitHostsSection(app)
            SettingsPage.PLUGINS -> PluginsSection(app.pluginRepository)
            // The row is hidden without the feature; a stale back stack entry shows an empty page.
            SettingsPage.KUBERNETES ->
                summary?.takeIf { it.allows(Feature.KUBECONFIG) }?.let { KubeconfigSection(stores.talos, stores.appLock, it) }
            SettingsPage.CLUSTERS ->
                ClustersSection(stores.configs, actions.onReimport, actions.onIssueConfig, actions.onCleared)
            SettingsPage.SUPPORT ->
                SupportSection(summary, actions.onSupportBundle, actions.onActivity, actions.onIntegrations)
            SettingsPage.ABOUT -> {
                AboutSection(actions.onChangelog, actions.onLicenses, actions.onSupportedIntegrations, actions.onFunding, actions.onReplayTour)
                UpdatesSection { app.updateManager }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SettingsPageScaffold(page: SettingsPage, snackbar: SnackbarHostState, onBack: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(page.title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
        snackbarHost = { SnackbarHost(snackbar) },
    ) { padding ->
        Column(
            Modifier.pageContent(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
            content = content,
        )
    }
}
