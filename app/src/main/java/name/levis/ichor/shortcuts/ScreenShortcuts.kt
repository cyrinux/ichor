package name.levis.ichor.shortcuts

import android.content.Context
import android.content.Intent
import androidx.core.content.pm.ShortcutInfoCompat
import androidx.core.content.pm.ShortcutManagerCompat
import androidx.core.graphics.drawable.IconCompat
import androidx.core.net.toUri
import name.levis.ichor.MainActivity

/**
 * Home screen shortcuts to one screen of a cluster ("Add to home screen"). The intent is the
 * screen's ichor://open link and nothing else: the launcher stores it in clear, so it carries
 * exactly what a share link carries, and the app opens it like one (after unlock).
 */
object ScreenShortcuts {
    /**
     * Asks the launcher to pin a shortcut opening the share link [link] (https form, see
     * [appShareLink]), titled [title] ([longLabel] where there is room), its icon [glyph] on
     * [color]. False when the launcher cannot pin shortcuts or the request fails.
     */
    fun pin(context: Context, link: String, title: String, longLabel: String, glyph: String, color: Int): Boolean {
        val appLink = appShareLink(link) ?: return false
        return runCatching {
            if (!ShortcutManagerCompat.isRequestPinShortcutSupported(context)) return false
            val shortcut = ShortcutInfoCompat.Builder(context, screenShortcutId(appLink))
                .setShortLabel(title)
                .setLongLabel(longLabel)
                .setIcon(IconCompat.createWithAdaptiveBitmap(ClusterShortcuts.glyphIcon(context, color, glyph)))
                .setIntent(Intent(Intent.ACTION_VIEW, appLink.toUri(), context, MainActivity::class.java))
                .build()
            ShortcutManagerCompat.requestPinShortcut(context, shortcut, null)
        }.getOrDefault(false)
    }
}
