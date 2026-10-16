package name.levis.ichor.shortcuts

import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Typeface
import androidx.core.content.pm.ShortcutInfoCompat
import androidx.core.content.pm.ShortcutManagerCompat
import androidx.core.graphics.ColorUtils
import androidx.core.graphics.createBitmap
import androidx.core.graphics.drawable.IconCompat
import name.levis.ichor.MainActivity
import name.levis.ichor.R

/**
 * Launcher shortcuts (long press on the app icon): one per cluster, opening the app on it.
 * Each one shows the cluster's initial in its color, so they are told apart like in the app.
 */
object ClusterShortcuts {
    /** Adaptive icon: 108dp, the inner 72dp always visible whatever the launcher's mask. */
    private const val ICON_DP = 108f
    private const val LETTER_DP = 40f
    private const val LIGHT_BACKGROUND = 0.5
    /** res/xml/shortcuts.xml: "Search clusters", which counts toward the launcher's limit. */
    private const val STATIC_SHORTCUTS = 1

    /** How many cluster shortcuts the launcher shows: its limit less the static "Search clusters" one. */
    fun max(context: Context): Int =
        runCatching { ShortcutManagerCompat.getMaxShortcutCountPerActivity(context) - STATIC_SHORTCUTS }
            .getOrDefault(0)
            .coerceAtLeast(0)

    /**
     * Makes [specs] the app's shortcuts. Pinned shortcuts (on the home screen) whose id is
     * not among [known], those of removed clusters, are disabled (the system keeps them,
     * greyed out); [known] also covers the clusters past what the launcher shows. Screen shortcuts
     * (see [ScreenShortcuts]) are kept while their cluster is among [clusters]. Best effort: the
     * system rate-limits apps in the background, and the next change publishes again.
     */
    fun publish(context: Context, specs: List<ShortcutSpec>, known: Set<String>, clusters: Set<String>) {
        runCatching {
            val shortcuts = specs.map { build(context, it) }
            val ids = specs.map { it.id }.toSet()
            val pinned = ShortcutManagerCompat.getShortcuts(context, ShortcutManagerCompat.FLAG_MATCH_PINNED)
            val kept = pinned.filter { keepsPinned(PinnedShortcut(it.id, linkOf(it)), ids, known, clusters) }
            // A cluster imported again after its removal: its pinned shortcuts work again.
            val revived = kept.filter { !it.isEnabled }
            val revivedClusters = revived.filter { it.id in ids }.map { it.id }.toSet()
            if (revived.isNotEmpty()) {
                ShortcutManagerCompat.enableShortcuts(
                    context,
                    shortcuts.filter { it.id in revivedClusters } + revived.filter { isScreenShortcut(it.id) },
                )
            }
            ShortcutManagerCompat.setDynamicShortcuts(context, shortcuts)
            val gone = pinned.filter { it !in kept }.map { it.id }
            if (gone.isNotEmpty()) {
                ShortcutManagerCompat.disableShortcuts(context, gone, context.getString(R.string.shortcut_cluster_removed))
            }
        }
    }

    /** The link a pinned shortcut opens; null when the system did not return its intent. */
    private fun linkOf(shortcut: ShortcutInfoCompat): String? = runCatching { shortcut.intent.dataString }.getOrNull()

    private fun build(context: Context, spec: ShortcutSpec): ShortcutInfoCompat =
        ShortcutInfoCompat.Builder(context, spec.id)
            .setShortLabel(spec.label)
            .setLongLabel(spec.label)
            .setRank(spec.rank)
            .setIcon(IconCompat.createWithAdaptiveBitmap(icon(context, spec)))
            .setIntent(
                Intent(context, MainActivity::class.java)
                    .setAction(Intent.ACTION_VIEW)
                    .putExtra(MainActivity.EXTRA_CLUSTER, spec.fingerprint),
            )
            .build()

    private fun icon(context: Context, spec: ShortcutSpec): Bitmap = glyphIcon(context, spec.color, shortcutInitial(spec.label))

    /** An adaptive icon bitmap: [glyph] in black or white on [background], whichever reads better. */
    fun glyphIcon(context: Context, background: Int, glyph: String): Bitmap {
        val density = context.resources.displayMetrics.density
        val size = (ICON_DP * density).toInt()
        val bitmap = createBitmap(size, size)
        val canvas = Canvas(bitmap)
        canvas.drawColor(background)
        val light = ColorUtils.calculateLuminance(background) > LIGHT_BACKGROUND
        val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = if (light) android.graphics.Color.BLACK else android.graphics.Color.WHITE
            textSize = LETTER_DP * density
            textAlign = Paint.Align.CENTER
            typeface = Typeface.DEFAULT_BOLD
        }
        // Vertically centered on the glyph box, not the baseline.
        val y = size / 2f - (paint.descent() + paint.ascent()) / 2f
        canvas.drawText(glyph, size / 2f, y, paint)
        return bitmap
    }
}
