package name.levis.ichor.ui.share

import android.widget.Toast
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AddToHomeScreen
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.seedOf
import name.levis.ichor.shortcuts.ScreenShortcuts
import name.levis.ichor.shortcuts.screenGlyph
import name.levis.ichor.ui.components.TooltipIconButton

/**
 * Pins a home screen shortcut to a screen of the cluster on screen (the target, shown as the
 * label, blank for the cluster's own screen), in the cluster's color and named after it; a
 * toast when the launcher refuses. Null
 * while no cluster is on screen (nothing to link to).
 */
@Composable
fun rememberAddToHome(): ((ShareTarget, String) -> Unit)? {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val colors by app.clusterColors.colors.collectAsStateWithLifecycle()
    val names by app.clusterNames.names.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val active = config?.activeSummary?.takeIf { it.clusterId.isNotBlank() } ?: return null
    val cluster = ClusterLabels(names, mask.enabled).of(active)
    val color = colors.seedOf(active)
    return remember(context, active, cluster, color) {
        { target, label ->
            val link = shareLinkFor(target, active.clusterId)
            // A blank label: the screen is the cluster's own, which names it.
            val title = label.ifBlank { cluster }
            val pinned = link != null && ScreenShortcuts.pin(
                context,
                link,
                title = title,
                longLabel = if (label.isBlank()) cluster else context.getString(R.string.shortcut_screen_label, label, cluster),
                glyph = screenGlyph(target.target, title),
                color = color,
            )
            if (!pinned) Toast.makeText(context, R.string.shortcut_pin_unsupported, Toast.LENGTH_LONG).show()
        }
    }
}

/** A top-bar button adding [target] (named [label]) to the home screen. */
@Composable
fun AddToHomeButton(target: ShareTarget, label: String) {
    val add = rememberAddToHome() ?: return
    TooltipIconButton(Icons.Outlined.AddToHomeScreen, stringResource(R.string.shortcut_add_home), onClick = { add(target, label) })
}

/** A menu entry adding [target] (named [label]) to the home screen; [onClick] closes the menu. */
@Composable
fun AddToHomeMenuItem(target: ShareTarget, label: String, onClick: () -> Unit = {}) {
    val add = rememberAddToHome() ?: return
    DropdownMenuItem(
        text = { Text(stringResource(R.string.shortcut_add_home)) },
        leadingIcon = { Icon(Icons.Outlined.AddToHomeScreen, null) },
        onClick = {
            onClick()
            add(target, label)
        },
    )
}
