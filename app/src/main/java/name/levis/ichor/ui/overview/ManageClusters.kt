package name.levis.ichor.ui.overview

import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import name.levis.ichor.TalosApp
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.isKube
import name.levis.ichor.model.isOmni
import name.levis.ichor.ui.kubeauth.KubeAccessDialog
import name.levis.ichor.ui.kubeauth.OmniSignInSheet
import name.levis.ichor.ui.kubeauth.SignInAccountDialog
import name.levis.ichor.ui.kubeauth.SignInSheet
import name.levis.ichor.ui.userMessage

/**
 * The cluster sheet as both homes (Talos overview, Kubernetes home) open it: pick, rename,
 * color, VPN only, sign-in or Kubernetes access, remove, add. [onEndpoints] edits a Talos
 * cluster's endpoints, when offered.
 */
@Composable
fun ManageClustersSheet(
    config: StoredConfig,
    colors: Map<String, Int>,
    labels: ClusterLabels,
    onClose: () -> Unit,
    onAddCluster: () -> Unit,
    onClustersCleared: () -> Unit,
    onEndpoints: ((ContextSummary) -> Unit)? = null,
) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val vpnOnly by app.vpnOnly.fingerprints.collectAsStateWithLifecycle()
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var account by remember { mutableStateOf<ContextSummary?>(null) }
    var signingIn by remember { mutableStateOf<String?>(null) }
    // Removals still writing the stored config: their rows show a spinner until they are gone.
    var removingNames by remember { mutableStateOf(emptySet<String>()) }
    // Which kubeconfig and Omni clusters wait for a sign-in: read from what is stored, no network.
    val signInNeeded by produceState(emptySet<String>(), config.kubeYaml, config.talosYaml, invalidations) {
        val kube = config.summary.contexts.filter { it.isKube && it.signIn.isNotEmpty() }
            .filter { runCatching { app.kubeAuthRepository.info(it.name)?.signedIn == false }.getOrDefault(false) }
        val omni = config.summary.contexts.filter { it.isOmni }
            .filter { runCatching { !app.omniAuthRepository.info(it.name).signedIn }.getOrDefault(false) }
        value = (kube + omni).map { it.name }.toSet()
    }
    ClusterSheet(
        config = config,
        colors = colors,
        labels = labels,
        onSelect = {
            onClose()
            app.selectCluster(it)
        },
        onRename = { cluster, name -> app.renameCluster(cluster.fingerprint, name) },
        onColor = { cluster, color -> app.clusterColors.set(cluster.fingerprint, color) },
        vpnOnly = vpnOnly,
        onVpnOnly = { cluster, on -> app.setVpnOnly(cluster.fingerprint, on) },
        onAdd = {
            onClose()
            onAddCluster()
        },
        onRemove = { name ->
            removingNames = removingNames + name
            scope.launch {
                runCatching { app.removeCluster(name) }.fold(
                    onSuccess = { remains -> if (!remains) onClustersCleared() },
                    onFailure = { Toast.makeText(context, it.userMessage(), Toast.LENGTH_LONG).show() },
                )
                removingNames = removingNames - name
            }
        },
        onDismiss = onClose,
        onEndpoints = onEndpoints?.let { edit ->
            { cluster ->
                onClose()
                edit(cluster)
            }
        },
        signInNeeded = signInNeeded,
        onAccount = { account = it },
        removingNames = removingNames,
    )

    account?.let { cluster ->
        if (cluster.isOmni) {
            OmniSignInSheet(context = cluster.name, onDismiss = { account = null }, onSignedIn = { account = null })
        } else if (cluster.isKube) {
            SignInAccountDialog(
                cluster = cluster,
                label = labels.of(cluster),
                onSignIn = {
                    account = null
                    signingIn = cluster.name
                },
                onDismiss = { account = null },
            )
        } else {
            val links by app.kubeAccess.links.collectAsStateWithLifecycle()
            KubeAccessDialog(
                label = labels.of(cluster),
                current = links[cluster.fingerprint].orEmpty(),
                kubeClusters = config.summary.contexts.filter { it.isKube }.map { it to labels.of(it) },
                onPick = { app.setKubeAccess(cluster.fingerprint, it) },
                onDismiss = { account = null },
            )
        }
    }
    signingIn?.let { name ->
        SignInSheet(context = name, onDismiss = { signingIn = null }, onSignedIn = { signingIn = null })
    }
}
