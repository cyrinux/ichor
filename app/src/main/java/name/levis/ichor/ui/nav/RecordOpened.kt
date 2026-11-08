package name.levis.ichor.ui.nav

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ShareTarget

/**
 * Records [target], on the cluster on screen, among the search's recently opened objects once
 * its screen shows: home rows, pins, links and search results all lead here. Nothing for a
 * null target (a screen that names no object).
 */
@Composable
internal fun RecordOpened(app: TalosApp, target: ShareTarget?, label: String, detail: String = "") {
    LaunchedEffect(target, label) {
        val cluster = app.configRepository.config.value?.activeSummary?.clusterId.orEmpty()
        if (target != null) app.uiPreferences.addRecentTarget(target.copy(cluster = cluster), label, detail)
    }
}
