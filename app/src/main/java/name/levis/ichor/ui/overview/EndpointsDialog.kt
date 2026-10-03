package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.NetworkCheck
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.ENDPOINT_MAX
import name.levis.ichor.model.EndpointProbe
import name.levis.ichor.model.isEndpoint
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage

/** The outcome of testing an endpoint: under way, what it answered, or why it did not. */
private sealed interface ProbeResult {
    data object Running : ProbeResult

    data class Answered(val probe: EndpointProbe) : ProbeResult

    data class Failed(val message: String) : ProbeResult
}

/**
 * Edits the endpoints of [context]'s talosconfig context (the addresses the app connects
 * through): add, remove, test each with the cluster's credentials. An endpoint that does not
 * answer now can still be saved (it may answer over a VPN). [onScan] searches the network instead.
 */
@Composable
fun EndpointsDialog(
    context: ContextSummary,
    label: String,
    onProbe: suspend (String) -> EndpointProbe,
    onSave: suspend (List<String>) -> Unit,
    onScan: () -> Unit,
    onDismiss: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    var endpoints by remember { mutableStateOf(context.endpoints) }
    var probes by remember { mutableStateOf(emptyMap<String, ProbeResult>()) }
    var draft by remember { mutableStateOf("") }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    val probe = { endpoint: String ->
        probes = probes + (endpoint to ProbeResult.Running)
        scope.launch {
            val result = try {
                ProbeResult.Answered(onProbe(endpoint))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                ProbeResult.Failed(e.userMessage())
            }
            probes = probes + (endpoint to result)
        }
    }
    val add = {
        val endpoint = draft.trim()
        if (isEndpoint(endpoint) && endpoint !in endpoints) {
            endpoints = endpoints + endpoint
            draft = ""
            probe(endpoint)
        }
    }

    AlertDialog(
        onDismissRequest = { if (!saving) onDismiss() },
        title = { Text(stringResource(R.string.endpoints_title, label), maxLines = 2, overflow = TextOverflow.Ellipsis) },
        text = {
            Column(Modifier.heightIn(max = 480.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.endpoints_body), style = MaterialTheme.typography.bodySmall)
                endpoints.forEach { endpoint ->
                    EndpointRow(
                        endpoint = endpoint,
                        result = probes[endpoint],
                        onTest = { probe(endpoint) },
                        onRemove = { endpoints = endpoints - endpoint },
                    )
                }
                OutlinedTextField(
                    value = draft,
                    onValueChange = { draft = it.take(ENDPOINT_MAX) },
                    label = { Text(stringResource(R.string.endpoints_add_label)) },
                    placeholder = { Text("192.168.1.10") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done),
                    keyboardActions = KeyboardActions(onDone = { add() }),
                    trailingIcon = {
                        IconButton(onClick = add, enabled = isEndpoint(draft.trim())) {
                            Icon(Icons.Outlined.Add, stringResource(R.string.endpoints_add))
                        }
                    },
                    modifier = Modifier.fillMaxWidth(),
                )
                TextButton(onClick = onScan) { Text(stringResource(R.string.endpoint_scan_action)) }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
            }
        },
        confirmButton = {
            TextButton(
                enabled = !saving && endpoints.isNotEmpty() && endpoints != context.endpoints,
                onClick = {
                    saving = true
                    error = null
                    scope.launch {
                        try {
                            onSave(endpoints)
                            onDismiss()
                        } catch (e: CancellationException) {
                            throw e
                        } catch (e: Exception) {
                            error = e.userMessage()
                        } finally {
                            saving = false
                        }
                    }
                },
            ) { Text(stringResource(R.string.endpoints_save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss, enabled = !saving) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun EndpointRow(endpoint: String, result: ProbeResult?, onTest: () -> Unit, onRemove: () -> Unit) {
    val colors = LocalStatusColors.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(endpoint, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            when (result) {
                null, ProbeResult.Running -> Unit
                is ProbeResult.Answered -> Text(
                    stringResource(
                        R.string.endpoints_answered,
                        listOf(result.probe.hostname, result.probe.version).filter { it.isNotBlank() }.joinToString(" · "),
                    ),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.ok,
                )
                is ProbeResult.Failed -> Text(result.message, style = MaterialTheme.typography.bodySmall, color = colors.bad)
            }
        }
        if (result == ProbeResult.Running) {
            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        } else {
            IconButton(onClick = onTest) { Icon(Icons.Outlined.NetworkCheck, stringResource(R.string.endpoints_test, endpoint)) }
        }
        IconButton(onClick = onRemove) { Icon(Icons.Outlined.Delete, stringResource(R.string.endpoints_remove, endpoint)) }
    }
}
