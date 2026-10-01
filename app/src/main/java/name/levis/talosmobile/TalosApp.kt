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
import name.levis.talosmobile.data.UiPreferences
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
    val uiPreferences by lazy { UiPreferences(getSharedPreferences("talos-viewer-ui", Context.MODE_PRIVATE)) }
    val appLock by lazy {
        AppLock(
            PrefsLockSettings(getSharedPreferences("talos-viewer-security", Context.MODE_PRIVATE)),
            clock = SystemClock::elapsedRealtime,
        )
    }

    val supportPrompt by lazy { SupportPrompt(getSharedPreferences("talos-viewer-support", Context.MODE_PRIVATE)) }
    val updateManager by lazy { UpdateManager(this, getSharedPreferences("talos-viewer-update", Context.MODE_PRIVATE)) }
    val monitorStore by lazy { MonitorStore(getSharedPreferences("talos-viewer-monitor", Context.MODE_PRIVATE)) }

    /** Re-evaluates whether background monitoring should run (alerts on or widget placed). */
    fun launchSync(runNow: Boolean = false) {
        ProcessLifecycleOwner.get().lifecycleScope.launch { syncMonitoring(this@TalosApp, runNow) }
    }

    override fun onCreate() {
        super.onCreate()
        launchSync()
        // Process-wide foreground/background, so moving between our own screens never relocks.
        ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) = appLock.onForeground()
            override fun onStop(owner: LifecycleOwner) = appLock.onBackground()
        })
    }
}
