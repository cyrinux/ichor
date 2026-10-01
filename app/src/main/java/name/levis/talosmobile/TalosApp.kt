package name.levis.talosmobile

import android.app.Application
import android.content.Context
import android.os.SystemClock
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.lifecycleScope
import name.levis.talosmobile.data.ConfigRepository
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.data.SupportPrompt
import name.levis.talosmobile.data.PrivacyMask
import name.levis.talosmobile.data.UiPreferences
import androidx.glance.appwidget.updateAll
import name.levis.talosmobile.widget.ClusterWidget
import name.levis.talosmobile.i18n.AppLocale
import name.levis.talosmobile.monitor.MonitorStore
import name.levis.talosmobile.monitor.syncMonitoring
import name.levis.talosmobile.security.AppLock
import name.levis.talosmobile.security.PrefsLockSettings
import name.levis.talosmobile.update.UpdateManager
import kotlinx.coroutines.launch

/** Holds app-wide singletons (manual DI; the app is small). */
class TalosApp : Application() {
    val configRepository by lazy { ConfigRepository(this) }
    val talosRepository by lazy { TalosRepository(configRepository) }
    val uiPreferences by lazy { UiPreferences(getSharedPreferences(UiPreferences.FILE, Context.MODE_PRIVATE)) }
    val appLock by lazy {
        AppLock(
            PrefsLockSettings(getSharedPreferences("talosdev-mobile-security", Context.MODE_PRIVATE)),
            clock = SystemClock::elapsedRealtime,
        )
    }

    val supportPrompt by lazy { SupportPrompt(getSharedPreferences("talosdev-mobile-support", Context.MODE_PRIVATE)) }
    val updateManager by lazy { UpdateManager(this, getSharedPreferences("talosdev-mobile-update", Context.MODE_PRIVATE)) }
    val monitorStore by lazy { MonitorStore(getSharedPreferences("talosdev-mobile-monitor", Context.MODE_PRIVATE)) }

    /** Re-evaluates whether background monitoring should run (alerts on or widget placed). */
    fun launchSync(runNow: Boolean = false) {
        ProcessLifecycleOwner.get().lifecycleScope.launch { syncMonitoring(this@TalosApp, runNow) }
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
        // Process-wide foreground/background, so moving between our own screens never relocks.
        ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) = appLock.onForeground()
            override fun onStop(owner: LifecycleOwner) = appLock.onBackground()
        })
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
