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
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubeActionAccess
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.KubeWhoAmI
import name.levis.ichor.model.firstDenial
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

/** What refuses [action] in [namespace] for sure, or null (allowed, unknown, loading, unreadable, or no action). */
@Composable
fun rememberKubeDenial(action: KubeAction?, namespace: String): KubePermission? = rememberKubeActionAccess(namespace)?.denial(action)

/**
 * What refuses an action over several objects ([checks]: each one's action and namespace), each
 * distinct namespace asked: the first refusal, or null when none is sure yet (loading,
 * unreadable) or at all. For a bulk action, allowed only where each of its objects is.
 */
@Composable
fun rememberFirstKubeDenial(checks: List<Pair<KubeAction?, String>>): KubePermission? {
    if (LocalInspectionMode.current) return null
    val app = LocalContext.current.applicationContext as? TalosApp ?: return null
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val cluster = config?.activeContext
    val asked = checks.filter { it.first != null }.distinct()
    val denial by produceState<KubePermission?>(null, asked, cluster) {
        value = null
        if (cluster == null || asked.isEmpty()) return@produceState
        val accesses = coroutineScope {
            asked.map { it.second }.distinct().map { namespace ->
                async {
                    namespace to try {
                        app.kubePermissions.actionAccess(namespace)
                    } catch (e: CancellationException) {
                        throw e
                    } catch (_: Exception) {
                        null
                    }
                }
            }.awaitAll().toMap()
        }
        value = firstDenial(asked) { accesses[it] }
    }
    return denial
}

/** What refuses [action] on objects of [namespaces] (a bulk action): the first namespace's refusal, null when each allows it or is unknown. */
@Composable
fun rememberKubeDenialAcross(action: KubeAction?, namespaces: List<String>): KubePermission? =
    rememberFirstKubeDenial(namespaces.distinct().map { action to it })

/**
 * What refuses [verb] on [resource] ("resource/subresource") of [group] in [namespace] ("" for a
 * cluster-scoped one) on the object [name], for an action [KubeAction] does not cover (saving an
 * edited object): null while it loads, when allowed, unknown or unreadable.
 */
@Composable
fun rememberKubeCanDenial(verb: String, group: String, resource: String, namespace: String, name: String): KubePermission? {
    if (LocalInspectionMode.current) return null
    val app = LocalContext.current.applicationContext as? TalosApp ?: return null
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val cluster = config?.activeContext
    val denial by produceState<KubePermission?>(null, verb, group, resource, namespace, name, cluster) {
        value = null
        if (cluster == null) return@produceState
        value = try {
            app.kubePermissions.can(verb, group, resource, namespace, name).takeIf { it.denied }
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            null
        }
    }
    return denial
}

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
