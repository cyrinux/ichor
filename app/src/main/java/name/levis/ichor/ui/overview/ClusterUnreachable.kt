package name.levis.ichor.ui.overview

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Lock
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ClusterOutage
import name.levis.ichor.model.OutageCause
import name.levis.ichor.model.OutageRecovery
import name.levis.ichor.model.RecoveryAction
import name.levis.ichor.model.RecoveryExplanation
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/** While no node answers, the overview tries again this often (and at once when the network changes). */
const val UNREACHABLE_RETRY_SECONDS = 15

/**
 * Replaces the list of red "unreachable" nodes when none answered: one explanation of why
 * (usually the phone is off the cluster's network), the error once, and what to do about it.
 * Scrollable so the surrounding pull-to-refresh still works.
 */
@Composable
fun ClusterUnreachableBox(
    outage: ClusterOutage,
    recovery: OutageRecovery,
    endpoints: List<String>,
    onRetry: () -> Unit,
    onSignIn: () -> Unit,
    onReplaceConfig: () -> Unit,
    onShowNodes: () -> Unit,
    onScan: () -> Unit,
    onEditEndpoints: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = LocalStatusColors.current
    val (icon, tint, body) = outageLook(outage.cause)
    // At least the screen's height, so the content is centered and still scrolls (pull to refresh).
    BoxWithConstraints(modifier.fillMaxSize()) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).heightIn(min = maxHeight).padding(32.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(48.dp))
            Spacer(Modifier.height(12.dp))
            Text(stringResource(R.string.overview_unreachable_title), style = MaterialTheme.typography.titleLarge, textAlign = TextAlign.Center)
            Spacer(Modifier.height(8.dp))
            Text(stringResource(body), style = MaterialTheme.typography.bodyMedium, textAlign = TextAlign.Center)
            recovery.explanation?.let {
                Text(
                    stringResource(it.text),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.padding(top = 8.dp),
                )
            }
            if (endpoints.isNotEmpty()) {
                Spacer(Modifier.height(12.dp))
                Text(
                    stringResource(R.string.overview_unreachable_endpoints, endpoints.joinToString(", ")),
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
            }
            outage.errors.forEach {
                Text(
                    it,
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.bad,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
            Spacer(Modifier.height(20.dp))
            val (primaryLabel, onPrimary) = recoveryAction(recovery.primary, onRetry, onSignIn, onReplaceConfig)
            // Wraps on a small phone or a large font: these labels are longer than Retry.
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                IchorButton(onClick = onPrimary) { Text(stringResource(primaryLabel)) }
                if (recovery.retrySecondary) IchorOutlinedButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
            }
            // Android 17 keeps the Wi-Fi/Ethernet network out of reach until allowed: often the cause.
            if (outage.cause == OutageCause.NETWORK) {
                LocalNetworkNotice(onGranted = onRetry, centered = true, modifier = Modifier.padding(top = 12.dp))
            }
            // A talosconfig shared from elsewhere may list no endpoint reachable from here.
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
                modifier = Modifier.padding(top = 8.dp),
            ) {
                if (recovery.scan) IchorOutlinedButton(onClick = onScan) { Text(stringResource(R.string.endpoint_scan_action)) }
                if (recovery.editEndpoints) IchorTextButton(onClick = onEditEndpoints) { Text(stringResource(R.string.endpoints_edit)) }
            }
            // The nodes stay one tap away: their actions sheet can still wake a node over LAN.
            IchorTextButton(onClick = onShowNodes) {
                Text(pluralStringResource(R.plurals.overview_unreachable_show_nodes, outage.nodes, outage.nodes))
            }
            Spacer(Modifier.height(4.dp))
            Text(
                stringResource(R.string.overview_unreachable_auto_retry, UNREACHABLE_RETRY_SECONDS),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

/**
 * Over the last known nodes while none answers (see [name.levis.ichor.model.withLastKnown]):
 * why, as in [ClusterUnreachableBox], and that what follows is the last known state.
 */
@Composable
fun LastKnownBanner(
    outage: ClusterOutage,
    recovery: OutageRecovery,
    onRetry: () -> Unit,
    onSignIn: () -> Unit,
    onReplaceConfig: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val (icon, tint, body) = outageLook(outage.cause)
    IchorCard(modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(24.dp))
                Spacer(Modifier.width(12.dp))
                Text(stringResource(R.string.overview_unreachable_title), style = MaterialTheme.typography.titleMedium)
            }
            Text(stringResource(body), style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(top = 8.dp))
            MutedText(
                stringResource(R.string.overview_last_known_state),
                modifier = Modifier.padding(top = 4.dp),
            )
            val (primaryLabel, onPrimary) = recoveryAction(recovery.primary, onRetry, onSignIn, onReplaceConfig)
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.padding(top = 8.dp),
            ) {
                if (recovery.primary != RecoveryAction.RETRY) IchorOutlinedButton(onClick = onPrimary) { Text(stringResource(primaryLabel)) }
                IchorOutlinedButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
            }
            if (outage.cause == OutageCause.NETWORK) {
                LocalNetworkNotice(onGranted = onRetry, modifier = Modifier.padding(top = 8.dp))
            }
        }
    }
}

/** Icon, tint and explanation of an outage, the same in the full-screen notice and the banner. */
@Composable
private fun outageLook(cause: OutageCause): Triple<ImageVector, Color, Int> {
    val colors = LocalStatusColors.current
    return when (cause) {
        OutageCause.NETWORK -> Triple(Icons.Outlined.CloudOff, colors.warn, R.string.overview_unreachable_network)
        OutageCause.CREDENTIALS -> Triple(Icons.Outlined.Lock, colors.bad, R.string.overview_unreachable_credentials)
        OutageCause.OTHER -> Triple(Icons.Outlined.ErrorOutline, colors.bad, R.string.overview_unreachable_other)
    }
}

/** The label and the tap of a recovery action, the same in the notice and the banner. */
@Composable
private fun recoveryAction(
    action: RecoveryAction,
    onRetry: () -> Unit,
    onSignIn: () -> Unit,
    onReplaceConfig: () -> Unit,
): Pair<Int, () -> Unit> {
    val context = LocalContext.current
    return when (action) {
        RecoveryAction.VPN_SETTINGS -> R.string.common_vpn_settings to { openVpnSettings(context) }
        RecoveryAction.SIGN_IN -> R.string.kube_signin_again to onSignIn
        RecoveryAction.REPLACE_CONFIG -> R.string.overview_unreachable_replace_config to onReplaceConfig
        RecoveryAction.RETRY -> R.string.common_retry to onRetry
    }
}

private val RecoveryExplanation.text: Int
    get() = when (this) {
        RecoveryExplanation.OMNI_SESSION -> R.string.overview_unreachable_credentials_omni
        RecoveryExplanation.REPLACE_CONFIG -> R.string.overview_unreachable_credentials_replace
    }

private fun openVpnSettings(context: Context) {
    runCatching { context.startActivity(Intent(Settings.ACTION_VPN_SETTINGS)) }
        .onFailure { if (it !is ActivityNotFoundException) throw it }
}
