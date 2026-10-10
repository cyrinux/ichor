package name.levis.ichor.ui.nav

import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import name.levis.ichor.TalosApp
import name.levis.ichor.model.LogSource
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.address
import name.levis.ichor.model.containerLogSubtitle
import name.levis.ichor.model.containerLogTitle
import name.levis.ichor.model.sensitive
import name.levis.ichor.ui.capture.CaptureFileScreen
import name.levis.ichor.ui.capture.CaptureScreen
import name.levis.ichor.ui.capture.CapturesScreen
import name.levis.ichor.ui.debug.DebugShellScreen
import name.levis.ichor.ui.debug.ShellKey
import name.levis.ichor.ui.etcd.EtcdScreen
import name.levis.ichor.ui.etcd.ReplaceControlPlaneScreen
import name.levis.ichor.ui.events.EventsScreen
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.hardware.HardwareScreen
import name.levis.ichor.ui.health.HealthScreen
import name.levis.ichor.ui.images.ImagesScreen
import name.levis.ichor.ui.issueconfig.IssueConfigScreen
import name.levis.ichor.ui.kubebrowser.KubeLinks
import name.levis.ichor.ui.kubebrowser.LocalKubeLinks
import name.levis.ichor.ui.kubespan.KubeSpanScreen
import name.levis.ichor.ui.logs.LogsScreen
import name.levis.ichor.ui.machineconfig.MachineConfigScreen
import name.levis.ichor.ui.nettools.NetToolsScreen
import name.levis.ichor.ui.upgrade.ClusterUpgradeScreen
import name.levis.ichor.ui.maintenance.MaintenanceScreen
import name.levis.ichor.ui.network.NetworkScreen
import name.levis.ichor.ui.node.NodeDetailScreen
import name.levis.ichor.ui.node.NodeMenuEntry
import name.levis.ichor.ui.node.PowerAction
import name.levis.ichor.ui.nodes.NodesScreen
import name.levis.ichor.ui.overview.OverviewViewModel
import name.levis.ichor.ui.resources.ResourceDetailScreen
import name.levis.ichor.ui.resources.ResourceListScreen
import name.levis.ichor.ui.resources.ResourceRef
import name.levis.ichor.ui.resources.ResourceTypesScreen
import name.levis.ichor.ui.storage.StorageScreen
import name.levis.ichor.ui.upgrade.UpgradeScreen

/** The Talos screens: a node and its tabs, logs, storage, resources, shells, events, KubeSpan, etcd, health. */
internal fun NavGraphBuilder.talosGraph(nav: NavHostController, app: TalosApp, kubeLinks: KubeLinks) {
    composable(Routes.NODES, arguments = listOf(navArgument("filter") { type = NavType.StringType; defaultValue = "" })) { entry ->
        // The overview's data and refresh: only ever opened from it, so it is below on the stack.
        val home = remember(entry) { nav.getBackStackEntry(Routes.OVERVIEW) }
        NodesScreen(
            initialFilter = NodeFilter.entries.firstOrNull { it.name == entry.arguments?.getString("filter") },
            vm = viewModel(viewModelStoreOwner = home, factory = factory { OverviewViewModel(app.talosRepository, app.configRepository) }),
            onBack = { nav.popBackStack() },
            onNode = { nav.navigate(Routes.node(it.node, it.hostname, it.role)) },
            onNodeAction = nav::openNodeAction,
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
        val host = entry.arguments?.getString("host") ?: addr
        CompositionLocalProvider(LocalKubeLinks provides kubeLinks) {
            NodeDetailScreen(
                node = addr,
                hostname = host,
                role = entry.arguments?.getString("role") ?: "unknown",
                initialTab = entry.arguments?.getInt("tab") ?: 0,
                initialAction = when (entry.arguments?.getString("action")) {
                    "reboot" -> PowerAction.REBOOT
                    "shutdown" -> PowerAction.SHUTDOWN
                    else -> null
                },
                initialCordon = entry.arguments?.getString("action") == "cordon",
                onBack = { nav.popBackStack() },
                onLogs = { service -> nav.navigate(Routes.logs(addr, host, service)) },
                onContainerLogs = { c ->
                    nav.navigate(Routes.containerLogs(addr, host, c.id, containerLogTitle(c), containerLogSubtitle(c)))
                },
                onMenu = { item ->
                    when (item) {
                        NodeMenuEntry.KERNEL_LOG -> Routes.logs(addr, host, null)
                        NodeMenuEntry.EVENTS -> Routes.events(addr, host)
                        NodeMenuEntry.NETWORK -> Routes.network(addr, host)
                        NodeMenuEntry.HARDWARE -> Routes.hardware(addr, host)
                        NodeMenuEntry.IMAGES -> Routes.images(addr, host)
                        NodeMenuEntry.STORAGE -> Routes.storage(addr, host)
                        NodeMenuEntry.RESOURCES -> Routes.resources(addr, host)
                        NodeMenuEntry.DEBUG_SHELL -> Routes.debug(addr, host)
                        NodeMenuEntry.CAPTURE -> Routes.capture(addr, host)
                        NodeMenuEntry.NET_TOOLS -> Routes.netTools(addr, host)
                        NodeMenuEntry.CAPTURES -> Routes.CAPTURES
                        NodeMenuEntry.MACHINE_CONFIG -> Routes.machineConfig(addr, host)
                        NodeMenuEntry.UPGRADE -> Routes.upgrade(addr, host)
                        NodeMenuEntry.MAINTENANCE -> Routes.maintenance(addr, host)
                        NodeMenuEntry.DRAIN -> Routes.maintenance(addr, host, drain = true)
                        // Handled on the node screen (a confirmation, no screen of its own).
                        NodeMenuEntry.CORDON, NodeMenuEntry.RESET -> null
                    }?.let { nav.navigate(it) }
                },
            )
        }
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
    composable(Routes.NETWORK, arguments = nodeArguments()) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        NetworkScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
    }
    composable(Routes.HARDWARE, arguments = nodeArguments()) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        HardwareScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
    }
    composable(Routes.IMAGES, arguments = nodeArguments()) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        ImagesScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
    }
    composable(Routes.CAPTURE, arguments = nodeArguments()) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        CaptureScreen(
            node = addr,
            hostname = entry.arguments?.getString("host") ?: addr,
            onBack = { nav.popBackStack() },
            onCaptures = { nav.navigate(Routes.CAPTURES) },
        )
    }
    composable(Routes.CAPTURES) {
        CapturesScreen(onBack = { nav.popBackStack() }, onOpen = { nav.navigate(Routes.captureFile(it)) })
    }
    composable(Routes.CAPTURE_FILE, arguments = listOf(navArgument("name") { type = NavType.StringType })) { entry ->
        CaptureFileScreen(name = entry.arguments?.getString("name").orEmpty(), onBack = { nav.popBackStack() })
    }
    composable(
        Routes.UPGRADE,
        arguments = nodeArguments() + navArgument("version") { type = NavType.StringType; defaultValue = "" },
    ) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        UpgradeScreen(
            node = addr,
            hostname = entry.arguments?.getString("host") ?: addr,
            initialVersion = entry.arguments?.getString("version").orEmpty(),
            onBack = { nav.popBackStack() },
            onMaintenance = {
                nav.popBackStack()
                nav.navigate(Routes.maintenance(addr, entry.arguments?.getString("host") ?: addr))
            },
        )
    }
    composable(
        Routes.MAINTENANCE,
        arguments = nodeArguments() + navArgument("drain") { type = NavType.BoolType; defaultValue = false },
    ) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        MaintenanceScreen(
            node = addr,
            hostname = entry.arguments?.getString("host") ?: addr,
            drainOnly = entry.arguments?.getBoolean("drain") ?: false,
            onBack = { nav.popBackStack() },
        )
    }
    composable(
        Routes.CLUSTER_UPGRADE,
        arguments = listOf(navArgument("version") { type = NavType.StringType; defaultValue = "" }),
    ) { entry ->
        ClusterUpgradeScreen(initialVersion = entry.arguments?.getString("version").orEmpty(), onBack = { nav.popBackStack() })
    }
    composable(Routes.NET_TOOLS, arguments = nodeArguments()) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        NetToolsScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
    }
    composable(Routes.ISSUE_CONFIG) { IssueConfigScreen(onBack = { nav.popBackStack() }) }
    composable(
        Routes.LOGS,
        arguments = listOf(
            navArgument("addr") { type = NavType.StringType },
            navArgument("host") { type = NavType.StringType },
            navArgument("service") { type = NavType.StringType; defaultValue = "" },
            navArgument("container") { type = NavType.StringType; defaultValue = "" },
            navArgument("title") { type = NavType.StringType; defaultValue = "" },
            navArgument("subtitle") { type = NavType.StringType; defaultValue = "" },
        ),
    ) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        val container = entry.arguments?.getString("container").orEmpty()
        LogsScreen(
            node = addr,
            hostname = entry.arguments?.getString("host") ?: addr,
            source = if (container.isEmpty()) {
                LogSource.Service(entry.arguments?.getString("service")?.takeIf { it.isNotEmpty() })
            } else {
                LogSource.Container(
                    id = container,
                    title = entry.arguments?.getString("title").orEmpty().ifEmpty { container.take(12) },
                    subtitle = entry.arguments?.getString("subtitle").orEmpty(),
                )
            },
            onBack = { nav.popBackStack() },
        )
    }
    composable(Routes.STORAGE, arguments = nodeArguments()) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        StorageScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
    }
    composable(Routes.RESOURCES, arguments = nodeArguments()) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        val host = entry.arguments?.getString("host") ?: addr
        ResourceTypesScreen(
            node = addr,
            hostname = host,
            onBack = { nav.popBackStack() },
            onType = { nav.navigate(Routes.resourceList(addr, host, it.namespace, it.type, it.sensitive)) },
        )
    }
    composable(Routes.RESOURCE_LIST, arguments = resourceArguments()) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        val host = entry.arguments?.getString("host") ?: addr
        val ref = entry.resourceRef(addr)
        ResourceListScreen(
            ref = ref,
            hostname = host,
            onBack = { nav.popBackStack() },
            // The item's own namespace when the list spans several.
            onItem = { nav.navigate(Routes.resource(addr, host, ref.copy(namespace = it.namespace.ifEmpty { ref.namespace }), it.id)) },
        )
    }
    composable(Routes.RESOURCE, arguments = resourceArguments() + navArgument("id") { type = NavType.StringType }) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        ResourceDetailScreen(
            ref = entry.resourceRef(addr),
            id = entry.arguments?.getString("id").orEmpty(),
            onBack = { nav.popBackStack() },
        )
    }
    composable(
        Routes.DEBUG,
        arguments = listOf(
            navArgument("addr") { type = NavType.StringType },
            navArgument("host") { type = NavType.StringType },
            navArgument("ctx") { type = NavType.StringType; defaultValue = "" },
        ),
    ) { entry ->
        val addr = entry.arguments?.getString("addr").orEmpty()
        val context = entry.arguments?.getString("ctx").orEmpty()
            .ifEmpty { app.configRepository.config.value?.activeContext.orEmpty() }
        DebugShellScreen(
            key = ShellKey(context, addr),
            hostname = entry.arguments?.getString("host") ?: addr,
            onBack = { nav.popBackStack() },
        )
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
    composable(Routes.KUBESPAN) {
        KubeSpanScreen(
            onBack = { nav.popBackStack() },
            // Only a node the talosconfig targets has a detail screen.
            onNode = { n -> if (n.node.isNotBlank()) nav.navigate(Routes.node(n.node, n.hostname, n.role)) },
        )
    }
    composable(Routes.ETCD) {
        EtcdScreen(
            onBack = { nav.popBackStack() },
            onReplace = { m -> nav.navigate(Routes.replaceControlPlane(m.id, m.address, m.hostname)) },
        )
    }
    composable(
        Routes.REPLACE_CONTROL_PLANE,
        arguments = listOf(
            navArgument("member") { type = NavType.StringType },
            navArgument("addr") { type = NavType.StringType },
            navArgument("host") { type = NavType.StringType },
        ),
    ) { entry ->
        ReplaceControlPlaneScreen(
            memberId = entry.arguments?.getString("member").orEmpty(),
            node = entry.arguments?.getString("addr").orEmpty(),
            hostname = entry.arguments?.getString("host").orEmpty(),
            onBack = { nav.popBackStack() },
            onOpenConfig = { addr, host -> nav.navigate(Routes.machineConfig(addr, host)) },
        )
    }
    composable(Routes.HEALTH) {
        HealthScreen(onBack = { nav.popBackStack() }, onDiagnose = { note -> nav.navigate(Routes.diagnosis(note)) })
    }
}

/** The addr/host arguments of a per-node screen. */
private fun nodeArguments() = listOf(
    navArgument("addr") { type = NavType.StringType },
    navArgument("host") { type = NavType.StringType },
)

/** The arguments of the resource browser's list and detail screens. */
private fun resourceArguments() = nodeArguments() + listOf(
    navArgument("ns") { type = NavType.StringType; defaultValue = "" },
    navArgument("type") { type = NavType.StringType },
    navArgument("sensitive") { type = NavType.BoolType; defaultValue = false },
)

private fun NavBackStackEntry.resourceRef(addr: String) = ResourceRef(
    node = addr,
    namespace = arguments?.getString("ns").orEmpty(),
    type = arguments?.getString("type").orEmpty(),
    sensitive = arguments?.getBoolean("sensitive") ?: false,
)
