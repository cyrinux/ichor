package name.levis.ichor

import name.levis.ichorgo.Ichorgo
import name.levis.ichor.util.StartupTrace
import android.app.Application
import android.content.Context
import android.os.SystemClock
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.lifecycleScope
import name.levis.ichor.data.KubePermissions
import name.levis.ichor.data.RunbookManager
import name.levis.ichor.data.RunbookRepository
import name.levis.ichor.data.CARotationManager
import name.levis.ichor.data.PodSweepManager
import name.levis.ichor.data.BackupManager
import name.levis.ichor.data.ClusterRepoStore
import name.levis.ichor.data.StagedEdits
import name.levis.ichor.data.ProposalsStore
import name.levis.ichor.data.AiPreferences
import name.levis.ichor.data.CaptureRepository
import name.levis.ichor.data.CiliumRepository
import name.levis.ichor.data.AutoscalingRepository
import name.levis.ichor.data.PolicyRepository
import name.levis.ichor.data.PluginRepository
import name.levis.ichor.data.VirtualMachineRepository
import name.levis.ichor.data.ArgoRolloutsRepository
import name.levis.ichor.data.NetPerfHistory
import name.levis.ichor.data.ImageScanRepository
import name.levis.ichor.data.NetPerfRepository
import name.levis.ichor.data.PublicIpRepository
import name.levis.ichor.data.ChangelogRepository
import name.levis.ichor.data.ClusterColors
import name.levis.ichor.data.DiagnosisRepository
import name.levis.ichor.localmodel.LocalModelHost
import name.levis.ichor.localmodel.LocalModelStore
import name.levis.ichor.data.MetricsChatRepository
import name.levis.ichor.data.SecureStore
import name.levis.ichor.data.KeystoreSealer
import name.levis.ichor.data.KeystoreValue
import name.levis.ichor.data.OfflineCache
import name.levis.ichor.data.SearchRepository
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.TalosUpdateChecker
import name.levis.ichor.data.UpgradeManager
import name.levis.ichor.data.ImagePullManager
import name.levis.ichor.data.GitOpsOperationManager
import name.levis.ichor.data.NetPerfManager
import name.levis.ichor.data.RolloutManager
import name.levis.ichor.data.MaintenanceManager
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.EtcdRepository
import name.levis.ichor.data.MachineConfigRepository
import name.levis.ichor.data.NodeLifecycleRepository
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.data.GitHostStore
import name.levis.ichor.model.BackupLockMode
import name.levis.ichor.model.isDemo
import name.levis.ichor.data.GoCall
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.data.SupportBundleRepository
import name.levis.ichor.data.SupportPrompt
import name.levis.ichor.data.FundingHistory
import name.levis.ichor.data.RoadmapRepository
import name.levis.ichor.data.createFeatureStore
import name.levis.ichor.data.createGoogleNativeSignIn
import name.levis.ichor.data.PrivacyMask
import name.levis.ichor.data.MaskSwitcher
import name.levis.ichor.data.MaskEpoch
import name.levis.ichor.data.UiPreferences
import name.levis.ichor.data.PinStore
import androidx.glance.appwidget.updateAll
import name.levis.ichor.widget.ClusterWidget
import name.levis.ichor.widget.WidgetClusters
import name.levis.ichor.ui.apps.AppIconLoader
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.monitor.AlertActionToken
import name.levis.ichor.monitor.AlertSnoozes
import name.levis.ichor.monitor.MonitorStore
import name.levis.ichor.monitor.wholeClusters
import name.levis.ichor.monitor.seed
import name.levis.ichor.monitor.toKubeNodesSeed
import name.levis.ichor.monitor.toOverviewSeed
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.data.realFingerprint
import name.levis.ichor.monitor.canPostNotifications
import name.levis.ichor.monitor.syncMonitoring
import name.levis.ichor.security.AppLock
import name.levis.ichor.security.SecurityKeyPrompts
import name.levis.ichor.security.PrefsLockSettings
import name.levis.ichor.update.UpdateManager
import name.levis.ichor.update.createStoreUpdater
import name.levis.ichor.wear.createWearSnapshotSync
import name.levis.ichor.wear.wearPayload
import name.levis.ichor.ui.debug.DebugShells
import name.levis.ichor.ui.OperationService
import name.levis.ichor.ui.operations.OperationKind
import name.levis.ichor.data.JoinManager
import name.levis.ichor.data.K8sUpgradeManager
import name.levis.ichor.data.NetToolTargets
import name.levis.ichor.data.ClusterUpgradeManager
import name.levis.ichor.data.ConfigTryManager
import name.levis.ichor.data.ConfigMultiManager
import name.levis.ichor.data.ClusterNames
import name.levis.ichor.data.WakeOnLanStore
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.data.VpnMonitor
import name.levis.ichor.data.AuditEventClusters
import name.levis.ichor.data.UnwatchedClusters
import name.levis.ichor.data.VpnOnlyClusters
import name.levis.ichor.data.KubeScopes
import name.levis.ichor.data.KubeServers
import name.levis.ichor.data.KubeAccess
import name.levis.ichor.data.KubeAuthRepository
import name.levis.ichor.data.KubeAuthStore
import name.levis.ichor.data.SecureStoreValue
import name.levis.ichor.data.hasStrongBox
import name.levis.ichor.data.SkippedTalosUpdates
import name.levis.ichor.data.SnapshotKeys
import name.levis.ichor.data.MetricsStore
import name.levis.ichor.data.AlertmanagerStore
import name.levis.ichor.data.ClusterSecureFiles
import name.levis.ichor.data.HistoryRepository
import name.levis.ichor.data.HistoryStore
import name.levis.ichor.data.LastLooked
import name.levis.ichor.data.LiveFocus
import name.levis.ichor.data.AlertmanagerRepository
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.data.VpnRequiredException
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.heldBackForVpn
import name.levis.ichor.model.seededAuditEventClusters
import name.levis.ichor.model.signInKeys
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.shortcuts.ClusterShortcuts
import name.levis.ichor.shortcuts.ShortcutSpec
import name.levis.ichor.shortcuts.clusterShortcutIds
import name.levis.ichor.shortcuts.clusterShortcuts
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Holds app-wide singletons (manual DI; the app is small). */
class TalosApp : Application() {
    val configRepository by lazy { ConfigRepository(this, guard = ::holdBackOffVpn, kubeAccess = { kubeAccess.links.value }, dekHolder = appLock) }

    /** The sign-ins of kubeconfig clusters (tokens, keys typed), sealed like the configs; the Go core reads and writes it. */
    val kubeAuthStore by lazy {
        KubeAuthStore(SecureStoreValue(java.io.File(filesDir, KubeAuthStore.FILE), KubeAuthStore.KEY_ALIAS, hasStrongBox(packageManager), appLock))
    }

    /** The "tap your security key" prompts the app lock shows (see SecurityKeyPrompts). */
    val keyPrompts by lazy { SecurityKeyPrompts() }

    /**
     * Seals or unseals the credential files (configs, kubeconfig sign-ins) after the security-key
     * requirement changed; the data key must be held while this runs (see AppLock.provideDek).
     */
    suspend fun resealCredentialStores() {
        configRepository.reseal()
        kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) { kubeAuthStore.reseal() }
    }
    val kubeAuthRepository by lazy { KubeAuthRepository(configRepository) }
    /** The Go core's credentials and the results cache, shared by the repositories below. */
    private val goCall by lazy { GoCall(configRepository, kubeServers, offlineCache, idle = maskSwitcher::awaitIdle) }
    val talosRepository by lazy { TalosRepository(goCall) }
    val etcdRepository by lazy { EtcdRepository(goCall) }
    val machineConfigRepository by lazy { MachineConfigRepository(goCall) }
    val nodeLifecycleRepository by lazy { NodeLifecycleRepository(goCall) }
    val kubeRepository by lazy { KubeRepository(goCall) }
    val gitOpsRepository by lazy { GitOpsRepository(goCall, gitHostStore, clusterRepos, proposals) }

    /** The pull requests opened from the phone, per cluster; no token in them. */
    val proposals by lazy { ProposalsStore(getSharedPreferences(ProposalsStore.FILE, Context.MODE_PRIVATE)) }

    /** Edits staged in the repository browser for one pull request: this session only. */
    val stagedEdits by lazy { StagedEdits() }

    /** The Git repository saved for each cluster (its Talos and Kubernetes config); no token in it. */
    val clusterRepos by lazy { ClusterRepoStore(getSharedPreferences(ClusterRepoStore.FILE, Context.MODE_PRIVATE)) }

    /** Git host tokens (Settings → Git hosts): one Keystore key and file per host. */
    val gitHostStore by lazy {
        GitHostStore(
            getSharedPreferences(GitHostStore.FILE, Context.MODE_PRIVATE),
            { host ->
                val name = GitHostStore.tokenName(host)
                SecureStoreValue(java.io.File(filesDir, "$name.enc"), name, strongBox = false)
            },
            ProcessLifecycleOwner.get().lifecycleScope,
            demo = { configRepository.config.value?.activeSummary?.isDemo == true },
        )
    }

    /** Search across clusters: what every cluster last showed, never a cluster call. */
    val searchRepository by lazy {
        SearchRepository(
            summary = { configRepository.config.value?.summary },
            active = { configRepository.config.value?.activeContext },
            offline = offlineCache,
            offlineEnabled = { uiPreferences.offlineCache.value },
            results = goCall.results,
        )
    }
    val dataServicesRepository by lazy { DataServicesRepository(goCall) }
    val pluginRepository by lazy { PluginRepository(goCall) }
    val alertmanagerRepository by lazy { AlertmanagerRepository(goCall) }

    /** Last known cluster data on disk, only while "Keep last known state" is on (Settings → Privacy). */
    internal val offlineCache by lazy {
        OfflineCache(
            java.io.File(noBackupFilesDir, "offline"),
            KeystoreSealer("ichor-offline"),
            enabled = { uiPreferences.offlineCache.value },
        )
    }
    /** Debug shells, kept open across screens until exited (see DebugShellService). */
    val debugShells by lazy { DebugShells(this, configRepository, kubeServers) }
    val captureRepository by lazy { CaptureRepository(configRepository, filesDir) }
    val netPerfRepository by lazy { NetPerfRepository(configRepository, kubeServers) }
    /** The image vulnerability scan running or last run, app-wide; OperationService keeps the app alive while it runs. */
    val imageScanRepository by lazy {
        ImageScanRepository(
            configRepository,
            kubeServers,
            scanner = { uiPreferences.imageScanner.value },
            onStarted = { OperationService.start(this, OperationKind.IMAGE_SCAN) },
        )
    }
    val ciliumRepository by lazy { CiliumRepository(configRepository, kubeServers) }
    val policyRepository by lazy { PolicyRepository(configRepository, kubeServers) }
    /** KubeVirt virtual machines and their start/stop/restart/pause/migrate actions. */
    val vmRepository by lazy { VirtualMachineRepository(goCall) }
    /** Argo Rollouts and their promote/abort/retry/pause actions. */
    val argoRolloutsRepository by lazy { ArgoRolloutsRepository(goCall) }
    val autoscalingRepository by lazy { AutoscalingRepository(configRepository, kubeServers) }
    /** Any kind as YAML, Helm releases, followed pod logs and port-forwards (Kubernetes API only). */
    val kubeBrowser by lazy { KubeBrowserRepository(configRepository, kubeServers) }
    /** Which Kubernetes actions the active cluster's credentials may run, and who they are. */
    val kubePermissions by lazy { KubePermissions(configRepository, kubeServers) }
    val netPerfHistory by lazy { NetPerfHistory(java.io.File(noBackupFilesDir, "netperf")) }
    val publicIps by lazy {
        PublicIpRepository(
            configRepository,
            kubeServers,
            KeystoreValue(java.io.File(noBackupFilesDir, "public-ips.enc"), "ichor-public-ips"),
            masked = { uiPreferences.privacyMask.value.enabled },
        )
    }
    val supportBundleRepository by lazy {
        SupportBundleRepository(configRepository, kubeServers, filesDir) { configRepository.config.value?.activeSummary?.fingerprint?.let { historyRepository.export(it) } }
    }
    /** The followed upgrade; OperationService keeps the app alive while it runs. */
    val upgradeManager by lazy {
        UpgradeManager(configRepository, kubeServers, onStarted = { OperationService.start(this, OperationKind.UPGRADE) }, onFinished = talosRepository::forgetFeatures)
    }
    /** The followed node maintenance; OperationService keeps the app alive while it runs. */
    val maintenanceManager by lazy { MaintenanceManager(nodeLifecycleRepository, onStarted = { OperationService.start(this, OperationKind.MAINTENANCE) }) }
    /** The followed node join; OperationService keeps the app alive while it runs. */
    val joinManager by lazy {
        JoinManager(
            nodeLifecycleRepository,
            configRepository,
            text = { id, args -> AppLocale.wrap(this).getString(id, *args.toTypedArray()) },
            onStarted = { OperationService.start(this, OperationKind.JOIN) },
        )
    }
    /** The followed machine config try; OperationService keeps its countdown going off screen. */
    val configTryManager by lazy { ConfigTryManager(machineConfigRepository::tryMachineConfig, onStarted = { OperationService.start(this, OperationKind.CONFIG_TRY) }) }
    /** The followed multi-node config apply; OperationService keeps the app alive while it runs. */
    val configMultiManager by lazy {
        ConfigMultiManager(machineConfigRepository::applyMachineConfigMulti, onStarted = { OperationService.start(this, OperationKind.CONFIG_MULTI) })
    }
    /** The followed rolling cluster upgrade; OperationService keeps the app alive while it runs. */
    val clusterUpgradeManager by lazy {
        ClusterUpgradeManager(nodeLifecycleRepository::startClusterUpgrade, onStarted = { OperationService.start(this, OperationKind.CLUSTER_UPGRADE) })
    }
    /** The followed Kubernetes upgrade; OperationService keeps the app alive while it runs. */
    val k8sUpgradeManager by lazy {
        K8sUpgradeManager(nodeLifecycleRepository::startK8sUpgrade, onStarted = { OperationService.start(this, OperationKind.K8S_UPGRADE) })
    }
    /** The followed image pull; OperationService keeps the app alive while it runs. */
    val imagePullManager by lazy { ImagePullManager(talosRepository, onStarted = { OperationService.start(this, OperationKind.IMAGE_PULL) }) }
    /** The followed Argo CD sync and Flux reconcile started from the phone; OperationService keeps the app alive meanwhile. */
    val gitOpsOperations by lazy {
        GitOpsOperationManager(
            gitOpsRepository::startArgoSyncFollow,
            gitOpsRepository::startFluxReconcileFollow,
            onArgoStarted = { OperationService.start(this, OperationKind.ARGO_SYNC) },
            onFluxStarted = { OperationService.start(this, OperationKind.FLUX_RECONCILE) },
        )
    }
    /** The followed rollout after a restart; OperationService keeps the app alive while it rolls. */
    val rolloutManager by lazy { RolloutManager(kubeRepository::rolloutWatch, onStarted = { OperationService.start(this, OperationKind.ROLLOUT) }) }
    /** The network test running or last run, app-wide; OperationService keeps the app alive while it runs. */
    val netPerfManager by lazy {
        NetPerfManager(
            netPerfRepository::start,
            // A failed write loses this test from the history only.
            save = { scope, report -> withContext(Dispatchers.IO) { runCatching { netPerfHistory.add(scope, report) } } },
            onStarted = { OperationService.start(this, OperationKind.NETPERF) },
        )
    }
    val runbookRepository by lazy { RunbookRepository(goCall) }
    /** The runbook running or last run, app-wide; OperationService keeps the app alive while it runs. */
    val runbookManager by lazy { RunbookManager(runbookRepository::start, onStarted = { OperationService.start(this, OperationKind.RUNBOOK) }) }
    /** The "Clean up dead pods" sweep running or last run, app-wide; OperationService keeps the app alive while it runs. */
    val podSweepManager by lazy { PodSweepManager(kubeRepository::startSanitize, onStarted = { OperationService.start(this, OperationKind.POD_SWEEP) }) }
    /** The Talos CA rotation running or last run, app-wide; OperationService keeps the app alive while it runs. */
    val caRotationManager by lazy {
        CARotationManager(
            nodeLifecycleRepository::startCARotation,
            store = { context, yaml -> configRepository.replaceCredentials(context, yaml) },
            activeContext = { configRepository.config.value?.activeContext.orEmpty() },
            onStarted = { OperationService.start(this, OperationKind.CA_ROTATION) },
        )
    }
    val talosUpdateChecker by lazy { TalosUpdateChecker() }
    /** The Talos release each cluster's update card was skipped for. */
    val skippedTalosUpdates by lazy { SkippedTalosUpdates(getSharedPreferences(SkippedTalosUpdates.FILE, Context.MODE_PRIVATE)) }
    /** The network tools' recent targets, per cluster. */
    val netToolTargets by lazy { NetToolTargets(getSharedPreferences(NetToolTargets.FILE, Context.MODE_PRIVATE)) }
    val uiPreferences by lazy { UiPreferences(getSharedPreferences(UiPreferences.FILE, Context.MODE_PRIVATE)) }
    /** The objects pinned to each cluster's overview. */
    val pinStore by lazy { PinStore(getSharedPreferences(UiPreferences.FILE, Context.MODE_PRIVATE)) }
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
    /** The clusters the background monitor leaves out (all are watched by default). */
    val unwatchedClusters by lazy { UnwatchedClusters(getSharedPreferences(UnwatchedClusters.FILE, Context.MODE_PRIVATE)) }
    /** The clusters that record actions as Kubernetes Events (none by default). */
    val auditEventClusters by lazy { AuditEventClusters(getSharedPreferences(AuditEventClusters.FILE, Context.MODE_PRIVATE)) }
    /** The cluster each home-screen widget shows. */
    val widgetClusters by lazy { WidgetClusters(getSharedPreferences(WidgetClusters.FILE, Context.MODE_PRIVATE)) }
    val kubeServers by lazy { KubeServers(getSharedPreferences(KubeServers.FILE, Context.MODE_PRIVATE)) }
    /** The kubeconfig cluster each Talos cluster's Kubernetes calls go through, when not the Talos admin kubeconfig. */
    val kubeAccess by lazy { KubeAccess(getSharedPreferences(KubeAccess.FILE, Context.MODE_PRIVATE)) }
    /** The namespace each cluster's Kubernetes screen lists. */
    val kubeScopes by lazy { KubeScopes(getSharedPreferences(KubeScopes.FILE, Context.MODE_PRIVATE)) }
    /** Public keys each cluster's etcd snapshots are encrypted for. */
    val snapshotKeys by lazy { SnapshotKeys(getSharedPreferences(SnapshotKeys.FILE, Context.MODE_PRIVATE)) }
    /** Each cluster's Prometheus/Mimir source and saved PromQL panels (Metrics screen). */
    val metricsStore by lazy { MetricsStore(this) }
    val alertmanagerStore by lazy { AlertmanagerStore(this) }
    /** Each cluster's 30-day history ring, one record per monitor run, sealed per cluster. */
    val historyStore by lazy {
        HistoryStore(ClusterSecureFiles(this, "history")) { ring, record, now -> Ichorgo.historyAppend(ring, record, now) }
    }
    val historyRepository by lazy { HistoryRepository(historyStore) }
    /** When the user last looked at each cluster's home ("since you last looked"). */
    val lastLooked by lazy { LastLooked(getSharedPreferences(LastLooked.FILE, Context.MODE_PRIVATE)) }
    val vpn by lazy { VpnMonitor(this) }
    /** The nodes the live cluster summary samples on every tick of a large cluster. */
    val liveFocus by lazy { LiveFocus() }
    val appLock by lazy {
        AppLock(
            PrefsLockSettings(getSharedPreferences("ichor-security", Context.MODE_PRIVATE)),
            clock = SystemClock::elapsedRealtime,
        )
    }

    val supportPrompt by lazy { SupportPrompt(getSharedPreferences("ichor-support", Context.MODE_PRIVATE)) }
    val fundingHistory by lazy { FundingHistory(getSharedPreferences(FundingHistory.FILE, Context.MODE_PRIVATE)) }
    val featureStore by lazy { createFeatureStore(this, fundingHistory) }
    val googleNativeSignIn by lazy { createGoogleNativeSignIn() }
    val roadmapRepository by lazy { RoadmapRepository(getSharedPreferences(RoadmapRepository.FILE, Context.MODE_PRIVATE)) }
    val changelogRepository by lazy { ChangelogRepository(this, getSharedPreferences(ChangelogRepository.PREFS, Context.MODE_PRIVATE)) }
    val updateManager by lazy {
        UpdateManager(this, getSharedPreferences("ichor-update", Context.MODE_PRIVATE), changelogRepository)
    }
    /** Google Play in-app updates in the Play build (the others use [updateManager]). */
    val storeUpdater by lazy { createStoreUpdater(this) }
    private val wearSync by lazy { createWearSnapshotSync(this) }

    /**
     * Sends the Wear OS tile what the widget shows, with the same names (masked in screenshot
     * mode). A no-op in the open-source builds and without Google Play services.
     */
    fun syncWear() {
        val stored = configRepository.config.value
        val payload = wearPayload(
            monitorStore.state.value,
            stored?.summary,
            stored?.activeContext,
            clusterNames.names.value,
            uiPreferences.privacyMask.value.enabled,
        )
        wearSync.send(payload)
    }

    /** Bundled app icons, and downloaded ones when the user allowed it (Settings → Privacy). */
    val appIcons by lazy { AppIconLoader(this) }
    val monitorStore by lazy {
        MonitorStore(
            getSharedPreferences("ichor-monitor", Context.MODE_PRIVATE),
            KeystoreValue(java.io.File(noBackupFilesDir, "monitor-snapshot.enc"), "ichor-monitor-snapshot"),
        )
    }

    /** The Talos home from the last background check of the cluster on screen, until it answers. */
    fun overviewSeed(): TalosRepository.Timed<ClusterOverview>? =
        monitorStore.seed(configRepository.config.value?.realFingerprint, kube = false, masked = uiPreferences.privacyMask.value.enabled) { it.toOverviewSeed() }

    /** The Kubernetes home's nodes from the last background check of the cluster on screen. */
    fun kubeNodesSeed(): TalosRepository.Timed<KubeNodesOverview>? =
        monitorStore.seed(configRepository.config.value?.realFingerprint, kube = true, masked = uiPreferences.privacyMask.value.enabled) { it.toKubeNodesSeed() }

    /** The alerts snoozed from their notification. */
    val alertSnoozes by lazy { AlertSnoozes(getSharedPreferences(AlertSnoozes.FILE, Context.MODE_PRIVATE)) }

    /** The secret an alert's in-app button carries, so no other app can ask for its confirmation. */
    val alertActionToken by lazy { AlertActionToken(getSharedPreferences(AlertSnoozes.FILE, Context.MODE_PRIVATE)) }

    /** The optional AI diagnosis: off until enabled in Settings. API keys get their own Keystore keys. */
    val diagnosisRepository by lazy { DiagnosisRepository(configRepository, kubeServers) }
    /** The panel assistant of the Metrics screen, with the same providers and keys. */
    val metricsChatRepository by lazy { MetricsChatRepository(configRepository, kubeServers) }
    val aiPreferences by lazy {
        AiPreferences(
            getSharedPreferences(AiPreferences.FILE, Context.MODE_PRIVATE),
            diagnosisRepository.providers.map { it.id },
            { provider -> SecureStore(java.io.File(filesDir, "ai-key-$provider.enc"), keyAlias = "ai-key-$provider") },
            ProcessLifecycleOwner.get().lifecycleScope,
        )
    }

    /** The model files of the "This phone" AI provider (downloaded or imported), and the engine that runs them. */
    val localModels by lazy { LocalModelStore(this) }
    val localModelHost by lazy { LocalModelHost(localModels, ProcessLifecycleOwner.get().lifecycleScope) }

    /** Passphrase-sealed backups of the config and settings, restorable on another device (Android or iOS). */
    val backupManager by lazy {
        BackupManager(
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
        )
    }

    /** Re-evaluates whether background monitoring should run (alerts on or widget placed). */
    fun launchSync(runNow: Boolean = false) {
        ProcessLifecycleOwner.get().lifecycleScope.launch { syncMonitoring(this@TalosApp, runNow) }
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

    /**
     * Another cluster is on screen: the widgets that follow it show its own snapshot (each
     * cluster keeps one), and a check runs.
     */
    internal fun forgetShownCluster() {
        ProcessLifecycleOwner.get().lifecycleScope.launch {
            ClusterWidget().updateAll(this@TalosApp)
            syncWear()
            syncMonitoring(this@TalosApp, runNow = true)
        }
    }

    /**
     * Switches screenshot mode (see [MaskSwitcher]); the screens show a placeholder meanwhile
     * and every repository call waits, so no name read under one setting is drawn under the other.
     */
    val maskSwitcher: MaskSwitcher by lazy {
        MaskSwitcher(
            current = { uiPreferences.privacyMask.value },
            dropReadData = {
                // Kept under the previous setting: masked and real names must never mix.
                talosRepository.invalidate()
                withContext(Dispatchers.IO) {
                    offlineCache.clear()
                    // Keys differ masked vs unmasked: diffing across the switch would alert on every node.
                    monitorStore.clearSnapshots()
                    uiPreferences.clearRecentSearches()
                }
            },
            apply = { mask ->
                withContext(Dispatchers.IO) {
                    applyPrivacyMask(mask)
                    uiPreferences.setPrivacyMask(mask)
                }
            },
            // The config summary (context names, endpoints) is masked too, and the Go side
            // forgets its previous mapping: re-read it before anything reloads.
            reread = { runCatching { configRepository.reparse() } },
            // What a call in flight before the switch cached meanwhile goes too; the screens
            // reload once the switch is over (their calls wait for it).
            reload = { talosRepository.invalidate() },
            epochs = MaskEpoch.counter,
        )
    }

    /** True while screenshot mode switches: the root navigation covers every screen. */
    val maskTransition: StateFlow<Boolean> get() = maskSwitcher.transition

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
     * Keeps the last known data in step with the config: the active cluster's is read back
     * when it is shown, removed clusters' is deleted, and all of it once no config is left.
     */
    private fun syncOfflineCache() {
        ProcessLifecycleOwner.get().lifecycleScope.launch(Dispatchers.IO) {
            combine(configRepository.config, configRepository.generation) { stored, generation -> stored to generation }
                .collect { (stored, generation) ->
                    when {
                        stored != null -> {
                            // The cluster on screen first: deleting the removed ones' can wait.
                            talosRepository.restoreOffline()
                            offlineCache.retain(stored.summary.contexts.map { it.fingerprint })
                        }
                        // Generation 0: not loaded yet (locked); later, every cluster was removed.
                        generation > 0 -> {
                            offlineCache.clear()
                            uiPreferences.clearRecentSearches()
                        }
                    }
                }
        }
    }

    override fun onCreate() {
        super.onCreate()
        // First, so the trace sees the whole start (when on, this first Go call loads the core).
        StartupTrace.setEnabled(BuildConfig.DEBUG || uiPreferences.startupTrace.value)
        StartupTrace.markOnce("app.onCreate start")
        syncLanguage()
        // Errors turned into text away from a screen are localized with the app's language.
        name.levis.ichor.ui.AppTexts.context = this
        // Before any Go call: the monitor worker and the widget run in this process too.
        Ichorgo.setAuthStore(kubeAuthStore)
        applyPrivacyMask(uiPreferences.privacyMask.value)
        applyAuditEventClusters()
        // Where Go remembers node names, so a node that is down still shows its hostname.
        Ichorgo.setDataDir(noBackupFilesDir.path, coreDataKey() ?: ByteArray(0))
        StartupTrace.markOnce("app.goInit done")
        // GKE's "Sign in with Google" (Play build with Play services): Google's SDK holds the
        // client, so no client ID here.
        if (googleNativeSignIn.available(this)) Ichorgo.setGoogleSignInClient("android", "")
        // The on-device model: nothing is loaded until the user asks it a question.
        if (BuildConfig.LOCAL_MODEL) Ichorgo.setLocalModel(localModelHost)
        launchSync()
        // Every cluster of the stored config gets a color of its own, as soon as it shows up.
        ProcessLifecycleOwner.get().lifecycleScope.launch {
            configRepository.config.collect { stored ->
                stored?.let {
                    clusterColors.sync(it.summary)
                    clusterNames.sync(it.summary)
                    wakeOnLan.sync(it.summary)
                    // Rewrites a sealed file (a Keystore call): off the main thread, its store is locked.
                    launch(Dispatchers.IO) { publicIps.sync(it.summary) }
                    vpnOnly.sync(it.summary)
                    clusterRepos.sync(it.summary)
                    proposals.sync(it.summary)
                    unwatchedClusters.sync(it.summary)
                    // The app-wide switch of older versions, once: on, every cluster keeps recording.
                    seededAuditEventClusters(uiPreferences.takeLegacyAuditEvents(), it.summary)
                        .forEach { fp -> auditEventClusters.set(fp, true) }
                    // Per cluster: a context added to a recording cluster records too; removed ones go.
                    auditEventClusters.replaceWith(wholeClusters(auditEventClusters.fingerprints.value, it.summary))
                    applyAuditEventClusters()
                    widgetClusters.sync(it.summary)
                    // A removed cluster's snoozes go with it (its snapshot at the next check).
                    alertSnoozes.retain(it.summary.contexts.map { c -> c.fingerprint })
                    kubeServers.sync(it.summary)
                    kubeAccess.sync(it.summary)
                    // Omni sign-ins are kept under their own key, not a fingerprint.
                    launch(Dispatchers.IO) { kubeAuthStore.retain(it.summary.contexts.map { c -> c.fingerprint } + signInKeys(it.summary.contexts)) }
                    kubeScopes.sync(it.summary)
                    snapshotKeys.sync(it.summary)
                    skippedTalosUpdates.sync(it.summary)
                    netToolTargets.sync(it.summary)
                    val fingerprints = it.summary.contexts.map { c -> c.fingerprint }
                    launch(Dispatchers.IO) { metricsStore.sync(fingerprints) }
                    launch(Dispatchers.IO) { alertmanagerStore.sync(fingerprints) }
                    launch(Dispatchers.IO) { historyStore.sync(fingerprints) }
                    lastLooked.sync(it.summary)
                }
                // The deleted config takes the metrics and Alertmanager setups (and their credentials) with it, and the sign-ins.
                if (stored == null && configRepository.generation.value > 0) {
                    launch(Dispatchers.IO) { metricsStore.sync(emptyList()) }
                    launch(Dispatchers.IO) { alertmanagerStore.sync(emptyList()) }
                    launch(Dispatchers.IO) { historyStore.sync(emptyList()) }
                    launch(Dispatchers.IO) {
                        kubeAuthStore.clear()
                        // Drops the tokens the core keeps in memory.
                        Ichorgo.setAuthStore(kubeAuthStore)
                    }
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
            override fun onStart(owner: LifecycleOwner) {
                appLock.onForeground()
                renewSignInsOnForeground(owner)
            }
            override fun onStop(owner: LifecycleOwner) {
                appLock.onBackground()
                // A model takes gigabytes: freed in the background, once an answer being written is done.
                owner.lifecycleScope.launch(Dispatchers.IO) { localModelHost.unload() }
            }
        })
        StartupTrace.markOnce("app.onCreate end")
    }

    /** When the sign-ins were last renewed on coming to the front, elapsed-realtime millis. */
    private val lastSignInRenewal = java.util.concurrent.atomic.AtomicLong(-SIGN_IN_RENEW_INTERVAL_MS)

    /**
     * Renews every sign-in as the app comes to the front, at most every
     * [SIGN_IN_RENEW_INTERVAL_MS]: the first screen finds a fresh token instead of paying the
     * refresh, and a session the identity provider ended shows its banner at once.
     */
    private fun renewSignInsOnForeground(owner: LifecycleOwner) {
        if (configRepository.config.value == null) return
        val now = android.os.SystemClock.elapsedRealtime()
        val last = lastSignInRenewal.get()
        if (now - last < SIGN_IN_RENEW_INTERVAL_MS || !lastSignInRenewal.compareAndSet(last, now)) return
        owner.lifecycleScope.launch(Dispatchers.IO) {
            val renewals = runCatching { kubeAuthRepository.renewAll() }.getOrNull().orEmpty()
            // What was loaded with the ended session goes: the screens reload and show the banner.
            if (renewals.any { it.needsUser }) talosRepository.invalidate()
        }
    }

    override fun onTrimMemory(level: Int) {
        super.onTrimMemory(level)
        // In the background and the system wants memory back: a loaded model goes at once.
        if (level >= android.content.ComponentCallbacks2.TRIM_MEMORY_BACKGROUND) {
            ProcessLifecycleOwner.get().lifecycleScope.launch(Dispatchers.IO) { localModelHost.release() }
        }
    }

    /**
     * Keeps a launcher shortcut per cluster, labelled and colored like in the app. Until the
     * config is loaded (generation 0, e.g. before the first unlock) the previous ones stay;
     * once every cluster is removed, so are they. Home screen shortcuts to a screen stay while
     * their cluster is on the phone.
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
                stored != null -> Triple(
                    clusterShortcuts(stored.summary, ClusterLabels(names, mask.enabled), colors, ClusterShortcuts.max(this)),
                    clusterShortcutIds(stored.summary),
                    stored.summary.contexts.map { it.clusterId }.filter { it.isNotBlank() }.toSet(),
                )
                generation > 0 -> Triple(emptyList<ShortcutSpec>(), emptySet<String>(), emptySet<String>())
                else -> null
            }
        }
        ProcessLifecycleOwner.get().lifecycleScope.launch(Dispatchers.Default) {
            sources.filterNotNull().distinctUntilChanged().collect { (specs, known, clusters) ->
                ClusterShortcuts.publish(this@TalosApp, specs, known, clusters)
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

/** How often coming to the front renews the sign-ins: 5 minutes. */
private const val SIGN_IN_RENEW_INTERVAL_MS = 5 * 60 * 1000L
