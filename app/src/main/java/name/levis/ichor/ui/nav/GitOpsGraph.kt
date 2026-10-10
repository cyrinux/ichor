package name.levis.ichor.ui.nav

import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.argocd.ArgoAppScreen
import name.levis.ichor.ui.argocd.ArgoAppsScreen
import name.levis.ichor.ui.argocd.ArgoWindowsScreen
import name.levis.ichor.ui.flux.DiffTool
import name.levis.ichor.ui.flux.FluxAppScreen
import name.levis.ichor.ui.flux.FluxDiffScreen
import name.levis.ichor.ui.flux.FluxScreen
import name.levis.ichor.ui.kubebrowser.KubeLinks

/** Argo CD and Flux: their apps, sync windows and diffs. */
internal fun NavGraphBuilder.gitOpsGraph(nav: NavHostController, app: TalosApp, kubeLinks: KubeLinks) {
    composable(Routes.ARGO_CD) {
        ArgoAppsScreen(
            onBack = { nav.popBackStack() },
            onApp = { ns, name -> nav.navigate(Routes.argoApp(ns, name)) },
            onWindows = { nav.navigate(Routes.ARGO_WINDOWS) },
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
            onNode = { n, tab -> nav.navigate(Routes.node(n.node, n.hostname, n.role, tab)) },
            onWindows = { nav.navigate(Routes.ARGO_WINDOWS) },
            onDiff = { nav.navigate(Routes.argoDiff(ns, name)) },
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
            onNode = { n, tab -> nav.navigate(Routes.node(n.node, n.hostname, n.role, tab)) },
            onDiff = { nav.navigate(Routes.fluxDiff(kind, ns, name)) },
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
}
