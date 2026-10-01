package name.levis.talosmobile.ui.capture

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.core.content.FileProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.talosmobile.R
import name.levis.talosmobile.ui.LocalizedException
import name.levis.talosmobile.ui.UiText
import name.levis.talosmobile.ui.uiText
import java.io.File

/** MIME type of pcap files (as registered with IANA). */
const val PCAP_MIME = "application/vnd.tcpdump.pcap"

/** Opens the share sheet for [file] through the app's FileProvider (read-only grant). */
fun shareCapture(context: Context, file: File) {
    val uri = FileProvider.getUriForFile(context, "${context.packageName}.files", file)
    val send = Intent(Intent.ACTION_SEND)
        .setType(PCAP_MIME)
        .putExtra(Intent.EXTRA_STREAM, uri)
        .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    try {
        context.startActivity(Intent.createChooser(send, context.getString(R.string.capture_share_chooser)))
    } catch (_: ActivityNotFoundException) {
        // No app can receive it: nothing to do, "Save to…" remains.
    }
}

/**
 * "Save to…": the system file picker, then a copy of the capture into the picked document.
 * [onResult] gets the message to show. Must be called at screen level so the picker result
 * is never lost.
 */
@Composable
fun rememberSaveCapture(onResult: (UiText) -> Unit): (File) -> Unit {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    // The source survives activity recreation while the picker is open.
    var pending by rememberSaveable { mutableStateOf<String?>(null) }
    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument(PCAP_MIME)) { uri ->
        val source = pending?.let(::File)
        pending = null
        if (uri == null || source == null) return@rememberLauncherForActivityResult
        scope.launch {
            val result = runCatching { withContext(Dispatchers.IO) { copyToDocument(context, source, uri) } }
            onResult(
                result.fold(
                    onSuccess = { UiText.Res(R.string.capture_saved) },
                    onFailure = { UiText.Res(R.string.capture_save_failed, it.uiText()) },
                ),
            )
        }
    }
    return { file ->
        pending = file.path
        saver.launch(file.name)
    }
}

private fun copyToDocument(context: Context, source: File, uri: Uri) {
    // "wt" truncates when the user picked an existing file.
    val out = context.contentResolver.openOutputStream(uri, "wt")
        ?: throw LocalizedException(UiText.Res(R.string.capture_open_failed))
    out.use { stream -> source.inputStream().use { it.copyTo(stream) } }
}

/** "Delete this capture?" for [file]. */
@Composable
fun DeleteCaptureDialog(file: File, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.capture_delete_title)) },
        text = { Text(stringResource(R.string.capture_delete_body, file.name)) },
        confirmButton = { TextButton(onClick = onConfirm) { Text(stringResource(R.string.common_delete)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
