package name.levis.ichor.ui.tour

import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.UiPreferences
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.isKube

/**
 * Settings → "Replay the demo tour": the Talos demo becomes the cluster shown (added like
 * "Try demo" when it was removed) and its tour is due again; the caller then opens the home.
 */
suspend fun replayDemoTour(configs: ConfigRepository, prefs: UiPreferences, select: (String) -> Unit) {
    val demo = configs.config.value?.summary?.contexts?.firstOrNull { it.isDemo && !it.isKube }
    if (demo != null) select(demo.name) else configs.saveDemo()
    prefs.setTourPending(true)
}
