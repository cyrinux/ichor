package name.levis.ichor.widget

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ConfigSummary

/**
 * The cluster each placed widget shows, by app widget id → context fingerprint. A widget without
 * one (placed before, or set so) follows the cluster on screen.
 */
class WidgetClusters(private val prefs: SharedPreferences) {
    private val _choices = MutableStateFlow(read())
    val choices: StateFlow<Map<Int, String>> = _choices.asStateFlow()

    /** Shows the cluster [fingerprint] on the widget [widgetId]; null: the cluster on screen. */
    fun set(widgetId: Int, fingerprint: String?) =
        store(if (fingerprint.isNullOrBlank()) _choices.value - widgetId else _choices.value + (widgetId to fingerprint))

    /** Forgets the widgets removed from the home screen. */
    fun forget(widgetIds: IntArray) = store(_choices.value - widgetIds.toSet())

    /** Widgets of a cluster no longer in [summary] follow the cluster on screen again. */
    fun sync(summary: ConfigSummary) {
        val known = summary.contexts.map { it.fingerprint }.toSet()
        store(_choices.value.filterValues { it in known })
    }

    private fun read(): Map<Int, String> =
        prefs.all.mapNotNull { (id, fingerprint) -> id.toIntOrNull()?.let { key -> (fingerprint as? String)?.let { key to it } } }.toMap()

    private fun store(choices: Map<Int, String>) {
        if (choices == _choices.value) return
        val editor = prefs.edit().clear()
        choices.forEach { (id, fingerprint) -> editor.putString(id.toString(), fingerprint) }
        editor.apply()
        _choices.value = choices
    }

    companion object {
        const val FILE = "ichor-widgets"
    }
}
