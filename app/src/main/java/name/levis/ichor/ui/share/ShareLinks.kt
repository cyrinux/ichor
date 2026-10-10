package name.levis.ichor.ui.share

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.TalosJson
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.monitor.AlertActionRequest
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.shareText
import name.levis.ichorgo.Ichorgo

/** A share link to open ([url], not yet checked), and the action an alert's button asks to confirm there. */
data class OpenLink(val url: String, val action: AlertActionRequest? = null)

/** The link to [target] on the cluster on screen; null when there is none to name. */
fun TalosApp.shareLinkOf(target: ShareTarget): String? =
    configRepository.config.value?.activeSummary?.clusterId?.let { shareLinkFor(target, it) }

/** The link to [target] on the cluster [clusterId]; null when it names none or the target is invalid. */
fun shareLinkFor(target: ShareTarget, clusterId: String): String? {
    if (clusterId.isBlank()) return null
    return runCatching { Ichorgo.buildShareLink(TalosJson.encodeToString(ShareTarget.serializer(), target.copy(cluster = clusterId))) }.getOrNull()
}

/** The screen an ichor://open or https share link names; null for anything else or an invalid one. */
fun parseShareLink(link: String): ShareTarget? =
    runCatching { TalosJson.decodeFromString(ShareTarget.serializer(), Ichorgo.parseShareLink(link)) }.getOrNull()

/** Opens the share sheet with the link to a target of the cluster on screen. */
@Composable
fun rememberShareLink(): (ShareTarget) -> Unit {
    val context = LocalContext.current
    val title = stringResource(R.string.share_link)
    return remember(context, title) {
        { target -> (context.applicationContext as TalosApp).shareLinkOf(target)?.let { shareText(context, it, title) } }
    }
}

/** A button sharing the link to [target]; [icon] a link one where Share already shares something else. */
@Composable
fun ShareLinkButton(target: ShareTarget, icon: ImageVector = Icons.Outlined.Share) {
    val share = rememberShareLink()
    TooltipIconButton(icon, stringResource(R.string.share_link), onClick = { share(target) })
}

/** A menu entry sharing the link to [target]; [onClick] closes the menu. */
@Composable
fun ShareLinkMenuItem(target: ShareTarget, onClick: () -> Unit = {}) {
    val share = rememberShareLink()
    DropdownMenuItem(
        text = { Text(stringResource(R.string.share_link)) },
        leadingIcon = { Icon(Icons.Outlined.Share, null) },
        onClick = {
            onClick()
            share(target)
        },
    )
}
