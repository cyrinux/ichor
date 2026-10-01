package name.levis.talosmobile.shortcuts

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
import name.levis.talosmobile.MainActivity
import name.levis.talosmobile.R

/**
 * Launcher shortcuts (long press on the app icon): one per cluster, opening the app on it.
 * Each one shows the cluster's initial in its color, so they are told apart like in the app.
 */
object ClusterShortcuts {
    /** Adaptive icon: 108dp, the inner 72dp always visible whatever the launcher's mask. */
    private const val ICON_DP = 108f
    private const val LETTER_DP = 40f
    private const val LIGHT_BACKGROUND = 0.5

    /** How many shortcuts the launcher shows. */
    fun max(context: Context): Int =
        runCatching { ShortcutManagerCompat.getMaxShortcutCountPerActivity(context) }.getOrDefault(0)

    /**
     * Makes [specs] the app's shortcuts. Pinned shortcuts (on the home screen) whose id is
     * not among [known], those of removed clusters, are disabled (the system keeps them,
     * greyed out); [known] also covers the clusters past what the launcher shows. Best effort: the
     * system rate-limits apps in the background, and the next change publishes again.
     */
    fun publish(context: Context, specs: List<ShortcutSpec>, known: Set<String>) {
        runCatching {
            val shortcuts = specs.map { build(context, it) }
            val ids = specs.map { it.id }.toSet()
            val pinned = ShortcutManagerCompat.getShortcuts(context, ShortcutManagerCompat.FLAG_MATCH_PINNED)
            // A cluster imported again after its removal: its pinned shortcut works again.
            val revived = pinned.filter { it.id in ids && !it.isEnabled }
            if (revived.isNotEmpty()) ShortcutManagerCompat.enableShortcuts(context, shortcuts.filter { s -> revived.any { it.id == s.id } })
            ShortcutManagerCompat.setDynamicShortcuts(context, shortcuts)
            val gone = pinned.map { it.id }.filter { it !in known && it !in ids }
            if (gone.isNotEmpty()) {
                ShortcutManagerCompat.disableShortcuts(context, gone, context.getString(R.string.shortcut_cluster_removed))
            }
        }
    }

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

    private fun icon(context: Context, spec: ShortcutSpec): Bitmap {
        val density = context.resources.displayMetrics.density
        val size = (ICON_DP * density).toInt()
        val bitmap = createBitmap(size, size)
        val canvas = Canvas(bitmap)
        canvas.drawColor(spec.color)
        val light = ColorUtils.calculateLuminance(spec.color) > LIGHT_BACKGROUND
        val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = if (light) android.graphics.Color.BLACK else android.graphics.Color.WHITE
            textSize = LETTER_DP * density
            textAlign = Paint.Align.CENTER
            typeface = Typeface.DEFAULT_BOLD
        }
        // Vertically centered on the glyph box, not the baseline.
        val y = size / 2f - (paint.descent() + paint.ascent()) / 2f
        canvas.drawText(shortcutInitial(spec.label), size / 2f, y, paint)
        return bitmap
    }
}
