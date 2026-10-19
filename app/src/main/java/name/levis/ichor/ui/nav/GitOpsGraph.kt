package name.levis.ichor.ui.nav

import android.os.Bundle
import androidx.navigation.NamedNavArgument
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import name.levis.ichor.TalosApp
import name.levis.ichor.model.decodeQuickEdit
import name.levis.ichor.model.encodeQuickEdit
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
import name.levis.ichor.ui.gitops.SourceEditScreen
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
            onDiffOf = { focus -> nav.navigate(Routes.argoDiff(ns, name, focus)) },
        )
    }
    composable(
        Routes.ARGO_DIFF,
        arguments = listOf(
            navArgument("ns") { type = NavType.StringType; defaultValue = "" },
            navArgument("name") { type = NavType.StringType; defaultValue = "" },
            navArgument("focus") { type = NavType.StringType; defaultValue = "" },
        ),
    ) { entry ->
        val ns = entry.arguments?.getString("ns").orEmpty()
        val name = entry.arguments?.getString("name").orEmpty()
        FluxDiffScreen(
            focus = entry.arguments?.getString("focus").orEmpty(),
            kind = "Application",
            namespace = ns,
            name = name,
            onBack = { nav.popBackStack() },
            tool = DiffTool.ARGO,
            onOpenSource = { idx, path, line -> nav.navigate(Routes.gitOpsFile(SOURCE_TOOL_ARGO, "Application", ns, name, idx, path, line)) },
            onKeepLive = { idx, path, edit -> nav.navigate(Routes.gitOpsEdit(SOURCE_TOOL_ARGO, "Application", ns, name, idx, path, encodeQuickEdit(edit))) },
            onAddToken = { nav.navigate(Routes.settingsPage(SettingsPage.GIT_HOSTS)) },
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
            navArgument("focus") { type = NavType.StringType; defaultValue = "" },
        ),
    ) { entry ->
        val kind = entry.arguments?.getString("kind").orEmpty()
        val ns = entry.arguments?.getString("ns").orEmpty()
        val name = entry.arguments?.getString("name").orEmpty()
        FluxDiffScreen(
            focus = entry.arguments?.getString("focus").orEmpty(),
            kind = kind,
            namespace = ns,
            name = name,
            onBack = { nav.popBackStack() },
            onOpenSource = { idx, path, line -> nav.navigate(Routes.gitOpsFile(SOURCE_TOOL_FLUX, kind, ns, name, idx, path, line)) },
            onKeepLive = { idx, path, edit -> nav.navigate(Routes.gitOpsEdit(SOURCE_TOOL_FLUX, kind, ns, name, idx, path, encodeQuickEdit(edit))) },
            onAddToken = { nav.navigate(Routes.settingsPage(SettingsPage.GIT_HOSTS)) },
        )
    }
    sourceGraph(nav)
}

/** The route of the file [path] of [t]'s files, [line] highlighted (0 for none). */
private fun sourceFileRoute(t: SourceTarget, path: String, line: Int = 0): String = when (t) {
    is SourceTarget.App -> Routes.gitOpsFile(t.tool, t.kind, t.namespace, t.name, t.index, path, line)
    SourceTarget.Repo -> Routes.repoFile(path, line)
}

/** The route of the editor of [t]'s file [path], with a [quick] edit's JSON ("" for none). */
private fun sourceEditRoute(t: SourceTarget, path: String, quick: String = ""): String = when (t) {
    is SourceTarget.App -> Routes.gitOpsEdit(t.tool, t.kind, t.namespace, t.name, t.index, path, quick)
    SourceTarget.Repo -> Routes.repoEdit(path, quick)
}

/**
 * The source browser, file view and editor, for an Argo CD or Flux app's source and for the
 * active cluster's repository.
 */
private fun NavGraphBuilder.sourceGraph(nav: NavHostController) {
    val appArgs = listOf(
        navArgument("tool") { type = NavType.StringType; defaultValue = "" },
        navArgument("kind") { type = NavType.StringType; defaultValue = "" },
        navArgument("ns") { type = NavType.StringType; defaultValue = "" },
        navArgument("name") { type = NavType.StringType; defaultValue = "" },
        navArgument("idx") { type = NavType.IntType; defaultValue = 0 },
    )
    val app = { args: Bundle? ->
        SourceTarget.App(
            tool = args?.getString("tool").orEmpty(),
            kind = args?.getString("kind").orEmpty(),
            namespace = args?.getString("ns").orEmpty(),
            name = args?.getString("name").orEmpty(),
            index = args?.getInt("idx") ?: 0,
        )
    }
    sourceScreens(nav, Routes.GITOPS_SOURCE, Routes.GITOPS_FILE, Routes.GITOPS_EDIT, appArgs, app)
    sourceScreens(nav, Routes.REPO_SOURCE, Routes.REPO_FILE, Routes.REPO_EDIT, emptyList()) { SourceTarget.Repo }
}

/** The three screens of one kind of [SourceTarget], read from the routes' [targetArgs]. */
private fun NavGraphBuilder.sourceScreens(
    nav: NavHostController,
    sourceRoute: String,
    fileRoute: String,
    editRoute: String,
    targetArgs: List<NamedNavArgument>,
    target: (Bundle?) -> SourceTarget,
) {
    val onAddToken = { nav.navigate(Routes.settingsPage(SettingsPage.GIT_HOSTS)) }
    composable(
        sourceRoute,
        arguments = targetArgs + navArgument("dir") { type = NavType.StringType; defaultValue = "" },
    ) { entry ->
        val t = target(entry.arguments)
        SourceBrowserScreen(
            target = t,
            startDir = entry.arguments?.getString("dir")?.takeIf { it.isNotEmpty() },
            onBack = { nav.popBackStack() },
            onFile = { path -> nav.navigate(sourceFileRoute(t, path)) },
            onAddToken = onAddToken,
            onHit = { path, line -> nav.navigate(sourceFileRoute(t, path, line)) },
        )
    }
    composable(
        fileRoute,
        arguments = targetArgs + navArgument("path") { type = NavType.StringType; defaultValue = "" } +
            navArgument("line") { type = NavType.IntType; defaultValue = 0 },
    ) { entry ->
        val t = target(entry.arguments)
        val path = entry.arguments?.getString("path").orEmpty()
        SourceFileScreen(
            target = t,
            path = path,
            line = entry.arguments?.getInt("line")?.takeIf { it > 0 },
            onBack = { nav.popBackStack() },
            onAddToken = onAddToken,
            onEdit = { nav.navigate(sourceEditRoute(t, path)) },
            onQuickEdit = { edit -> nav.navigate(sourceEditRoute(t, path, encodeQuickEdit(edit))) },
        )
    }
    composable(
        editRoute,
        arguments = targetArgs + navArgument("path") { type = NavType.StringType; defaultValue = "" } +
            navArgument("quick") { type = NavType.StringType; defaultValue = "" },
    ) { entry ->
        SourceEditScreen(
            target = target(entry.arguments),
            path = entry.arguments?.getString("path").orEmpty(),
            onBack = { nav.popBackStack() },
            onAddToken = onAddToken,
            quick = decodeQuickEdit(entry.arguments?.getString("quick")),
        )
    }
}
