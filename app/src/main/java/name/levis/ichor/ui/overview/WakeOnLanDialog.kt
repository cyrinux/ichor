package name.levis.ichor.ui.overview

import android.content.Context
import android.widget.Toast
import androidx.compose.runtime.key
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.data.WakeOnLanSender
import name.levis.ichor.model.wolKey
import name.levis.ichor.ui.userMessage
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.LinkInfo
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.WOL_DEFAULT_BROADCAST
import name.levis.ichor.model.WOL_DEFAULT_PORT
import name.levis.ichor.model.WolInputError
import name.levis.ichor.model.WolInputException
import name.levis.ichor.model.WolTarget
import name.levis.ichor.model.parseMac
import name.levis.ichor.model.parseWolTarget
import name.levis.ichor.model.wolCandidates

/**
 * How to wake [node]: its MAC address, and where to send the magic packet. While the node
 * is up, the MACs of its Ethernet links are offered, so it can be set before it is needed.
 * [onSave] with null forgets the setting.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun WakeOnLanDialog(
    node: NodeOverview,
    saved: WolTarget?,
    onSave: (WolTarget?) -> Unit,
    onDismiss: () -> Unit,
) {
    var mac by remember { mutableStateOf(saved?.mac.orEmpty()) }
    var broadcast by remember { mutableStateOf(saved?.broadcast.orEmpty()) }
    var port by remember { mutableStateOf(saved?.port?.takeIf { it != WOL_DEFAULT_PORT }?.toString().orEmpty()) }
    var error by remember { mutableStateOf<WolInputError?>(null) }
    val links = rememberNodeLinks(node)

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.wol_title, node.hostname)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(stringResource(R.string.wol_hint), style = MaterialTheme.typography.bodySmall)
                OutlinedTextField(
                    value = mac,
                    onValueChange = {
                        mac = it
                        error = null
                    },
                    label = { Text(stringResource(R.string.wol_mac)) },
                    placeholder = { Text("aa:bb:cc:dd:ee:ff") },
                    isError = error == WolInputError.MAC,
                    supportingText = { if (error == WolInputError.MAC) Text(stringResource(R.string.wol_mac_invalid)) },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace),
                    keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                if (links.isNotEmpty()) {
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        links.forEach { link ->
                            FilterChip(
                                selected = parseMac(mac)?.toList() == parseMac(link.hardwareAddr)?.toList(),
                                onClick = {
                                    mac = link.hardwareAddr
                                    error = null
                                },
                                label = { Text("${link.name}  ${link.hardwareAddr}", fontFamily = FontFamily.Monospace) },
                            )
                        }
                    }
                }
                OutlinedTextField(
                    value = broadcast,
                    onValueChange = {
                        broadcast = it
                        error = null
                    },
                    label = { Text(stringResource(R.string.wol_broadcast)) },
                    placeholder = { Text(WOL_DEFAULT_BROADCAST) },
                    isError = error == WolInputError.BROADCAST,
                    supportingText = {
                        Text(
                            stringResource(
                                if (error == WolInputError.BROADCAST) R.string.wol_broadcast_invalid else R.string.wol_broadcast_hint,
                            ),
                        )
                    },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = port,
                    onValueChange = {
                        port = it.filter(Char::isDigit).take(5)
                        error = null
                    },
                    label = { Text(stringResource(R.string.wol_port)) },
                    placeholder = { Text(WOL_DEFAULT_PORT.toString()) },
                    isError = error == WolInputError.PORT,
                    supportingText = { if (error == WolInputError.PORT) Text(stringResource(R.string.wol_port_invalid)) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            TextButton(onClick = {
                parseWolTarget(mac, broadcast, port).fold(
                    onSuccess = onSave,
                    onFailure = { error = (it as? WolInputException)?.error },
                )
            }) { Text(stringResource(R.string.wol_save)) }
        },
        dismissButton = {
            if (saved != null) {
                TextButton(onClick = { onSave(null) }) { Text(stringResource(R.string.wol_forget)) }
            }
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
        },
    )
}

/**
 * Wake-on-LAN for the nodes of the cluster [fingerprint]: what the actions sheet offers for
 * a node, and the settings dialog while it is open. Nothing in screenshot mode: node
 * addresses are masked then (they would not match the saved ones), and MACs are revealing.
 */
@Composable
fun rememberWakeOnLan(fingerprint: String?): (NodeOverview) -> WolActions? {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val targets by app.wakeOnLan.targets.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    var editing by remember { mutableStateOf<NodeOverview?>(null) }
    // Screenshot mode turned on with the dialog open: closed, not just hidden until it is off.
    LaunchedEffect(mask.enabled) { if (mask.enabled) editing = null }
    if (fingerprint.isNullOrBlank() || mask.enabled) return { null }

    editing?.let { node ->
        key(node.node) {
            WakeOnLanDialog(
                node = node,
                saved = targets[wolKey(fingerprint, node.node)],
                onSave = {
                    app.wakeOnLan.set(fingerprint, node.node, it)
                    editing = null
                },
                onDismiss = { editing = null },
            )
        }
    }
    return { node ->
        val target = targets[wolKey(fingerprint, node.node)]
        WolActions(
            target = target,
            // Not the screen's scope: leaving the overview must not cut the packets short.
            onWake = { target?.let { ProcessLifecycleOwner.get().lifecycleScope.launch { wake(app, node, it) } } },
            onSettings = { editing = node },
        )
    }
}

private suspend fun wake(context: Context, node: NodeOverview, target: WolTarget) {
    val message = try {
        val destination = WakeOnLanSender(context).send(target)
        context.getString(R.string.wol_sent, node.hostname, destination)
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        context.getString(R.string.wol_failed, e.userMessage())
    }
    Toast.makeText(context, message, Toast.LENGTH_LONG).show()
}

/** The node's Ethernet links with a MAC address, best effort: none while it is down. */
@Composable
private fun rememberNodeLinks(node: NodeOverview): List<LinkInfo> {
    val talos = (LocalContext.current.applicationContext as TalosApp).talosRepository
    var links by remember(node.node) { mutableStateOf(emptyList<LinkInfo>()) }
    LaunchedEffect(node.node, node.reachable) {
        if (node.reachable) {
            links = runCatching { wolCandidates(talos.network(node.node).links) }.getOrDefault(emptyList())
        }
    }
    return links
}
