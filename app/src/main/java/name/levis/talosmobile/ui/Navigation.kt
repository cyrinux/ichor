package name.levis.talosmobile.ui

import android.net.Uri
import androidx.compose.runtime.Composable
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.ui.etcd.EtcdScreen
import name.levis.talosmobile.ui.health.HealthScreen
import name.levis.talosmobile.ui.importconfig.ImportScreen
import name.levis.talosmobile.ui.logs.LogsScreen
import name.levis.talosmobile.ui.node.NodeDetailScreen
import name.levis.talosmobile.ui.overview.OverviewScreen
import name.levis.talosmobile.ui.settings.SettingsScreen

private object Routes {
    const val IMPORT = "import"
    const val OVERVIEW = "overview"
    const val NODE = "node?addr={addr}&host={host}&role={role}"
    const val LOGS = "logs?addr={addr}&host={host}&service={service}"
    const val ETCD = "etcd"
    const val HEALTH = "health"
    const val SETTINGS = "settings"

    /** Empty [service] means the kernel log. */
    fun logs(addr: String, host: String, service: String?) =
        "logs?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&service=${Uri.encode(service.orEmpty())}"

    fun node(addr: String, host: String, role: String) =
        "node?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&role=${Uri.encode(role)}"
}

@Composable
fun Navigation(app: TalosApp, startWithImport: Boolean) {
    val nav = rememberNavController()

    NavHost(navController = nav, startDestination = if (startWithImport) Routes.IMPORT else Routes.OVERVIEW) {
        composable(Routes.IMPORT) {
            ImportScreen(onImported = {
                app.launchSync(runNow = true)
                nav.resetTo(Routes.OVERVIEW)
            })
        }
        composable(Routes.OVERVIEW) {
            OverviewScreen(
                onNode = { nav.navigate(Routes.node(it.node, it.hostname, it.role)) },
                onEtcd = { nav.navigate(Routes.ETCD) },
                onHealth = { nav.navigate(Routes.HEALTH) },
                onSettings = { nav.navigate(Routes.SETTINGS) },
            )
        }
        composable(
            Routes.NODE,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType },
                navArgument("host") { type = NavType.StringType },
                navArgument("role") { type = NavType.StringType; defaultValue = "unknown" },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            NodeDetailScreen(
                node = addr,
                hostname = entry.arguments?.getString("host") ?: addr,
                role = entry.arguments?.getString("role") ?: "unknown",
                onBack = { nav.popBackStack() },
                onLogs = { service -> nav.navigate(Routes.logs(addr, entry.arguments?.getString("host") ?: addr, service)) },
            )
        }
        composable(
            Routes.LOGS,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType },
                navArgument("host") { type = NavType.StringType },
                navArgument("service") { type = NavType.StringType; defaultValue = "" },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            LogsScreen(
                node = addr,
                hostname = entry.arguments?.getString("host") ?: addr,
                service = entry.arguments?.getString("service")?.takeIf { it.isNotEmpty() },
                onBack = { nav.popBackStack() },
            )
        }
        composable(Routes.ETCD) { EtcdScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.HEALTH) { HealthScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.SETTINGS) {
            SettingsScreen(
                configs = app.configRepository,
                uiPreferences = app.uiPreferences,
                appLock = app.appLock,
                talos = app.talosRepository,
                onBack = { nav.popBackStack() },
                onReimport = { nav.navigate(Routes.IMPORT) },
                onCleared = {
                    app.launchSync(runNow = true)
                    nav.resetTo(Routes.IMPORT)
                },
            )
        }
    }
}

/** Navigates to [route] and drops everything else from the back stack. */
private fun NavHostController.resetTo(route: String) {
    navigate(route) {
        popUpTo(graph.id) { inclusive = true }
        launchSingleTop = true
    }
}
