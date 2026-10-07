package name.levis.ichor.ui.components

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.clusterLogo

/**
 * A cluster's logo (Talos, Kubernetes or its cloud, see [clusterLogo]) in a ring of the
 * cluster's [color]; [selected] adds a check badge in that color.
 */
@Composable
fun ClusterLogo(context: ContextSummary, color: Color, selected: Boolean, size: Dp = 32.dp) {
    val loader = (LocalContext.current.applicationContext as TalosApp).appIcons
    val dark = MaterialTheme.colorScheme.surface.luminance() < 0.5f
    val icon = clusterLogo(context)
    val logo by produceState(loader.peekBundled(icon, dark), icon, dark) { value = loader.bundled(icon, dark) }
    Box(Modifier.size(size)) {
        Box(
            Modifier.fillMaxSize()
                .border(2.dp, color, CircleShape)
                .padding(size * LOGO_INSET),
            contentAlignment = Alignment.Center,
        ) {
            logo?.let { Image(it, contentDescription = null, contentScale = ContentScale.Fit, modifier = Modifier.fillMaxSize()) }
        }
        if (selected) {
            Box(
                Modifier.align(Alignment.BottomEnd)
                    .size(size * BADGE_RATIO)
                    .background(color, CircleShape)
                    .border(1.5.dp, MaterialTheme.colorScheme.surface, CircleShape),
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.Outlined.Check, contentDescription = null, tint = Color.White, modifier = Modifier.size(size * BADGE_RATIO * 0.7f))
            }
        }
    }
}

private const val LOGO_INSET = 0.2f
private const val BADGE_RATIO = 0.45f
