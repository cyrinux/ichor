package name.levis.ichor.ui.nav

import android.os.Bundle
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.argocd.ArgoAppScreen
import name.levis.ichor.ui.argocd.ArgoListDetail
import name.levis.ichor.ui.argocd.ArgoWindowsScreen
import name.levis.ichor.ui.flux.DiffTool
import name.levis.ichor.ui.flux.FluxAppScreen
import name.levis.ichor.ui.flux.FluxDiffScreen
import name.levis.ichor.ui.flux.FluxScreen
import name.levis.ichor.ui.gitops.SOURCE_TOOL_ARGO
import name.levis.ichor.ui.gitops.SOURCE_TOOL_FLUX
import name.levis.ichor.ui.gitops.SourceBrowserScreen
import name.levis.ichor.ui.gitops.SourceFileScreen
import name.levis.ichor.ui.gitops.SourceTarget
import name.levis.ichor.ui.kubebrowser.KubeLinks
import name.levis.ichor.ui.settings.SettingsPage

/** Argo CD and Flux: their apps, sync windows and diffs. */
internal fun NavGraphBuilder.gitOpsGraph(nav: NavHostController, app: TalosApp, kubeLinks: KubeLinks) {
    composable(Routes.ARGO_CD) {
        ArgoListDetail(
            onBack = { nav.popBackStack() },
            onOpenApp = { ns, name -> nav.navigate(Routes.argoApp(ns, name)) },
            onWindows = { nav.navigate(Routes.ARGO_WINDOWS) },
            onNode = { n, tab -> nav.navigate(Routes.nodeOrTab(n.node, n.hostname, n.role, tab)) },
            onDiff = { ns, name -> nav.navigate(Routes.argoDiff(ns, name)) },
            onSource = { ns, name, idx -> nav.navigate(Routes.gitOpsSource(SOURCE_TOOL_ARGO, "Application", ns, name, idx)) },
        )
    }
    composable(Routes.ARGO_WINDOWS) {
        ArgoWindowsScreen(onBack = { nav.popBackStack() })
    }
    composable(
        Routes.ARGO_APP,
        arguments = listOf(
            navArgument("ns") { type = NavType.StringType; defaultValue = "" },
            navArgument("name") { type = NavType.StringType; defaultValue = "" },
            navArgument("sync") { type = NavType.BoolType; defaultValue = false },
        ),
    ) { entry ->
        val ns = entry.arguments?.getString("ns").orEmpty()
        val name = entry.arguments?.getString("name").orEmpty()
        ArgoAppScreen(
            namespace = ns,
            name = name,
            initialSync = entry.arguments?.getBoolean("sync") == true,
            onBack = { nav.popBackStack() },
            onNode = { n, tab -> nav.navigate(Routes.nodeOrTab(n.node, n.hostname, n.role, tab)) },
            onWindows = { nav.navigate(Routes.ARGO_WINDOWS) },
            onDiff = { nav.navigate(Routes.argoDiff(ns, name)) },
            onSource = { idx -> nav.navigate(Routes.gitOpsSource(SOURCE_TOOL_ARGO, "Application", ns, name, idx)) },
        )
    }
    composable(
        Routes.ARGO_DIFF,
        arguments = listOf(
            navArgument("ns") { type = NavType.StringType; defaultValue = "" },
            navArgument("name") { type = NavType.StringType; defaultValue = "" },
        ),
    ) { entry ->
        FluxDiffScreen(
            kind = "Application",
            namespace = entry.arguments?.getString("ns").orEmpty(),
            name = entry.arguments?.getString("name").orEmpty(),
            onBack = { nav.popBackStack() },
            tool = DiffTool.ARGO,
        )
    }
    composable(Routes.FLUX) {
        FluxScreen(onBack = { nav.popBackStack() }, onApp = { kind, ns, name -> nav.navigate(Routes.fluxApp(kind, ns, name)) })
    }
    composable(
        Routes.FLUX_APP,
        arguments = listOf(
            navArgument("kind") { type = NavType.StringType; defaultValue = "" },
            navArgument("ns") { type = NavType.StringType; defaultValue = "" },
            navArgument("name") { type = NavType.StringType; defaultValue = "" },
            navArgument("reconcile") { type = NavType.BoolType; defaultValue = false },
        ),
    ) { entry ->
        val kind = entry.arguments?.getString("kind").orEmpty()
        val ns = entry.arguments?.getString("ns").orEmpty()
        val name = entry.arguments?.getString("name").orEmpty()
        FluxAppScreen(
            kind = kind,
            namespace = ns,
            name = name,
            initialReconcile = entry.arguments?.getBoolean("reconcile") == true,
            onBack = { nav.popBackStack() },
            onNode = { n, tab -> nav.navigate(Routes.nodeOrTab(n.node, n.hostname, n.role, tab)) },
            onDiff = { nav.navigate(Routes.fluxDiff(kind, ns, name)) },
            onApp = { other -> nav.navigate(Routes.fluxApp(other.kind, other.namespace, other.name)) },
            onSource = { idx -> nav.navigate(Routes.gitOpsSource(SOURCE_TOOL_FLUX, kind, ns, name, idx)) },
        )
    }
    composable(
        Routes.FLUX_DIFF,
        arguments = listOf(
            navArgument("kind") { type = NavType.StringType; defaultValue = "" },
            navArgument("ns") { type = NavType.StringType; defaultValue = "" },
            navArgument("name") { type = NavType.StringType; defaultValue = "" },
        ),
    ) { entry ->
        FluxDiffScreen(
            kind = entry.arguments?.getString("kind").orEmpty(),
            namespace = entry.arguments?.getString("ns").orEmpty(),
            name = entry.arguments?.getString("name").orEmpty(),
            onBack = { nav.popBackStack() },
        )
    }
    sourceGraph(nav)
}

/** The source browser of an Argo CD or Flux app and its file view. */
private fun NavGraphBuilder.sourceGraph(nav: NavHostController) {
    val targetArgs = listOf(
        navArgument("tool") { type = NavType.StringType; defaultValue = "" },
        navArgument("kind") { type = NavType.StringType; defaultValue = "" },
        navArgument("ns") { type = NavType.StringType; defaultValue = "" },
        navArgument("name") { type = NavType.StringType; defaultValue = "" },
        navArgument("idx") { type = NavType.IntType; defaultValue = 0 },
    )
    val target = { args: Bundle? ->
        SourceTarget(
            tool = args?.getString("tool").orEmpty(),
            kind = args?.getString("kind").orEmpty(),
            namespace = args?.getString("ns").orEmpty(),
            name = args?.getString("name").orEmpty(),
            index = args?.getInt("idx") ?: 0,
        )
    }
    val onAddToken = { nav.navigate(Routes.settingsPage(SettingsPage.GIT_HOSTS)) }
    composable(
        Routes.GITOPS_SOURCE,
        arguments = targetArgs + navArgument("dir") { type = NavType.StringType; defaultValue = "" },
    ) { entry ->
        val t = target(entry.arguments)
        SourceBrowserScreen(
            target = t,
            startDir = entry.arguments?.getString("dir")?.takeIf { it.isNotEmpty() },
            onBack = { nav.popBackStack() },
            onFile = { path -> nav.navigate(Routes.gitOpsFile(t.tool, t.kind, t.namespace, t.name, t.index, path)) },
            onAddToken = onAddToken,
        )
    }
    composable(
        Routes.GITOPS_FILE,
        arguments = targetArgs + navArgument("path") { type = NavType.StringType; defaultValue = "" },
    ) { entry ->
        SourceFileScreen(
            target = target(entry.arguments),
            path = entry.arguments?.getString("path").orEmpty(),
            onBack = { nav.popBackStack() },
            onAddToken = onAddToken,
        )
    }
}
