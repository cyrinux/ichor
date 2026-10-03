package name.levis.ichor.ui.overview

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import name.levis.ichor.R
import name.levis.ichor.data.LOCAL_NETWORK_PERMISSION
import name.levis.ichor.data.hasLocalNetworkAccess
import name.levis.ichor.data.localNetworkPermissionNeeded
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.onLocalNetwork

/** Asked once per app run: a refusal is not met with the system prompt on every cluster switch. */
private var askedThisRun = false

/**
 * Android 17 keeps the Wi-Fi/Ethernet network out of reach until the user allows it. When the
 * cluster on screen is on such a network ([onLocalNetwork]), asks right away rather than
 * letting its nodes time out; [onGranted] reloads it. Clusters reached over a VPN or the
 * Internet need nothing, and are never the reason for the prompt.
 */
@Composable
fun AskLocalNetworkAccess(cluster: ContextSummary?, onGranted: () -> Unit) {
    if (!localNetworkPermissionNeeded()) return
    val context = LocalContext.current
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { if (it) onGranted() }
    LaunchedEffect(cluster?.fingerprint) {
        if (askedThisRun || cluster == null || cluster.demo || !cluster.onLocalNetwork() || hasLocalNetworkAccess(context)) {
            return@LaunchedEffect
        }
        askedThisRun = true
        launcher.launch(LOCAL_NETWORK_PERMISSION)
    }
}

/**
 * While local network access is not allowed: says so, with a button to allow it, under a
 * "cluster unreachable" notice (it is then the likely cause). Nothing otherwise.
 */
@Composable
fun LocalNetworkNotice(onGranted: () -> Unit, modifier: Modifier = Modifier, centered: Boolean = false) {
    if (!localNetworkPermissionNeeded()) return
    val context = LocalContext.current
    var blocked by remember { mutableStateOf(!hasLocalNetworkAccess(context)) }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        blocked = !granted
        if (granted) onGranted()
    }
    // Allowed meanwhile (another prompt, or the system settings): the notice goes.
    LifecycleResumeEffect(Unit) {
        blocked = !hasLocalNetworkAccess(context)
        onPauseOrDispose {}
    }
    if (!blocked) return
    Column(modifier, horizontalAlignment = if (centered) Alignment.CenterHorizontally else Alignment.Start) {
        Text(
            stringResource(R.string.local_network_blocked),
            style = MaterialTheme.typography.bodySmall,
            textAlign = if (centered) TextAlign.Center else TextAlign.Start,
        )
        OutlinedButton(onClick = { launcher.launch(LOCAL_NETWORK_PERMISSION) }, modifier = Modifier.padding(top = 4.dp)) {
            Text(stringResource(R.string.local_network_allow))
        }
    }
}
