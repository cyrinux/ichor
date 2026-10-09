package name.levis.ichor.ui.kubebrowser

import name.levis.ichor.ui.nav.Routes
import android.net.Uri
import android.os.Bundle
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NamedNavArgument
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import name.levis.ichor.model.ApiResource
import name.levis.ichor.model.KubeObjectRef

/** Routes of the resource browser, Helm and port-forward screens (plans/roadmap/kubeconfig-only.md §7). */
object KubeBrowserRoutes {
    const val KINDS = "kube-browser"
    private const val LIST = "kube-browser-list?g={g}&v={v}&r={r}&k={k}&namespaced={namespaced}&edit={edit}"
    private const val OBJECT = "kube-object?g={g}&v={v}&r={r}&k={k}&ns={ns}&name={name}&edit={edit}"
    const val HELM = "kube-helm"
    const val STORAGE = "kube-storage"
    const val SERVICES = "kube-services"
    private const val HELM_RELEASE = "kube-helm-release?ns={ns}&name={name}"
    private const val FORWARD = "kube-forward?ns={ns}&pod={pod}"

    /** Set on the screen an object was opened from once it is deleted: a list reads itself again. */
    private const val DELETED = "kube-object-deleted"

    fun list(r: ApiResource) =
        "kube-browser-list?g=${e(r.group)}&v=${e(r.version)}&r=${e(r.resource)}&k=${e(r.kind)}&namespaced=${r.namespaced}&edit=${r.editable}&scale=${r.scalable}"

    fun obj(o: KubeObjectRef) =
        "kube-object?g=${e(o.group)}&v=${e(o.version)}&r=${e(o.resource)}&k=${e(o.kind)}&ns=${e(o.namespace)}&name=${e(o.name)}&edit=${o.editable}&scale=${o.scalable}"

    fun helmRelease(namespace: String, name: String) = "kube-helm-release?ns=${e(namespace)}&name=${e(name)}"

    fun forward(namespace: String, pod: String) = "kube-forward?ns=${e(namespace)}&pod=${e(pod)}"

    private fun e(s: String): String = Uri.encode(s)

    private fun strings(vararg names: String): List<NamedNavArgument> = names.map { navArgument(it) { type = NavType.StringType; defaultValue = "" } }

    private fun flags(vararg names: String): List<NamedNavArgument> = names.map { navArgument(it) { type = NavType.BoolType; defaultValue = false } }

    private fun Bundle?.str(name: String): String = this?.getString(name).orEmpty()

    /** The browser's screens, opened from the Kubernetes screen, the Kubernetes home and the pod log sheet ([links]). */
    fun NavGraphBuilder.kubeBrowserScreens(nav: NavHostController, links: KubeLinks) {
        composable(KINDS) {
            ResourceKindsScreen(onBack = { nav.popBackStack() }, onKind = { nav.navigate(list(it)) })
        }
        composable(LIST, arguments = strings("g", "v", "r", "k") + flags("namespaced", "edit", "scale")) { entry ->
            val a = entry.arguments
            val deleted by entry.savedStateHandle.getStateFlow(DELETED, 0L).collectAsStateWithLifecycle()
            val type = ApiResource(
                group = a.str("g"),
                version = a.str("v"),
                resource = a.str("r"),
                kind = a.str("k"),
                namespaced = a?.getBoolean("namespaced") == true,
                verbs = if (a?.getBoolean("edit") == true) listOf("update") else emptyList(),
                scalable = a?.getBoolean("scale") == true,
            )
            ResourceListScreen(type, deleted, onBack = { nav.popBackStack() }, onObject = { row ->
                links.onObject(KubeObjectRef(type.group, type.version, type.resource, type.kind, row.namespace, row.name, type.editable, type.scalable))
            })
        }
        composable(OBJECT, arguments = strings("g", "v", "r", "k", "ns", "name") + flags("edit", "scale")) { entry ->
            val a = entry.arguments
            val ref = KubeObjectRef(a.str("g"), a.str("v"), a.str("r"), a.str("k"), a.str("ns"), a.str("name"), a?.getBoolean("edit") == true, a?.getBoolean("scale") == true)
            KubeObjectScreen(
                ref,
                onBack = { nav.popBackStack() },
                onPortForward = { links.onPortForward(ref.namespace, ref.name) },
                onOwner = links.onObject,
                onHelmRelease = { ns, name -> nav.navigate(helmRelease(ns, name)) },
                onDeleted = {
                    nav.previousBackStackEntry?.savedStateHandle?.set(DELETED, System.currentTimeMillis())
                    nav.popBackStack()
                },
            )
        }
        composable(STORAGE) {
            StorageScreen(
                onBack = { nav.popBackStack() },
                onClaim = links.onObject,
                onDataService = { nav.navigate(Routes.dataServices(it)) },
            )
        }
        composable(SERVICES) {
            ServicesScreen(onBack = { nav.popBackStack() }, onService = links.onObject)
        }
        composable(HELM) {
            HelmReleasesScreen(onBack = { nav.popBackStack() }, onRelease = { nav.navigate(helmRelease(it.namespace, it.name)) })
        }
        composable(HELM_RELEASE, arguments = strings("ns", "name")) { entry ->
            HelmReleaseScreen(entry.arguments.str("ns"), entry.arguments.str("name"), onBack = { nav.popBackStack() })
        }
        composable(FORWARD, arguments = strings("ns", "pod")) { entry ->
            PortForwardScreen(entry.arguments.str("ns"), entry.arguments.str("pod"), onBack = { nav.popBackStack() })
        }
    }
}
