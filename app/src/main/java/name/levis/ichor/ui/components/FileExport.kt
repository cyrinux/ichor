package name.levis.ichor.ui.components

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.FileProvider
import java.io.OutputStream
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText
import java.io.File

/**
 * Opens the share sheet for [file] through the app's FileProvider (read-only grant). The
 * file must be under a directory listed in res/xml/capture_paths.xml.
 */
fun shareFile(context: Context, file: File, mime: String, @StringRes chooserTitle: Int) {
    val uri = FileProvider.getUriForFile(context, "${context.packageName}.files", file)
    val send = Intent(Intent.ACTION_SEND)
        .setType(mime)
        .putExtra(Intent.EXTRA_STREAM, uri)
        .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    try {
        context.startActivity(Intent.createChooser(send, context.getString(chooserTitle)))
    } catch (_: ActivityNotFoundException) {
        // No app can receive it: nothing to do, "Save to…" remains.
    }
}

/**
 * "Save to…": the system file picker, then a copy of the file into the picked document.
 * [onResult] gets the message to show ([saved], or [failed] with the reason). Must be
 * called at screen level so the picker result is never lost.
 */
@Composable
fun rememberSaveFile(mime: String, @StringRes saved: Int, @StringRes failed: Int, onResult: (UiText) -> Unit): (File) -> Unit {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    // The source survives activity recreation while the picker is open.
    var pending by rememberSaveable { mutableStateOf<String?>(null) }
    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument(mime)) { uri ->
        val source = pending?.let(::File)
        pending = null
        if (uri == null || source == null) return@rememberLauncherForActivityResult
        scope.launch {
            val result = runCatching { withContext(Dispatchers.IO) { copyToDocument(context, source, uri) } }
            onResult(result.fold(onSuccess = { UiText.Res(saved) }, onFailure = { UiText.Res(failed, it.uiText()) }))
        }
    }
    return { file ->
        pending = file.path
        saver.launch(file.name)
    }
}

private fun copyToDocument(context: Context, source: File, uri: Uri) {
    openTruncating(context, uri, R.string.capture_open_failed).use { stream -> source.inputStream().use { it.copyTo(stream) } }
}

/**
 * Opens a document picked with the system file picker for writing, truncated ("wt") when the
 * user picked an existing file; throws [failed] when it cannot be opened.
 */
fun openTruncating(context: Context, uri: Uri, @StringRes failed: Int): OutputStream =
    context.contentResolver.openOutputStream(uri, "wt") ?: throw LocalizedException(UiText.Res(failed))

/** Writes [text] (UTF-8) to a picked document, see [openTruncating]. Blocking. */
fun writeText(context: Context, uri: Uri, text: String, @StringRes failed: Int) {
    openTruncating(context, uri, failed).use { it.write(text.encodeToByteArray()) }
}
