package name.levis.ichor.ui.nav

import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.activity.ActivityScreen
import name.levis.ichor.ui.changelog.ChangelogScreen
import name.levis.ichor.ui.diagnosis.DiagnosisScreen
import name.levis.ichor.ui.funding.FundingScreen
import name.levis.ichor.ui.integrations.IntegrationsScreen
import name.levis.ichor.ui.kubebrowser.KubeLinks
import name.levis.ichor.ui.settings.LicensesScreen
import name.levis.ichor.ui.settings.SettingsScreen
import name.levis.ichor.ui.settings.SupportedIntegrationsScreen
import name.levis.ichor.ui.support.SupportBundleScreen

/** The app's own screens: settings, diagnosis, changelog, licenses, funding, support. */
internal fun NavGraphBuilder.appGraph(nav: NavHostController, app: TalosApp, kubeLinks: KubeLinks) {
    composable(Routes.INSIGHTS) { name.levis.ichor.ui.insights.InsightsScreen(onBack = { nav.popBackStack() }) }
    composable(Routes.CHANGELOG) { ChangelogScreen(onBack = { nav.popBackStack() }) }
    composable(Routes.LICENSES) { LicensesScreen(onBack = { nav.popBackStack() }) }
    composable(Routes.SUPPORTED_INTEGRATIONS) {
        SupportedIntegrationsScreen(configs = app.configRepository, kube = app.kubeRepository, onBack = { nav.popBackStack() })
    }
    composable(Routes.FUNDING) { FundingScreen(onBack = { nav.popBackStack() }) }
    composable(Routes.SUPPORT_BUNDLE) { SupportBundleScreen(onBack = { nav.popBackStack() }) }
    composable(Routes.INTEGRATIONS) { IntegrationsScreen(onBack = { nav.popBackStack() }) }
    composable(
        Routes.DIAGNOSIS,
        arguments = listOf(navArgument("note") { type = NavType.StringType; defaultValue = "" }),
    ) { entry ->
        DiagnosisScreen(
            initialNote = entry.arguments?.getString("note").orEmpty(),
            onBack = { nav.popBackStack() },
            onSettings = { nav.navigate(Routes.SETTINGS) },
        )
    }
    composable(Routes.ACTIVITY, arguments = listOf(navArgument("cluster") { type = NavType.StringType; defaultValue = "" })) { entry ->
        ActivityScreen(cluster = entry.arguments?.getString("cluster").orEmpty(), onBack = { nav.popBackStack() })
    }
    composable(Routes.SETTINGS) {
        SettingsScreen(
            configs = app.configRepository,
            uiPreferences = app.uiPreferences,
            appLock = app.appLock,
            talos = app.talosRepository,
            onBack = { nav.popBackStack() },
            onReimport = { nav.navigate(Routes.IMPORT) },
            onIssueConfig = { nav.navigate(Routes.ISSUE_CONFIG) },
            onSupportBundle = { nav.navigate(Routes.SUPPORT_BUNDLE) },
            onActivity = { nav.navigate(Routes.activity()) },
            onIntegrations = { nav.navigate(Routes.INTEGRATIONS) },
            onChangelog = { nav.navigate(Routes.CHANGELOG) },
            onLicenses = { nav.navigate(Routes.LICENSES) },
            onSupportedIntegrations = { nav.navigate(Routes.SUPPORTED_INTEGRATIONS) },
            onFunding = { nav.navigate(Routes.FUNDING) },
            onCleared = {
                app.launchSync(runNow = true)
                nav.resetTo(Routes.IMPORT)
            },
        )
    }
}
