package name.levis.ichor.ui.components

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.PersistableBundle

/**
 * Puts [text] on the clipboard under [label]. [sensitive] keeps secrets out of the clipboard
 * preview (honoured from Android 13).
 */
fun copyToClipboard(context: Context, label: String, text: String, sensitive: Boolean = false) {
    val clip = ClipData.newPlainText(label, text)
    if (sensitive) {
        clip.description.extras = PersistableBundle().apply { putBoolean("android.content.extra.IS_SENSITIVE", true) }
    }
    context.getSystemService(ClipboardManager::class.java).setPrimaryClip(clip)
}
