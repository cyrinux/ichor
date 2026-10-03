package name.levis.ichor.ui.overview

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
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
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
    endpoints: List<String>,
    onRetry: () -> Unit,
    onShowNodes: () -> Unit,
    modifier: Modifier = Modifier,
    onScan: (() -> Unit)? = null,
    onEditEndpoints: (() -> Unit)? = null,
) {
    val context = LocalContext.current
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
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (outage.cause == OutageCause.NETWORK) {
                    Button(onClick = { openVpnSettings(context) }) { Text(stringResource(R.string.common_vpn_settings)) }
                    OutlinedButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
                } else {
                    Button(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
                }
            }
            // Android 17 keeps the Wi-Fi/Ethernet network out of reach until allowed: often the cause.
            if (outage.cause == OutageCause.NETWORK) {
                LocalNetworkNotice(onGranted = onRetry, centered = true, modifier = Modifier.padding(top = 12.dp))
            }
            // A talosconfig shared from elsewhere may list no endpoint reachable from here.
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(top = 8.dp)) {
                if (outage.cause == OutageCause.NETWORK) {
                    onScan?.let { OutlinedButton(onClick = it) { Text(stringResource(R.string.endpoint_scan_action)) } }
                }
                onEditEndpoints?.let { TextButton(onClick = it) { Text(stringResource(R.string.endpoints_edit)) } }
            }
            // The nodes stay one tap away: their actions sheet can still wake a node over LAN.
            TextButton(onClick = onShowNodes) {
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
fun LastKnownBanner(outage: ClusterOutage, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val (icon, tint, body) = outageLook(outage.cause)
    Card(modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(24.dp))
                Spacer(Modifier.width(12.dp))
                Text(stringResource(R.string.overview_unreachable_title), style = MaterialTheme.typography.titleMedium)
            }
            Text(stringResource(body), style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(top = 8.dp))
            Text(
                stringResource(R.string.overview_last_known_state),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 4.dp),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(top = 8.dp)) {
                if (outage.cause == OutageCause.NETWORK) {
                    OutlinedButton(onClick = { openVpnSettings(context) }) { Text(stringResource(R.string.common_vpn_settings)) }
                }
                OutlinedButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
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

private fun openVpnSettings(context: Context) {
    runCatching { context.startActivity(Intent(Settings.ACTION_VPN_SETTINGS)) }
        .onFailure { if (it !is ActivityNotFoundException) throw it }
}
