package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer

/** The values last given to each runbook of a cluster ([scope], see [pinScope]): a rerun starts from them. */
class RunbookValueMemory(private val prefs: SharedPreferences) {
    fun load(scope: String, id: String): Map<String, String> =
        prefs.getString(key(scope, id), null)
            ?.let { runCatching { TalosJson.decodeFromString(VALUES, it) }.getOrNull() }
            .orEmpty()

    fun save(scope: String, id: String, values: Map<String, String>) {
        prefs.edit().putString(key(scope, id), TalosJson.encodeToString(VALUES, values)).apply()
    }

    private fun key(scope: String, id: String) = "$scope/$id"

    companion object {
        const val FILE = "runbook_values"
        private val VALUES = MapSerializer(String.serializer(), String.serializer())
    }
}
