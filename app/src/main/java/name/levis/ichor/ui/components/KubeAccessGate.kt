package name.levis.ichor.ui.components

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.CancellationException
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubeActionAccess
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.KubeWhoAmI
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.theme.LocalStatusColors

/** Why [this] refusal blocks an action: "Your account cannot patch deployments in shop". */
fun KubePermission.denialText(): UiText =
    if (namespace.isEmpty()) UiText.Res(R.string.kube_access_denied_cluster, verb, resource)
    else UiText.Res(R.string.kube_access_denied, verb, resource, namespace)

/**
 * The access of the active cluster's credentials to the app's Kubernetes actions in
 * [namespace] ("" for cluster-wide ones, e.g. a node's), read when shown and again for another
 * cluster. Null while it loads, and when it cannot be read: actions then stay offered, the
 * API server still decides.
 */
@Composable
fun rememberKubeActionAccess(namespace: String): KubeActionAccess? {
    if (LocalInspectionMode.current) return null
    val app = LocalContext.current.applicationContext as? TalosApp ?: return null
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val cluster = config?.activeContext
    val access by produceState<KubeActionAccess?>(null, namespace, cluster) {
        value = null
        if (cluster == null) return@produceState
        value = try {
            app.kubePermissions.actionAccess(namespace)
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            null
        }
    }
    return access
}

/** What refuses [action] in [namespace] for sure, or null (allowed, unknown, loading or unreadable). */
@Composable
fun rememberKubeDenial(action: KubeAction, namespace: String): KubePermission? = rememberKubeActionAccess(namespace)?.denial(action)

/** The one-line reason a refused action is disabled; nothing when [denial] is null. */
@Composable
fun KubeDenialNote(denial: KubePermission?, modifier: Modifier = Modifier) {
    if (denial == null) return
    Text(
        denial.denialText().asString(),
        style = MaterialTheme.typography.labelSmall,
        color = LocalStatusColors.current.warn,
        maxLines = 2,
        overflow = TextOverflow.Ellipsis,
        modifier = modifier,
    )
}

/**
 * Who the API server takes the credentials of the active cluster ([cluster], to read again
 * for another) for; null while it loads, when it cannot say or cannot be asked.
 */
@Composable
fun rememberKubeWhoAmI(cluster: String): KubeWhoAmI? {
    if (LocalInspectionMode.current) return null
    val app = LocalContext.current.applicationContext as? TalosApp ?: return null
    val who by produceState<KubeWhoAmI?>(null, cluster) {
        value = try {
            app.kubePermissions.whoAmI().takeIf { it.known }
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            null
        }
    }
    return who
}
