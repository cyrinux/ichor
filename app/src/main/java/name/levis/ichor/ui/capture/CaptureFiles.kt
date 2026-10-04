package name.levis.ichor.ui.capture

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.rememberSaveFile
import name.levis.ichor.ui.components.shareFile
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
    ConfirmDialog(
        title = stringResource(R.string.capture_delete_title),
        text = stringResource(R.string.capture_delete_body, file.name),
        confirm = stringResource(R.string.common_delete),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
    )
}
