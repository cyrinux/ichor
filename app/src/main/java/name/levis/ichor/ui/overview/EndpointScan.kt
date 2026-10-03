package name.levis.ichor.ui.overview

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.LOCAL_NETWORK_PERMISSION
import name.levis.ichor.data.hasLocalNetworkAccess
import name.levis.ichor.data.localAddresses
import name.levis.ichor.data.localNetworkPermissionNeeded
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.EndpointMatch
import name.levis.ichor.model.scanNetworks
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.userMessage

sealed interface EndpointScanState {
    data object Idle : EndpointScanState

    data class Scanning(val networks: List<String>) : EndpointScanState

    /** [matches] were added to their contexts; [localAccess] false when Android kept the LAN out of reach. */
    data class Done(val matches: List<EndpointMatch>, val networks: List<String>, val localAccess: Boolean) : EndpointScanState

    data class Failed(val message: String) : EndpointScanState
}

/**
 * Searches the networks around the phone for the nodes of the stored clusters (a talosconfig
 * shared without an endpoint reachable from here) and adds those that answer with a
 * cluster's credentials to its endpoints.
 */
class EndpointScanViewModel(
    private val find: suspend (List<String>) -> List<EndpointMatch>,
    private val add: suspend (List<EndpointMatch>) -> Unit,
) : ViewModel() {
    private val _state = MutableStateFlow<EndpointScanState>(EndpointScanState.Idle)
    val state: StateFlow<EndpointScanState> = _state.asStateFlow()

    fun scan(networks: List<String>, localAccess: Boolean) {
        if (_state.value is EndpointScanState.Scanning) return
        _state.value = EndpointScanState.Scanning(networks)
        viewModelScope.launch {
            _state.value = try {
                val matches = find(networks)
                if (matches.isNotEmpty()) add(matches)
                EndpointScanState.Done(matches, networks, localAccess)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                EndpointScanState.Failed(e.userMessage())
            }
        }
    }

    /** Drops the result. A scan under way keeps going: it adds what it finds. */
    fun reset() {
        if (_state.value !is EndpointScanState.Scanning) _state.value = EndpointScanState.Idle
    }
}

/**
 * Asks for local network access where Android wants it, then scans and shows the outcome.
 * [contexts] are the stored clusters, whose endpoints and nodes hint at where to look;
 * [labelOf] names a context as the cluster list does.
 */
@Composable
fun EndpointScanDialog(
    vm: EndpointScanViewModel,
    contexts: List<ContextSummary>,
    labelOf: (String) -> String,
    onEditEndpoints: (() -> Unit)?,
    onDismiss: () -> Unit,
) {
    val context = LocalContext.current
    val state by vm.state.collectAsStateWithLifecycle()
    val start = {
        val known = contexts.flatMap { it.endpoints + it.nodes }
        vm.scan(scanNetworks(localAddresses(context), known), hasLocalNetworkAccess(context))
    }
    // Denied: the scan still runs, networks reached over a VPN do not need the permission.
    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { start() }
    LaunchedEffect(Unit) {
        // A result from before (the dialog closed while that search ran) gives way to a new search.
        vm.reset()
        if (vm.state.value !is EndpointScanState.Idle) return@LaunchedEffect
        if (localNetworkPermissionNeeded() && !hasLocalNetworkAccess(context)) permission.launch(LOCAL_NETWORK_PERMISSION) else start()
    }
    val close = {
        vm.reset()
        onDismiss()
    }

    AlertDialog(
        onDismissRequest = close,
        title = { Text(stringResource(R.string.endpoint_scan_title)) },
        text = {
            Column(Modifier.heightIn(max = 420.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                when (val s = state) {
                    EndpointScanState.Idle -> Text(stringResource(R.string.endpoint_scan_body))
                    is EndpointScanState.Scanning -> {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            CircularProgressIndicator(Modifier.size(24.dp))
                            Text(stringResource(R.string.endpoint_scan_running))
                        }
                        Networks(s.networks)
                    }
                    is EndpointScanState.Done -> ScanResult(s, labelOf)
                    is EndpointScanState.Failed -> Text(s.message, color = MaterialTheme.colorScheme.error)
                }
            }
        },
        confirmButton = { TextButton(onClick = close) { Text(stringResource(R.string.common_ok)) } },
        dismissButton = {
            val s = state
            val nothing = s is EndpointScanState.Failed || (s is EndpointScanState.Done && s.matches.isEmpty())
            if (nothing && onEditEndpoints != null) {
                TextButton(onClick = {
                    close()
                    onEditEndpoints()
                }) { Text(stringResource(R.string.endpoints_edit)) }
            }
        },
    )
}

@Composable
private fun ScanResult(s: EndpointScanState.Done, labelOf: (String) -> String) {
    if (s.matches.isEmpty()) {
        Text(stringResource(R.string.endpoint_scan_none))
        if (!s.localAccess) {
            Text(stringResource(R.string.endpoint_scan_no_permission), color = MaterialTheme.colorScheme.error)
        }
        Networks(s.networks)
        return
    }
    Text(pluralStringResource(R.plurals.endpoint_scan_found, s.matches.size, s.matches.size))
    s.matches.forEach { match ->
        Column {
            Text(
                listOf(match.endpoint, match.hostname, match.role, match.version).filter { it.isNotBlank() }.joinToString(" · "),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
            )
            Text(
                stringResource(R.string.endpoint_scan_added_to, match.contexts.joinToString(", ") { labelOf(it) }),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
    Text(
        stringResource(R.string.endpoint_scan_discovery_hint),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

@Composable
private fun Networks(networks: List<String>) {
    Text(
        stringResource(R.string.endpoint_scan_networks, networks.joinToString(", ")),
        style = MaterialTheme.typography.bodySmall,
        fontFamily = FontFamily.Monospace,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(top = 4.dp),
    )
}

/**
 * The endpoint editor of the cluster named [editing] and the network search ([scanning]),
 * shown over a screen. [onEdit] and [onScan] change which is open.
 */
@Composable
fun EndpointTools(
    config: StoredConfig,
    labels: ClusterLabels,
    editing: String?,
    scanning: Boolean,
    onEdit: (String?) -> Unit,
    onScan: (Boolean) -> Unit,
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val scanVm: EndpointScanViewModel = viewModel(
        factory = factory { EndpointScanViewModel(app.talosRepository::findEndpoints, app::addFoundEndpoints) },
    )
    val contexts = config.summary.contexts
    val labelOf = { name: String -> contexts.firstOrNull { it.name == name }?.let(labels::of) ?: name }

    contexts.firstOrNull { it.name == editing }?.let { context ->
        EndpointsDialog(
            context = context,
            label = labels.of(context),
            onProbe = { app.talosRepository.probeEndpoint(context.name, it) },
            onSave = { app.setClusterEndpoints(context.name, it) },
            onScan = {
                onEdit(null)
                onScan(true)
            },
            onDismiss = { onEdit(null) },
        )
    }
    if (scanning) {
        EndpointScanDialog(
            vm = scanVm,
            contexts = contexts,
            labelOf = labelOf,
            // Not in screenshot mode: the editor would show, and save, fake endpoints.
            onEditEndpoints = { onEdit(config.activeContext) }.takeIf { !labels.masked && config.activeSummary?.demo == false },
            onDismiss = { onScan(false) },
        )
    }
}
