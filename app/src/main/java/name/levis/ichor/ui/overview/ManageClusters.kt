package name.levis.ichor.ui.overview

import name.levis.ichor.selectCluster
import name.levis.ichor.renameCluster
import name.levis.ichor.setClusterTags
import name.levis.ichor.setVpnOnly
import name.levis.ichor.setWatched
import name.levis.ichor.setAuditEvents
import name.levis.ichor.setKubeServer
import name.levis.ichor.setKubeAccess
import name.levis.ichor.removeCluster
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
import name.levis.ichor.ui.gitops.ClusterRepoSheet
import name.levis.ichor.ui.importconfig.ServiceAccountHelpDialog
import name.levis.ichor.ui.kubeauth.KubeAccessDialog
import name.levis.ichor.ui.kubeauth.SignInAccountDialog
import name.levis.ichor.ui.kubeauth.SignInSheet
import name.levis.ichor.ui.userMessage

/**
 * The cluster sheet as both homes (Talos overview, Kubernetes home) open it: pick, rename,
 * color, VPN only, background watch, sign-in or Kubernetes access, activity, cluster repository, remove, add. [onEndpoints] edits a Talos
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
    onActivity: ((ContextSummary) -> Unit)? = null,
    /** Settings → Git hosts, from the repository sheet of a cluster whose host has no token. */
    onGitHosts: (() -> Unit)? = null,
    /** "All clusters": the fleet view, from the sheet's first row. */
    onFleet: (() -> Unit)? = null,
) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val vpnOnly by app.vpnOnly.fingerprints.collectAsStateWithLifecycle()
    val tags by app.clusterTags.tags.collectAsStateWithLifecycle()
    val unwatched by app.unwatchedClusters.fingerprints.collectAsStateWithLifecycle()
    val auditEvents by app.auditEventClusters.fingerprints.collectAsStateWithLifecycle()
    val kubeServers by app.kubeServers.servers.collectAsStateWithLifecycle()
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var account by remember { mutableStateOf<ContextSummary?>(null) }
    var repository by remember { mutableStateOf<ContextSummary?>(null) }
    var signingIn by remember { mutableStateOf<String?>(null) }
    // Removals still writing the stored config: their rows show a spinner until they are gone.
    var removingNames by remember { mutableStateOf(emptySet<String>()) }
    // Which kubeconfig clusters wait for a sign-in: read from what is stored, no network.
    val signInNeeded by produceState(emptySet<String>(), config.kubeYaml, config.talosYaml, invalidations) {
        value = config.summary.contexts.filter { it.signIn.isNotEmpty() }
            .filter { runCatching { app.kubeAuthRepository.info(it.name)?.signedIn == false }.getOrDefault(false) }
            .map { it.name }
            .toSet()
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
        tags = tags,
        onTags = app::setClusterTags,
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
        onFleet = onFleet?.let { open ->
            {
                onClose()
                open()
            }
        },
        onEndpoints = onEndpoints?.let { edit ->
            { cluster ->
                onClose()
                edit(cluster)
            }
        },
        signInNeeded = signInNeeded,
        onAccount = { account = it },
        onActivity = onActivity?.let { open ->
            { cluster ->
                onClose()
                open(cluster)
            }
        },
        removingNames = removingNames,
        unwatched = unwatched,
        onWatch = app::setWatched,
        auditEvents = auditEvents,
        onAuditEvents = app::setAuditEvents,
        onRepository = { repository = it },
        kubeServers = kubeServers,
        // An open Kubernetes screen reloads its lists through the new address.
        onKubeServer = { cluster, server -> app.setKubeServer(cluster.fingerprint, server) },
    )

    repository?.let { cluster ->
        ClusterRepoSheet(
            app = app,
            fingerprint = cluster.fingerprint,
            label = labels.of(cluster),
            onDismiss = { repository = null },
            onGitHosts = onGitHosts?.let { open ->
                {
                    repository = null
                    onClose()
                    open()
                }
            },
        )
    }

    account?.let { cluster ->
        if (cluster.isKube && cluster.signIn.isEmpty()) {
            // Added with a token: how to make a least-privilege ServiceAccount one.
            ServiceAccountHelpDialog(onDismiss = { account = null })
        } else if (cluster.isKube || cluster.omni) {
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
