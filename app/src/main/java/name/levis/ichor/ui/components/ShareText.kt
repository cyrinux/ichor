package name.levis.ichor.ui.components

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent

/** Opens the share sheet with [text], titled [chooserTitle]. */
fun shareText(context: Context, text: String, chooserTitle: String) {
    val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, text)
    try {
        context.startActivity(Intent.createChooser(send, chooserTitle))
    } catch (_: ActivityNotFoundException) {
        // No app takes text: the text can still be selected and copied where it is shown.
    } catch (_: RuntimeException) {
        // A text too large for an intent (TransactionTooLargeException): same fallback.
    }
}
