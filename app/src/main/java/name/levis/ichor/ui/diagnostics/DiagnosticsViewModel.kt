package name.levis.ichor.ui.diagnostics

import android.app.Application
import androidx.lifecycle.viewModelScope
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.data.Diagnostics
import name.levis.ichor.model.DiagnosticsView
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.ExitRecords

/** The newest log entries the screen shows (the zip holds them all). */
internal const val DIAGNOSTICS_SHOWN = 200

/** Where the zip is written: a cache subdirectory the FileProvider shares (res/xml/capture_paths.xml). */
internal const val DIAGNOSTICS_DIR = "diagnostics"

/** The last export: the zip ready to share or save, or why it could not be made. */
sealed interface DiagExport {
    data object Running : DiagExport
    data class Ready(val file: File, val bytes: Long, val entries: Int) : DiagExport
    data class Failed(val message: UiText) : DiagExport
}

/** The OS's exit records and the core's diagnostics log, on this device; the zip made from them. */
class DiagnosticsViewModel(private val context: Application) : LoadingViewModel<DiagnosticsView>() {
    private val _export = MutableStateFlow<DiagExport?>(null)
    val export: StateFlow<DiagExport?> = _export.asStateFlow()

    override suspend fun fetch(): DiagnosticsView {
        val exits = withContext(Dispatchers.IO) { ExitRecords.read(context) }
        return DiagnosticsView(exits, Diagnostics.entries(DIAGNOSTICS_SHOWN))
    }

    /** Writes cacheDir/diagnostics/ichor-diagnostics-<date>.zip, the only zip kept there. */
    fun exportZip() {
        if (_export.value == DiagExport.Running) return
        _export.value = DiagExport.Running
        viewModelScope.launch {
            _export.value = runCatching {
                val summary = withContext(Dispatchers.IO) {
                    val dir = File(context.cacheDir, DIAGNOSTICS_DIR)
                    dir.mkdirs()
                    dir.listFiles()?.forEach { it.delete() }
                    val dest = File(dir, Diagnostics.zipName(System.currentTimeMillis()))
                    Diagnostics.export(dest, Diagnostics.appFacts(context), ExitRecords.read(context))
                }
                DiagExport.Ready(File(summary.path), summary.bytes, summary.entries)
            }.getOrElse { DiagExport.Failed(it.uiText()) }
        }
    }

    fun dismissExport() {
        _export.value = null
    }

    fun clear() {
        viewModelScope.launch {
            runCatching { Diagnostics.clear() }
            refresh()
        }
    }
}
