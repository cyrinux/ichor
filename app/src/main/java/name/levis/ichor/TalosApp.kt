package name.levis.ichor

import name.levis.talosmobile.Talosmobile
import android.app.Application
import android.content.Context
import android.os.SystemClock
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.lifecycleScope
import name.levis.ichor.data.AiPreferences
import name.levis.ichor.data.CaptureRepository
import name.levis.ichor.data.ChangelogRepository
import name.levis.ichor.data.ClusterColors
import name.levis.ichor.data.DiagnosisRepository
import name.levis.ichor.data.SecureStore
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.TalosUpdateChecker
import name.levis.ichor.data.UpgradeManager
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.SupportBundleRepository
import name.levis.ichor.data.SupportPrompt
import name.levis.ichor.data.PrivacyMask
import name.levis.ichor.data.UiPreferences
import androidx.glance.appwidget.updateAll
import name.levis.ichor.widget.ClusterWidget
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.monitor.MonitorStore
import name.levis.ichor.monitor.syncMonitoring
import name.levis.ichor.security.AppLock
import name.levis.ichor.security.PrefsLockSettings
import name.levis.ichor.update.UpdateManager
import name.levis.ichor.data.ClusterNames
import name.levis.ichor.data.WakeOnLanStore
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.shortcuts.ClusterShortcuts
import name.levis.ichor.shortcuts.ShortcutSpec
import name.levis.ichor.shortcuts.clusterShortcutIds
import name.levis.ichor.shortcuts.clusterShortcuts
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.launch

/** Holds app-wide singletons (manual DI; the app is small). */
class TalosApp : Application() {
    val configRepository by lazy { ConfigRepository(this) }
    val talosRepository by lazy { TalosRepository(configRepository) }
    val captureRepository by lazy { CaptureRepository(configRepository, filesDir) }
    val supportBundleRepository by lazy { SupportBundleRepository(configRepository, filesDir) }
    val upgradeManager by lazy { UpgradeManager(configRepository, onFinished = talosRepository::forgetFeatures) }
    val talosUpdateChecker by lazy { TalosUpdateChecker() }
    val uiPreferences by lazy { UiPreferences(getSharedPreferences(UiPreferences.FILE, Context.MODE_PRIVATE)) }
    val clusterColors by lazy { ClusterColors(getSharedPreferences(ClusterColors.FILE, Context.MODE_PRIVATE)) }
    val clusterNames by lazy { ClusterNames(getSharedPreferences(ClusterNames.FILE, Context.MODE_PRIVATE)) }
    val wakeOnLan by lazy {
        WakeOnLanStore(
            getSharedPreferences(WakeOnLanStore.FILE, Context.MODE_PRIVATE),
            getSharedPreferences(WakeOnLanStore.SEEN_FILE, Context.MODE_PRIVATE),
        )
    }
    val appLock by lazy {
        AppLock(
            PrefsLockSettings(getSharedPreferences("talosdev-mobile-security", Context.MODE_PRIVATE)),
            clock = SystemClock::elapsedRealtime,
        )
    }

    val supportPrompt by lazy { SupportPrompt(getSharedPreferences("talosdev-mobile-support", Context.MODE_PRIVATE)) }
    val changelogRepository by lazy { ChangelogRepository(this, getSharedPreferences(ChangelogRepository.PREFS, Context.MODE_PRIVATE)) }
    val updateManager by lazy {
        UpdateManager(this, getSharedPreferences("talosdev-mobile-update", Context.MODE_PRIVATE), changelogRepository)
    }
    val monitorStore by lazy { MonitorStore(getSharedPreferences("talosdev-mobile-monitor", Context.MODE_PRIVATE)) }

    /** The optional AI diagnosis: off until enabled in Settings. API keys get their own Keystore keys. */
    val diagnosisRepository by lazy { DiagnosisRepository(configRepository) }
    val aiPreferences by lazy {
        AiPreferences(
            getSharedPreferences(AiPreferences.FILE, Context.MODE_PRIVATE),
            diagnosisRepository.providers.map { it.id },
        ) { provider -> SecureStore(java.io.File(filesDir, "ai-key-$provider.enc"), keyAlias = "ai-key-$provider") }
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

    /** Removes a cluster from the stored config; false when it was the last one (nothing is stored anymore). */
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
            talosRepository.invalidate()
            ClusterWidget().updateAll(this@TalosApp)
            syncMonitoring(this@TalosApp, runNow = true)
        }
    }

    private fun applyPrivacyMask(mask: PrivacyMask) = Talosmobile.setPrivacyMask(mask.enabled, mask.words)

    override fun onCreate() {
        super.onCreate()
        migrateLegacyPreferences()
        syncLanguage()
        // Before any Talos call: the monitor worker and the widget run in this process too.
        applyPrivacyMask(uiPreferences.privacyMask.value)
        launchSync()
        // Every cluster of the stored config gets a color of its own, as soon as it shows up.
        ProcessLifecycleOwner.get().lifecycleScope.launch {
            configRepository.config.collect { stored ->
                stored?.let {
                    clusterColors.sync(it.summary)
                    clusterNames.sync(it.summary)
                    wakeOnLan.sync(it.summary)
                }
            }
        }
        publishClusterShortcuts()
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

    /**
     * v0.3.x stored settings under "talos-viewer*" names (before the Talosdev Mobile rebrand).
     * Rename the files once, before any SharedPreferences is opened, so nothing is lost.
     */
    private fun migrateLegacyPreferences() {
        val dir = java.io.File(applicationInfo.dataDir, "shared_prefs")
        dir.listFiles { f -> f.name.startsWith("talos-viewer") && f.name.endsWith(".xml") }?.forEach { old ->
            val renamed = java.io.File(dir, old.name.replaceFirst("talos-viewer", "talosdev-mobile"))
            if (!renamed.exists()) old.renameTo(renamed)
        }
    }
}
