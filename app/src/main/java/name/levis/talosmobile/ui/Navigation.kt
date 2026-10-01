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
import name.levis.talosmobile.ui.debug.DebugShellScreen
import name.levis.talosmobile.ui.etcd.EtcdScreen
import name.levis.talosmobile.ui.events.EventsScreen
import name.levis.talosmobile.ui.health.HealthScreen
import name.levis.talosmobile.ui.importconfig.ImportScreen
import name.levis.talosmobile.ui.kubespan.KubeSpanScreen
import name.levis.talosmobile.ui.logs.LogsScreen
import name.levis.talosmobile.ui.machineconfig.MachineConfigScreen
import name.levis.talosmobile.ui.node.NodeDetailScreen
import name.levis.talosmobile.ui.node.PowerAction
import name.levis.talosmobile.ui.overview.NodeAction
import name.levis.talosmobile.ui.overview.OverviewScreen
import name.levis.talosmobile.ui.settings.SettingsScreen

private object Routes {
    const val IMPORT = "import"
    const val OVERVIEW = "overview"
    const val NODE = "node?addr={addr}&host={host}&role={role}&tab={tab}&action={action}"
    const val LOGS = "logs?addr={addr}&host={host}&service={service}"
    const val ETCD = "etcd"
    const val KUBESPAN = "kubespan"
    const val DEBUG = "debug?addr={addr}&host={host}"
    const val MACHINE_CONFIG = "machineconfig?addr={addr}&host={host}"

    /** Empty addr: events of every node of the context. */
    const val EVENTS = "events?addr={addr}&host={host}"

    fun events(addr: String = "", host: String = "") = "events?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun machineConfig(addr: String, host: String) = "machineconfig?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun debug(addr: String, host: String) = "debug?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"
    const val HEALTH = "health"
    const val SETTINGS = "settings"

    /** Empty [service] means the kernel log. */
    fun logs(addr: String, host: String, service: String?) =
        "logs?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&service=${Uri.encode(service.orEmpty())}"

    /** [tab]: 0 services, 1 resources, 2 live, 3 processes, 4 pods; [action]: "reboot"/"shutdown" opens its confirmation. */
    fun node(addr: String, host: String, role: String, tab: Int = 0, action: String = "") =
        "node?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&role=${Uri.encode(role)}&tab=$tab&action=$action"
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
                onNodeAction = { n, action ->
                    when (action) {
                        NodeAction.LIVE -> nav.navigate(Routes.node(n.node, n.hostname, n.role, tab = 2))
                        NodeAction.SERVICES -> nav.navigate(Routes.node(n.node, n.hostname, n.role))
                        NodeAction.KERNEL_LOG -> nav.navigate(Routes.logs(n.node, n.hostname, null))
                        NodeAction.SHELL -> nav.navigate(Routes.debug(n.node, n.hostname))
                        NodeAction.REBOOT -> nav.navigate(Routes.node(n.node, n.hostname, n.role, action = "reboot"))
                        NodeAction.SHUTDOWN -> nav.navigate(Routes.node(n.node, n.hostname, n.role, action = "shutdown"))
                    }
                },
                onEtcd = { nav.navigate(Routes.ETCD) },
                onKubeSpan = { nav.navigate(Routes.KUBESPAN) },
                onHealth = { nav.navigate(Routes.HEALTH) },
                onEvents = { nav.navigate(Routes.events()) },
                onSettings = { nav.navigate(Routes.SETTINGS) },
            )
        }
        composable(
            Routes.NODE,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType },
                navArgument("host") { type = NavType.StringType },
                navArgument("role") { type = NavType.StringType; defaultValue = "unknown" },
                navArgument("tab") { type = NavType.IntType; defaultValue = 0 },
                navArgument("action") { type = NavType.StringType; defaultValue = "" },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            NodeDetailScreen(
                node = addr,
                hostname = entry.arguments?.getString("host") ?: addr,
                role = entry.arguments?.getString("role") ?: "unknown",
                initialTab = entry.arguments?.getInt("tab") ?: 0,
                initialAction = when (entry.arguments?.getString("action")) {
                    "reboot" -> PowerAction.REBOOT
                    "shutdown" -> PowerAction.SHUTDOWN
                    else -> null
                },
                onBack = { nav.popBackStack() },
                onLogs = { service -> nav.navigate(Routes.logs(addr, entry.arguments?.getString("host") ?: addr, service)) },
                onDebugShell = { nav.navigate(Routes.debug(addr, entry.arguments?.getString("host") ?: addr)) },
                onMachineConfig = { nav.navigate(Routes.machineConfig(addr, entry.arguments?.getString("host") ?: addr)) },
                onEvents = { nav.navigate(Routes.events(addr, entry.arguments?.getString("host") ?: addr)) },
            )
        }
        composable(
            Routes.MACHINE_CONFIG,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType },
                navArgument("host") { type = NavType.StringType },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            MachineConfigScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
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
        composable(
            Routes.DEBUG,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType },
                navArgument("host") { type = NavType.StringType },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            DebugShellScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
        }
        composable(
            Routes.EVENTS,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType; defaultValue = "" },
                navArgument("host") { type = NavType.StringType; defaultValue = "" },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty().ifEmpty { null }
            EventsScreen(
                node = addr,
                hostname = addr?.let { entry.arguments?.getString("host")?.ifEmpty { null } ?: it },
                onBack = { nav.popBackStack() },
            )
        }
        composable(Routes.KUBESPAN) { KubeSpanScreen(onBack = { nav.popBackStack() }) }
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
