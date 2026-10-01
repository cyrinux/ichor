package name.levis.talosmobile.ui.capture

import android.content.Context
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import name.levis.talosmobile.R
import name.levis.talosmobile.ui.UiText
import name.levis.talosmobile.ui.components.rememberSaveFile
import name.levis.talosmobile.ui.components.shareFile
import java.io.File

/** MIME type of pcap files (as registered with IANA). */
const val PCAP_MIME = "application/vnd.tcpdump.pcap"

/** Opens the share sheet for the capture [file]. */
fun shareCapture(context: Context, file: File) = shareFile(context, file, PCAP_MIME, R.string.capture_share_chooser)

/** "Save to…" for a capture; see [rememberSaveFile]. */
@Composable
fun rememberSaveCapture(onResult: (UiText) -> Unit): (File) -> Unit =
    rememberSaveFile(PCAP_MIME, R.string.capture_saved, R.string.capture_save_failed, onResult)

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
