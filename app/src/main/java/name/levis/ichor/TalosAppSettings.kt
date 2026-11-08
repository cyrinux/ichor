package name.levis.ichor

import name.levis.ichorgo.Ichorgo
import name.levis.ichor.util.StartupTrace
import name.levis.ichor.util.ExitRecords
import name.levis.ichor.data.BackupManager
import name.levis.ichor.data.Diagnostics
import name.levis.ichor.model.BackupLockMode
import name.levis.ichor.monitor.canPostNotifications
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.lifecycleScope
import name.levis.ichor.data.PrivacyMask
import name.levis.ichor.widget.refreshFleetGlance
import name.levis.ichor.widget.updateAllWidgets
import name.levis.ichor.monitor.clusterFingerprints
import name.levis.ichor.monitor.cancelPostedAlerts
import name.levis.ichor.monitor.syncMonitoring
import name.levis.ichor.ui.operations.OperationKind
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.EndpointMatch
import name.levis.ichor.model.auditEventClustersArgument
import name.levis.ichor.model.normalizeTags
import name.levis.ichor.model.signsOutOnRemoval
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch

// The settings TalosApp's screens change, and what follows each change: extension functions
// so TalosApp.kt stays the composition root.

/** The backup manager, over every store a backup holds. */
internal fun TalosApp.newBackupManager() = BackupManager(
    configRepository, uiPreferences, clusterColors, clusterNames, vpnOnly, kubeServers, kubeAccess, kubeAuthStore, wakeOnLan, monitorStore,
    pinStore, gitHostStore, aiPreferences, clusterRepos,
    lockMode = { BackupLockMode.of(appLock.enabled.value, appLock.securityKeys.value != null) },
    setPrivacyMask = this::setPrivacyMask,
    notificationsAllowed = { canPostNotifications(this) },
    onRestored = {
        applyAuditEventClusters()
        talosRepository.invalidate()
        forgetShownCluster()
    },
    proposals = proposals,
    plugins = pluginRepository,
    auditEvents = auditEventClusters,
    onCallSet = { onCallStore.read(it).isSet },
    tags = clusterTags,
)

/** Shows another cluster (a context of the stored config); the widget and the alerts follow it. */
fun TalosApp.selectCluster(name: String) {
    if (name == configRepository.config.value?.activeContext) return
    configRepository.selectContext(name)
    forgetShownCluster()
}

/** Gives the cluster [fingerprint] the name [name] (blank: its context name again); the widget follows. */
fun TalosApp.renameCluster(fingerprint: String, name: String) {
    clusterNames.set(fingerprint, name)
    ProcessLifecycleOwner.get().lifecycleScope.launch {
        refreshFleetGlance()
        updateAllWidgets(this@renameCluster)
        syncWear()
    }
}

/** Gives the cluster of [context] (all its contexts, so a fleet row finds them whichever was checked) the tags typed in [input]. */
fun TalosApp.setClusterTags(context: ContextSummary, input: String) {
    val summary = configRepository.config.value?.summary ?: return
    val tags = normalizeTags(input)
    clusterFingerprints(summary, context).forEach { clusterTags.set(it, tags) }
}

/**
 * Sets whether the cluster [fingerprint] is reached over a VPN only. When it is the one
 * on screen it reloads: with the VPN off, the screens say to connect it instead.
 */
fun TalosApp.setVpnOnly(fingerprint: String, vpnOnly: Boolean) {
    this.vpnOnly.set(fingerprint, vpnOnly)
    if (fingerprint == configRepository.config.value?.activeSummary?.fingerprint) talosRepository.invalidate()
}

/**
 * Sets whether the background monitor checks the cluster of [context] (all its contexts):
 * a cluster left out drops its snapshot and alerts at the next check.
 */
fun TalosApp.setWatched(context: ContextSummary, watched: Boolean) {
    val summary = configRepository.config.value?.summary ?: return
    clusterFingerprints(summary, context).forEach { unwatchedClusters.set(it, !watched) }
    launchSync(runNow = true)
}

/**
 * Sets the Kubernetes API address of the cluster [fingerprint] ([server] checked by
 * Ichorgo.normalizeKubeServer, blank for the kubeconfig's); its cached results go.
 */
fun TalosApp.setKubeServer(fingerprint: String, server: String) {
    kubeServers.set(fingerprint, server)
    if (fingerprint == configRepository.config.value?.activeSummary?.fingerprint) talosRepository.invalidate()
}

/**
 * Sets the Kubernetes access of the Talos cluster [fingerprint]: the kubeconfig cluster
 * [kubeFingerprint], or null for the Talos admin kubeconfig. Its Kubernetes screens reload.
 */
fun TalosApp.setKubeAccess(fingerprint: String, kubeFingerprint: String?) {
    kubeAccess.set(fingerprint, kubeFingerprint)
    configRepository.relink()
    if (fingerprint == configRepository.config.value?.activeSummary?.fingerprint) talosRepository.invalidate()
}

/**
 * Brings the app back over the browser a sign-in finished in. The task comes to the front
 * as it was (no new screen); Android may refuse it while the app is in the background.
 */
fun TalosApp.bringToFront() {
    val intent = android.content.Intent(this, MainActivity::class.java).addFlags(
        android.content.Intent.FLAG_ACTIVITY_NEW_TASK or
            android.content.Intent.FLAG_ACTIVITY_REORDER_TO_FRONT or
            android.content.Intent.FLAG_ACTIVITY_SINGLE_TOP,
    )
    runCatching { startActivity(intent) }
}

/** A kubeconfig cluster was signed in or out: what was loaded without (or with) it goes. */
fun TalosApp.signInChanged() {
    talosRepository.invalidate()
    launchSync(runNow = true)
}

/** Adds discovered [nodes] to the cluster [name]'s talosconfig context; screens and monitoring follow. */
suspend fun TalosApp.addClusterNodes(name: String, nodes: List<String>) {
    configRepository.addNodes(name, nodes)
    talosRepository.invalidate()
    launchSync(runNow = true)
}

/**
 * Takes [nodes] out of the cluster [name]'s talosconfig context; screens and monitoring follow.
 * Node discovery does not offer them again until the app restarts: a member lingers for a while.
 */
suspend fun TalosApp.removeClusterNodes(name: String, nodes: List<String>, hostnames: List<String> = emptyList()) {
    configRepository.removeNodes(name, nodes)
    nodeLifecycleRepository.noteForgotten(name, nodes + hostnames)
    talosRepository.invalidate()
    launchSync(runNow = true)
}

/** Replaces the endpoints of the cluster [name]'s talosconfig context; screens and monitoring follow. */
suspend fun TalosApp.setClusterEndpoints(name: String, endpoints: List<String>) {
    configRepository.setEndpoints(name, endpoints)
    talosRepository.invalidate()
    launchSync(runNow = true)
}

/**
 * Adds the endpoints a network search found to the contexts each one answered for. Once a
 * cluster answers again, node discovery offers the members it still misses.
 */
suspend fun TalosApp.addFoundEndpoints(matches: List<EndpointMatch>) {
    configRepository.addEndpoints(matches)
    talosRepository.invalidate()
    launchSync(runNow = true)
}

/** Removes a cluster from the stored config; false when it was the last one (nothing is stored anymore). */
suspend fun TalosApp.removeCluster(name: String): Boolean {
    val stored = configRepository.config.value
    // Its sign-in goes with it, the token the core holds in memory too (unless another
    // Omni cluster of the same identity still uses it).
    if (stored != null && signsOutOnRemoval(stored.summary.contexts, name)) {
        runCatching { kubeAuthRepository.signOut(name) }
    }
    val wasShown = name == stored?.activeContext
    val remains = configRepository.removeContext(name)
    if (wasShown) forgetShownCluster() else launchSync(runNow = true)
    return remains
}

/**
 * Turns screenshot mode on or off. Everything fetched under the previous setting is
 * dropped (in-memory results, the monitor snapshot behind the widget) and refetched,
 * so no real name or address lingers on screen or on the home screen.
 */
fun TalosApp.setPrivacyMask(mask: PrivacyMask) {
    if (mask == uiPreferences.privacyMask.value) return
    ProcessLifecycleOwner.get().lifecycleScope.launch {
        if (!maskSwitcher.switch(mask)) return@launch
        // What was posted with real names goes; a running operation is shown again masked.
        if (mask.enabled) cancelNamedNotifications()
        updateAllWidgets(this@setPrivacyMask)
        syncWear()
        syncMonitoring(this@setPrivacyMask, runNow = true)
    }
}

private fun TalosApp.cancelNamedNotifications() {
    cancelPostedAlerts(this)
    val manager = androidx.core.app.NotificationManagerCompat.from(this)
    name.levis.ichor.ui.operations.OperationKind.entries.forEach { manager.cancel(it.resultId) }
}

/**
 * Sets whether the cluster of [context] (all its contexts) records actions: a Kubernetes
 * Event on each object the app changes there. Saved, then handed to Go.
 */
fun TalosApp.setAuditEvents(context: ContextSummary, on: Boolean) {
    val summary = configRepository.config.value?.summary ?: return
    clusterFingerprints(summary, context).forEach { auditEventClusters.set(it, on) }
    applyAuditEventClusters()
}

/** Hands Go the clusters that record actions (it keeps none of its own across launches). */
fun TalosApp.applyAuditEventClusters() {
    Ichorgo.setAuditEventClusters(auditEventClustersArgument(auditEventClusters.fingerprints.value))
}

/** Turns the startup timing in logcat on or off (always on in a debug build). */
fun TalosApp.setStartupTrace(enabled: Boolean) {
    uiPreferences.setStartupTrace(enabled)
    StartupTrace.setEnabled(BuildConfig.DEBUG || enabled)
}

/** Keeps the diagnostics log on this phone across launches, or in memory only (its files deleted). */
fun TalosApp.setDiagPersist(enabled: Boolean) {
    uiPreferences.setDiagPersist(enabled)
    ProcessLifecycleOwner.get().lifecycleScope.launch { Diagnostics.setPersist(enabled) }
}

/**
 * At launch, after Ichorgo.setDataDir: the diagnostics log is kept or not as set, then the
 * OS's exit records since the last launch are copied to it. On IO, never blocking the start.
 */
fun TalosApp.applyDiagnostics() {
    ProcessLifecycleOwner.get().lifecycleScope.launch(Dispatchers.IO) {
        Diagnostics.setPersist(uiPreferences.diagPersist.value)
        runCatching { ExitRecords.logNew(this@applyDiagnostics, uiPreferences) }
    }
}

/**
 * Turns "Keep last known state" on or off. Off deletes everything kept and its key; on
 * starts with what the screens fetch next.
 */
fun TalosApp.setOfflineCache(enabled: Boolean) {
    if (enabled == uiPreferences.offlineCache.value) return
    uiPreferences.setOfflineCache(enabled)
    ProcessLifecycleOwner.get().lifecycleScope.launch(Dispatchers.IO) {
        if (enabled) talosRepository.restoreOffline() else offlineCache.clear()
    }
}
