package name.levis.ichor.ui.nav

import androidx.compose.runtime.compositionLocalOf
import androidx.navigation.NavHostController
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.kubeFocus

/**
 * Opens pod [name] of [namespace] where its logs and actions are: the Kubernetes screen's Pods
 * tab, scoped to its namespace, searched for its name, its log sheet open (as a pod link does).
 */
fun NavHostController.openPod(namespace: String, name: String) {
    ShareTarget.pod(namespace, name).kubeFocus?.let { navigate(Routes.workloads(it)) }
}

/**
 * Opens a pod (namespace, name) on the Kubernetes screen, provided by the graphs whose screens
 * name pods; null where no screen can open it (a pod name then stays plain text).
 */
val LocalOpenPod = compositionLocalOf<((namespace: String, name: String) -> Unit)?> { null }
