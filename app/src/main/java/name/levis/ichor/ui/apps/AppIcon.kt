package name.levis.ichor.ui.apps

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.TalosApp
import name.levis.ichor.model.CustomIcon
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.customIcon
import name.levis.ichor.model.monogram
import name.levis.ichor.model.monogramHue
import name.levis.ichor.model.remoteIconSlug

/** What a tile draws: nothing yet (loading), the icon, or the monogram when there is none. */
private sealed interface IconImage {
    data object Pending : IconImage
    data class Ready(val bitmap: ImageBitmap) : IconImage
    data object None : IconImage
}

/**
 * The app's icon in a rounded tile on a subtle surface, so logos with transparency read on
 * both themes; a monogram in the app's own colour when there is no icon, or [fallback] when given.
 */
@Composable
fun AppIconTile(
    app: InventoryApp,
    modifier: Modifier = Modifier,
    size: Dp = 52.dp,
    fallback: (@Composable () -> Unit)? = null,
) {
    val shape = RoundedCornerShape(size * CORNER_RATIO)
    val image = rememberIcon(app)
    Box(
        modifier.size(size).clip(shape).background(MaterialTheme.colorScheme.surfaceContainerHigh),
        contentAlignment = Alignment.Center,
    ) {
        when (image) {
            is IconImage.Ready -> Image(
                image.bitmap,
                contentDescription = null,
                contentScale = ContentScale.Fit,
                modifier = Modifier.fillMaxSize().padding(size * PADDING_RATIO),
            )
            IconImage.None -> fallback?.invoke() ?: Monogram(app, size)
            IconImage.Pending -> Unit
        }
    }
}

@Composable
private fun rememberIcon(app: InventoryApp): IconImage {
    val talos = LocalContext.current.applicationContext as TalosApp
    val loader = talos.appIcons
    val dark = MaterialTheme.colorScheme.surface.luminance() < 0.5f
    val remoteAllowed by talos.uiPreferences.remoteAppIcons.collectAsStateWithLifecycle()
    val slug = app.remoteIconSlug?.takeIf { remoteAllowed }
    // Its own icon: an inline one always, one at a URL only when downloads are allowed.
    val custom = remember(app.iconUrl) { customIcon(app.iconUrl) }?.takeIf { it is CustomIcon.Inline || remoteAllowed }
    val initial = when {
        custom != null -> loader.peekCustom(custom)?.let(IconImage::Ready) ?: IconImage.Pending
        app.icon.isNotEmpty() -> loader.peekBundled(app.icon, dark)?.let(IconImage::Ready) ?: IconImage.Pending
        slug != null -> loader.peekRemote(slug)?.let(IconImage::Ready) ?: IconImage.Pending
        else -> IconImage.None
    }
    val image by produceState(initial, custom?.key, app.icon, slug, dark) {
        // The state outlives a key change (e.g. the theme): start again from this key's icon.
        value = initial
        if (initial !is IconImage.Pending) return@produceState
        val bitmap = when {
            custom != null -> loader.custom(custom)
            app.icon.isNotEmpty() -> loader.bundled(app.icon, dark)
            slug != null -> loader.remote(slug)
            else -> null
        }
        value = bitmap?.let(IconImage::Ready) ?: IconImage.None
    }
    return image
}

/** Up to two initials on a colour derived from the app's id, readable in both themes. */
@Composable
private fun Monogram(app: InventoryApp, size: Dp) {
    val dark = MaterialTheme.colorScheme.surface.luminance() < 0.5f
    val hue = monogramHue(app.id)
    val background = if (dark) Color.hsl(hue, 0.42f, 0.30f) else Color.hsl(hue, 0.55f, 0.86f)
    val foreground = if (dark) Color.hsl(hue, 0.70f, 0.86f) else Color.hsl(hue, 0.60f, 0.26f)
    val fontSize = with(LocalDensity.current) { (size * TEXT_RATIO).toSp() }
    Box(Modifier.fillMaxSize().background(background), contentAlignment = Alignment.Center) {
        Text(monogram(app.name), color = foreground, fontSize = fontSize, fontWeight = FontWeight.SemiBold, maxLines = 1)
    }
}

/** A tile-sized placeholder while the inventory loads. */
@Composable
fun AppIconPlaceholder(modifier: Modifier = Modifier, size: Dp = 52.dp) {
    Box(
        modifier.size(size).clip(RoundedCornerShape(size * CORNER_RATIO))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh),
    )
}

/** 16dp on a 52dp tile. */
private const val CORNER_RATIO = 0.3f
private const val PADDING_RATIO = 0.16f
private const val TEXT_RATIO = 0.36f
