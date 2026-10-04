package name.levis.ichor

import name.levis.ichorgo.Ichorgo
import android.app.Application
import android.content.Context
import android.os.SystemClock
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.lifecycleScope
import name.levis.ichor.data.BackupManager
import name.levis.ichor.data.AiPreferences
import name.levis.ichor.data.CaptureRepository
import name.levis.ichor.data.CiliumRepository
import name.levis.ichor.data.NetPerfHistory
import name.levis.ichor.data.NetPerfRepository
import name.levis.ichor.data.PublicIpRepository
import name.levis.ichor.data.ChangelogRepository
import name.levis.ichor.data.ClusterColors
import name.levis.ichor.data.DiagnosisRepository
import name.levis.ichor.data.SecureStore
import name.levis.ichor.data.KeystoreSealer
import name.levis.ichor.data.KeystoreValue
import name.levis.ichor.data.OfflineCache
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.TalosUpdateChecker
import name.levis.ichor.data.UpgradeManager
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.SupportBundleRepository
import name.levis.ichor.data.SupportPrompt
import name.levis.ichor.data.FundingHistory
import name.levis.ichor.data.RoadmapRepository
import name.levis.ichor.data.createFeatureStore
import name.levis.ichor.data.PrivacyMask
import name.levis.ichor.data.UiPreferences
import androidx.glance.appwidget.updateAll
import name.levis.ichor.widget.ClusterWidget
import name.levis.ichor.ui.apps.AppIconLoader
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.monitor.MonitorStore
import name.levis.ichor.monitor.canPostNotifications
import name.levis.ichor.monitor.syncMonitoring
import name.levis.ichor.security.AppLock
import name.levis.ichor.security.PrefsLockSettings
import name.levis.ichor.update.UpdateManager
import name.levis.ichor.ui.debug.DebugShells
import name.levis.ichor.data.ClusterNames
import name.levis.ichor.data.WakeOnLanStore
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.data.VpnMonitor
import name.levis.ichor.data.VpnOnlyClusters
import name.levis.ichor.data.KubeServers
import name.levis.ichor.data.VpnRequiredException
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.EndpointMatch
import name.levis.ichor.model.heldBackForVpn
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.shortcuts.ClusterShortcuts
import name.levis.ichor.shortcuts.ShortcutSpec
import name.levis.ichor.shortcuts.clusterShortcutIds
import name.levis.ichor.shortcuts.clusterShortcuts
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.launch

/** Holds app-wide singletons (manual DI; the app is small). */
class TalosApp : Application() {
    val configRepository by lazy { ConfigRepository(this, guard = ::holdBackOffVpn) }
    val talosRepository by lazy { TalosRepository(configRepository, kubeServers, offlineCache) }

    /** Last known cluster data on disk, only while "Keep last known state" is on (Settings → Privacy). */
    private val offlineCache by lazy {
        OfflineCache(
            java.io.File(noBackupFilesDir, "offline"),
            KeystoreSealer("ichor-offline"),
            enabled = { uiPreferences.offlineCache.value },
        )
    }
    /** Debug shells, kept open across screens until exited (see DebugShellService). */
    val debugShells by lazy { DebugShells(this, configRepository) }
    val captureRepository by lazy { CaptureRepository(configRepository, filesDir) }
    val netPerfRepository by lazy { NetPerfRepository(configRepository, kubeServers) }
    val ciliumRepository by lazy { CiliumRepository(configRepository, kubeServers) }
    val netPerfHistory by lazy { NetPerfHistory(java.io.File(noBackupFilesDir, "netperf")) }
    val publicIps by lazy {
        PublicIpRepository(
            configRepository,
            kubeServers,
            KeystoreValue(java.io.File(noBackupFilesDir, "public-ips.enc"), "ichor-public-ips"),
            masked = { uiPreferences.privacyMask.value.enabled },
        )
    }
    val supportBundleRepository by lazy { SupportBundleRepository(configRepository, filesDir) }
    val upgradeManager by lazy { UpgradeManager(configRepository, onFinished = talosRepository::forgetFeatures) }
    val talosUpdateChecker by lazy { TalosUpdateChecker() }
    val uiPreferences by lazy { UiPreferences(getSharedPreferences(UiPreferences.FILE, Context.MODE_PRIVATE)) }
    val clusterColors by lazy { ClusterColors(getSharedPreferences(ClusterColors.FILE, Context.MODE_PRIVATE)) }
    val clusterNames by lazy { ClusterNames(getSharedPreferences(ClusterNames.FILE, Context.MODE_PRIVATE)) }
    val wakeOnLan by lazy {
        val sealed = KeystoreValue(java.io.File(noBackupFilesDir, "wake-on-lan.enc"), "ichor-wake-on-lan")
        WakeOnLanStore.migrate(
            sealed,
            getSharedPreferences(WakeOnLanStore.FILE, Context.MODE_PRIVATE),
            getSharedPreferences(WakeOnLanStore.SEEN_FILE, Context.MODE_PRIVATE),
        )
        WakeOnLanStore(sealed)
    }
    val vpnOnly by lazy { VpnOnlyClusters(getSharedPreferences(VpnOnlyClusters.FILE, Context.MODE_PRIVATE)) }
    val kubeServers by lazy { KubeServers(getSharedPreferences(KubeServers.FILE, Context.MODE_PRIVATE)) }
    val vpn by lazy { VpnMonitor(this) }
    val appLock by lazy {
        AppLock(
            PrefsLockSettings(getSharedPreferences("ichor-security", Context.MODE_PRIVATE)),
            clock = SystemClock::elapsedRealtime,
        )
    }

    val supportPrompt by lazy { SupportPrompt(getSharedPreferences("ichor-support", Context.MODE_PRIVATE)) }
    val fundingHistory by lazy { FundingHistory(getSharedPreferences(FundingHistory.FILE, Context.MODE_PRIVATE)) }
    val featureStore by lazy { createFeatureStore(this, fundingHistory) }
    val roadmapRepository by lazy { RoadmapRepository(getSharedPreferences(RoadmapRepository.FILE, Context.MODE_PRIVATE)) }
    val changelogRepository by lazy { ChangelogRepository(this, getSharedPreferences(ChangelogRepository.PREFS, Context.MODE_PRIVATE)) }
    val updateManager by lazy {
        UpdateManager(this, getSharedPreferences("ichor-update", Context.MODE_PRIVATE), changelogRepository)
    }
    /** Bundled app icons, and downloaded ones when the user allowed it (Settings → Privacy). */
    val appIcons by lazy { AppIconLoader(this) }
    val monitorStore by lazy {
        MonitorStore(
            getSharedPreferences("ichor-monitor", Context.MODE_PRIVATE),
            KeystoreValue(java.io.File(noBackupFilesDir, "monitor-snapshot.enc"), "ichor-monitor-snapshot"),
        )
    }

    /** The optional AI diagnosis: off until enabled in Settings. API keys get their own Keystore keys. */
    val diagnosisRepository by lazy { DiagnosisRepository(configRepository) }
    val aiPreferences by lazy {
        AiPreferences(
            getSharedPreferences(AiPreferences.FILE, Context.MODE_PRIVATE),
            diagnosisRepository.providers.map { it.id },
        ) { provider -> SecureStore(java.io.File(filesDir, "ai-key-$provider.enc"), keyAlias = "ai-key-$provider") }
    }

    /** Passphrase-sealed backups of the config and settings, restorable on another device (Android or iOS). */
    val backupManager by lazy {
        BackupManager(
            configRepository, uiPreferences, clusterColors, clusterNames, vpnOnly, kubeServers, wakeOnLan, monitorStore,
            setPrivacyMask = ::setPrivacyMask,
            notificationsAllowed = { canPostNotifications(this) },
            onRestored = {
                talosRepository.invalidate()
                forgetShownCluster()
            },
        )
    }

    /** Re-evaluates whether background monitoring should run (alerts on or widget placed). */
    fun launchSync(runNow: Boolean = false) {
        ProcessLifecycleOwner.get().lifecycleScope.launch { syncMonitoring(this@TalosApp, runNow) }
    }

    /** Shows another cluster (a context of the stored config); the widget and the alerts follow it. */
    fun selectCluster(name: String) {
        if (name == configRepository.config.value?.activeContext) return
        configRepository.selectContext(name)
        forgetShownCluster()
    }

    /** Gives the cluster [fingerprint] the name [name] (blank: its context name again); the widget follows. */
    fun renameCluster(fingerprint: String, name: String) {
        clusterNames.set(fingerprint, name)
        ProcessLifecycleOwner.get().lifecycleScope.launch { ClusterWidget().updateAll(this@TalosApp) }
    }

    /**
     * Sets whether the cluster [fingerprint] is reached over a VPN only. When it is the one
     * on screen it reloads: with the VPN off, the screens say to connect it instead.
     */
    fun setVpnOnly(fingerprint: String, vpnOnly: Boolean) {
        this.vpnOnly.set(fingerprint, vpnOnly)
        if (fingerprint == configRepository.config.value?.activeSummary?.fingerprint) talosRepository.invalidate()
    }

    /**
     * Sets the Kubernetes API address of the cluster [fingerprint] ([server] checked by
     * Ichorgo.normalizeKubeServer, blank for the kubeconfig's); its cached results go.
     */
    fun setKubeServer(fingerprint: String, server: String) {
        kubeServers.set(fingerprint, server)
        if (fingerprint == configRepository.config.value?.activeSummary?.fingerprint) talosRepository.invalidate()
    }

    /** Throws [VpnRequiredException] instead of trying a VPN-only cluster while no VPN is up. */
    private fun holdBackOffVpn(stored: StoredConfig) {
        if (heldBackForVpn(vpnOnly.fingerprints.value, stored.activeSummary?.fingerprint, vpn.isUp())) {
            throw VpnRequiredException()
        }
    }

    /** Once the VPN is up, a VPN-only cluster on screen reloads and is checked in the background. */
    private fun reloadWhenVpnConnects() {
        ProcessLifecycleOwner.get().lifecycleScope.launch {
            vpn.up.drop(1).filter { it }.collect {
                val shown = configRepository.config.value?.activeSummary?.fingerprint
                if (shown != null && shown in vpnOnly.fingerprints.value) {
                    talosRepository.invalidate()
                    syncMonitoring(this@TalosApp, runNow = true)
                }
            }
        }
    }

    /** Removes a cluster from the stored config; false when it was the last one (nothing is stored anymore). */
    /** Adds discovered [nodes] to the cluster [name]'s talosconfig context; screens and monitoring follow. */
    suspend fun addClusterNodes(name: String, nodes: List<String>) {
        configRepository.addNodes(name, nodes)
        talosRepository.invalidate()
        launchSync(runNow = true)
    }

    /** Replaces the endpoints of the cluster [name]'s talosconfig context; screens and monitoring follow. */
    suspend fun setClusterEndpoints(name: String, endpoints: List<String>) {
        configRepository.setEndpoints(name, endpoints)
        talosRepository.invalidate()
        launchSync(runNow = true)
    }

    /**
     * Adds the endpoints a network search found to the contexts each one answered for. Once a
     * cluster answers again, node discovery offers the members it still misses.
     */
    suspend fun addFoundEndpoints(matches: List<EndpointMatch>) {
        configRepository.addEndpoints(matches)
        talosRepository.invalidate()
        launchSync(runNow = true)
    }

    suspend fun removeCluster(name: String): Boolean {
        val wasShown = name == configRepository.config.value?.activeContext
        val remains = configRepository.removeContext(name)
        if (wasShown) forgetShownCluster() else launchSync(runNow = true)
        return remains
    }

    /**
     * Another cluster is on screen: the widget drops the previous one's nodes right away
     * (the next check may not reach the new cluster, and would then keep them) and a check
     * of the new one runs.
     */
    private fun forgetShownCluster() {
        monitorStore.clearSnapshot()
        ProcessLifecycleOwner.get().lifecycleScope.launch {
            ClusterWidget().updateAll(this@TalosApp)
            syncMonitoring(this@TalosApp, runNow = true)
        }
    }

    /**
     * Turns screenshot mode on or off. Everything fetched under the previous setting is
     * dropped (in-memory results, the monitor snapshot behind the widget) and refetched,
     * so no real name or address lingers on screen or on the home screen.
     */
    fun setPrivacyMask(mask: PrivacyMask) {
        if (mask == uiPreferences.privacyMask.value) return
        uiPreferences.setPrivacyMask(mask)
        applyPrivacyMask(mask)
        // Keys differ masked vs unmasked: diffing across the switch would alert on every node.
        monitorStore.clearSnapshot()
        ProcessLifecycleOwner.get().lifecycleScope.launch {
            // The config summary (context names, endpoints) is masked too, and the Go side
            // forgets its previous mapping: re-read it, then reload with the new names.
            runCatching { configRepository.reparse() }
            // Kept under the previous setting: masked and real names must never mix.
            offlineCache.clear()
            talosRepository.invalidate()
            ClusterWidget().updateAll(this@TalosApp)
            syncMonitoring(this@TalosApp, runNow = true)
        }
    }

    private fun applyPrivacyMask(mask: PrivacyMask) = Ichorgo.setPrivacyMask(mask.enabled, mask.words)

    /**
     * The key the Go core encrypts what it remembers with, created once and kept encrypted by
     * a Keystore key ([SecureStore]). One that no longer decrypts is replaced: what was sealed
     * with it is then forgotten. Null when the Keystore cannot be used: nothing is remembered.
     */
    private fun coreDataKey(): ByteArray? {
        val store = SecureStore(java.io.File(noBackupFilesDir, "core-key.enc"), "ichor-core-key")
        runCatching { store.read() }.getOrNull()?.takeIf { it.size == CORE_KEY_SIZE }?.let { return it }
        return runCatching { ByteArray(CORE_KEY_SIZE).also { java.security.SecureRandom().nextBytes(it); store.write(it) } }.getOrNull()
    }

    /**
     * Turns "Keep last known state" on or off. Off deletes everything kept and its key; on
     * starts with what the screens fetch next.
     */
    fun setOfflineCache(enabled: Boolean) {
        if (enabled == uiPreferences.offlineCache.value) return
        uiPreferences.setOfflineCache(enabled)
        ProcessLifecycleOwner.get().lifecycleScope.launch(Dispatchers.IO) {
            if (enabled) talosRepository.restoreOffline() else offlineCache.clear()
        }
    }

    /**
     * Keeps the last known data in step with the config: the active cluster's is read back
     * when it is shown, removed clusters' is deleted, and all of it once no config is left.
     */
    private fun syncOfflineCache() {
        ProcessLifecycleOwner.get().lifecycleScope.launch(Dispatchers.IO) {
            combine(configRepository.config, configRepository.generation) { stored, generation -> stored to generation }
                .collect { (stored, generation) ->
                    when {
                        stored != null -> {
                            offlineCache.retain(stored.summary.contexts.map { it.fingerprint })
                            talosRepository.restoreOffline()
                        }
                        // Generation 0: not loaded yet (locked); later, every cluster was removed.
                        generation > 0 -> offlineCache.clear()
                    }
                }
        }
    }

    override fun onCreate() {
        super.onCreate()
        syncLanguage()
        // Before any Talos call: the monitor worker and the widget run in this process too.
        applyPrivacyMask(uiPreferences.privacyMask.value)
        // Where Go remembers node names, so a node that is down still shows its hostname.
        Ichorgo.setDataDir(noBackupFilesDir.path, coreDataKey() ?: ByteArray(0))
        launchSync()
        // Every cluster of the stored config gets a color of its own, as soon as it shows up.
        ProcessLifecycleOwner.get().lifecycleScope.launch {
            configRepository.config.collect { stored ->
                stored?.let {
                    clusterColors.sync(it.summary)
                    clusterNames.sync(it.summary)
                    wakeOnLan.sync(it.summary)
                    publicIps.sync(it.summary)
                    vpnOnly.sync(it.summary)
                    kubeServers.sync(it.summary)
                }
                // A removed cluster (or the deleted config) takes its shells with it.
                if (stored != null || configRepository.generation.value > 0) {
                    debugShells.retainContexts(stored?.summary?.contexts.orEmpty().map { c -> c.name }.toSet())
                }
            }
        }
        publishClusterShortcuts()
        reloadWhenVpnConnects()
        syncOfflineCache()
        // Process-wide foreground/background, so moving between our own screens never relocks.
        ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) = appLock.onForeground()
            override fun onStop(owner: LifecycleOwner) = appLock.onBackground()
        })
    }

    /**
     * Keeps a launcher shortcut per cluster, labelled and colored like in the app. Until the
     * config is loaded (generation 0, e.g. before the first unlock) the previous ones stay;
     * once every cluster is removed, so are they.
     */
    private fun publishClusterShortcuts() {
        val sources = combine(
            configRepository.config,
            configRepository.generation,
            clusterColors.colors,
            clusterNames.names,
            uiPreferences.privacyMask,
        ) { stored, generation, colors, names, mask ->
            when {
                stored != null -> clusterShortcuts(
                    stored.summary,
                    ClusterLabels(names, mask.enabled),
                    colors,
                    ClusterShortcuts.max(this),
                ) to clusterShortcutIds(stored.summary)
                generation > 0 -> emptyList<ShortcutSpec>() to emptySet()
                else -> null
            }
        }
        ProcessLifecycleOwner.get().lifecycleScope.launch(Dispatchers.Default) {
            sources.filterNotNull().distinctUntilChanged().collect { (specs, known) ->
                ClusterShortcuts.publish(this@TalosApp, specs, known)
            }
        }
    }

    /**
     * On API 33+ the system owns the per-app language (it can also be changed in Android
     * settings), so mirror it into the stored choice shown in Settings. Below 33,
     * MainActivity applies the stored choice itself.
     */
    private fun syncLanguage() {
        val system = AppLocale.systemChoice(this) ?: return
        if (system != uiPreferences.language.value) uiPreferences.setLanguage(system)
    }
}

/** AES-256: what Ichorgo.setDataDir takes. */
private const val CORE_KEY_SIZE = 32
