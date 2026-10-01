package dev.talos.viewer

import android.app.Application
import android.content.Context
import android.os.SystemClock
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.lifecycleScope
import dev.talos.viewer.data.ConfigRepository
import dev.talos.viewer.data.TalosRepository
import dev.talos.viewer.data.UiPreferences
import dev.talos.viewer.monitor.MonitorStore
import dev.talos.viewer.monitor.syncMonitoring
import dev.talos.viewer.security.AppLock
import dev.talos.viewer.security.PrefsLockSettings
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
