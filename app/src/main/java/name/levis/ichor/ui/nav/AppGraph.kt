package name.levis.ichor.ui.nav

import name.levis.ichor.selectCluster
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.activity.ActivityScreen
import name.levis.ichor.ui.imagescan.SavedScansScreen
import name.levis.ichor.ui.changelog.ChangelogScreen
import name.levis.ichor.ui.diagnosis.DiagnosisScreen
import name.levis.ichor.ui.funding.FundingScreen
import name.levis.ichor.ui.integrations.IntegrationsScreen
import name.levis.ichor.ui.kubebrowser.KubeLinks
import name.levis.ichor.ui.runbooks.RunbookPlanScreen
import name.levis.ichor.ui.runbooks.RunbookRunScreen
import name.levis.ichor.ui.runbooks.RunbooksScreen
import name.levis.ichor.ui.search.SearchScreen
import name.levis.ichor.ui.settings.LicensesScreen
import name.levis.ichor.ui.settings.SettingsActions
import name.levis.ichor.ui.settings.SettingsPage
import name.levis.ichor.ui.settings.SettingsPageScreen
import name.levis.ichor.ui.settings.SettingsScreen
import name.levis.ichor.ui.settings.SettingsStores
import name.levis.ichor.ui.settings.SupportedIntegrationsScreen
import name.levis.ichor.ui.support.SupportBundleScreen
import name.levis.ichor.ui.tour.replayDemoTour

/**
 * The app's own screens: settings, diagnosis, changelog, licenses, funding, support, and the
 * search across clusters, whose results [openTarget] opens like share links.
 */
internal fun NavGraphBuilder.appGraph(nav: NavHostController, app: TalosApp, kubeLinks: KubeLinks, openTarget: (ShareTarget) -> Unit) {
    composable(Routes.SEARCH) {
        // A screen of the active cluster: opened over the search, so Back returns to it.
        SearchScreen(onBack = { nav.popBackStack() }, onOpen = openTarget, onOpenRoute = { nav.navigate(it) { launchSingleTop = true } })
    }
    composable(Routes.INSIGHTS) {
        name.levis.ichor.ui.insights.InsightsScreen(
            onBack = { nav.popBackStack() },
            onFix = { addr, host, fix -> nav.navigate(Routes.machineConfig(addr, host, fix)) },
        )
    }
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
        val cluster = entry.arguments?.getString("cluster").orEmpty()
        ActivityScreen(cluster = cluster, onBack = { nav.popBackStack() }, onScanReports = { nav.navigate(Routes.scanReports(cluster)) })
    }
    composable(Routes.RUNBOOKS) {
        RunbooksScreen(onBack = { nav.popBackStack() }, onOpen = { nav.navigate(Routes.runbookPlan(it)) })
    }
    composable(Routes.RUNBOOK_PLAN, arguments = listOf(navArgument("id") { type = NavType.StringType; defaultValue = "" })) { entry ->
        RunbookPlanScreen(
            id = entry.arguments?.getString("id").orEmpty(),
            onBack = { nav.popBackStack() },
            onRunning = { nav.navigate(Routes.RUNBOOK_RUN) { popUpTo(Routes.RUNBOOK_PLAN) { inclusive = true } } },
        )
    }
    composable(Routes.RUNBOOK_RUN) {
        RunbookRunScreen(onBack = { nav.popBackStack() }, onActivity = { nav.navigate(Routes.activity(it)) })
    }
    composable(Routes.SCAN_REPORTS, arguments = listOf(navArgument("cluster") { type = NavType.StringType; defaultValue = "" })) { entry ->
        SavedScansScreen(cluster = entry.arguments?.getString("cluster").orEmpty(), onBack = { nav.popBackStack() })
    }
    composable(Routes.SETTINGS) {
        SettingsScreen(
            configs = app.configRepository,
            onBack = { nav.popBackStack() },
            onOpen = { page -> nav.navigate(Routes.settingsPage(page)) },
        )
    }
    composable(Routes.SETTINGS_PAGE, arguments = listOf(navArgument("page") { type = NavType.StringType })) { entry ->
        val page = SettingsPage.fromRoute(entry.arguments?.getString("page")) ?: SettingsPage.APPEARANCE
        SettingsPageScreen(
            page = page,
            stores = SettingsStores(app.configRepository, app.uiPreferences, app.appLock, app.talosRepository),
            actions = settingsActions(nav, app),
            onBack = { nav.popBackStack() },
        )
    }
}

private fun settingsActions(nav: NavHostController, app: TalosApp) = SettingsActions(
    onReimport = { nav.navigate(Routes.IMPORT) },
    onIssueConfig = { nav.navigate(Routes.ISSUE_CONFIG) },
    onRotateCA = { nav.navigate(Routes.ROTATE_CA) },
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
    onReplayTour = {
        replayDemoTour(app.configRepository, app.uiPreferences, app::selectCluster)
        nav.resetTo(Routes.OVERVIEW)
    },
)
