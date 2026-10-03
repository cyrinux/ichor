package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.assignClusterColors

/**
 * The main color of each cluster, by context fingerprint (not its name, which screenshot
 * mode masks). The app's palette is generated from the color of the cluster on screen, so
 * one always sees which cluster that is.
 */
class ClusterColors(private val prefs: SharedPreferences) {
    private val _colors = MutableStateFlow(
        prefs.all.mapNotNull { (fingerprint, color) -> (color as? Int)?.let { fingerprint to it } }.toMap(),
    )
    val colors: StateFlow<Map<String, Int>> = _colors.asStateFlow()

    /** Gives the clusters of [summary] that have no color yet a free one, and forgets the removed ones. */
    fun sync(summary: ConfigSummary) = store(assignClusterColors(_colors.value, summary.contexts.map { it.fingerprint }))

    fun set(fingerprint: String, color: Int) {
        if (fingerprint.isNotBlank()) store(_colors.value + (fingerprint to color))
    }

    private fun store(colors: Map<String, Int>) {
        if (colors == _colors.value) return
        prefs.edit().clear().also { editor -> colors.forEach { (fingerprint, color) -> editor.putInt(fingerprint, color) } }.apply()
        _colors.value = colors
    }

    companion object {
        const val FILE = "ichor-cluster-colors"
    }
}
