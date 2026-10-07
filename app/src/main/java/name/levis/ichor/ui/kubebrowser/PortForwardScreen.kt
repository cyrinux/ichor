package name.levis.ichor.ui.kubebrowser

import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.ForwardEvent
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.ContainerPort
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.containerPorts
import name.levis.ichor.model.forwardUrl
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.userMessage

/** Connection errors kept on screen, the latest ones. */
private const val FORWARD_ERRORS_MAX = 20

/**
 * A forward: [port] of the pod, [address] once listening ("127.0.0.1:PORT"), the failed
 * connections, [error] why it ended; [running] until it ends or is stopped.
 */
data class ForwardState(
    val port: Int = 0,
    val running: Boolean = false,
    val address: String? = null,
    val connectionErrors: List<String> = emptyList(),
    val error: String? = null,
)

/**
 * A port-forward to [pod] while the screen lives: one at a time, stopped by [stop], by
 * another [start], or when the screen is left (the ViewModel is cleared). No service keeps
 * it once the screen is gone.
 */
class PortForwardViewModel(private val browser: KubeBrowserRepository, private val namespace: String, private val pod: String) : ViewModel() {
    private val _ports = MutableStateFlow<UiState<List<ContainerPort>>>(UiState.Loading)
    /** The TCP ports the pod's containers declare, read from its YAML. */
    val ports: StateFlow<UiState<List<ContainerPort>>> = _ports.asStateFlow()

    private val _forward = MutableStateFlow<ForwardState?>(null)
    val forward: StateFlow<ForwardState?> = _forward.asStateFlow()
    private var job: Job? = null

    init {
        viewModelScope.launch {
            _ports.value = cancellableCatching { containerPorts(browser.objectYaml(KubeObjectRef.pod(namespace, pod), reveal = false)) }
                .fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
        }
    }

    fun start(port: Int) {
        job?.cancel()
        _forward.value = ForwardState(port = port, running = true)
        job = viewModelScope.launch {
            try {
                browser.portForward(namespace, pod, port).collect { event ->
                    _forward.update { s ->
                        s?.let {
                            when (event) {
                                is ForwardEvent.Ready -> it.copy(address = event.address)
                                is ForwardEvent.ConnectionError -> it.copy(connectionErrors = (it.connectionErrors + event.message).takeLast(FORWARD_ERRORS_MAX))
                                is ForwardEvent.Done -> it.copy(running = false, error = event.error)
                            }
                        }
                    }
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _forward.update { it?.copy(running = false, error = e.userMessage()) }
            }
        }
    }

    fun stop() {
        job?.cancel()
        _forward.update { it?.copy(running = false) }
    }
}

/**
 * Forwards a port of [pod] to the phone, like `kubectl port-forward`: one of the ports its
 * containers declare, or one typed. The local URL opens in the browser or is copied; it is
 * reachable from this phone only (127.0.0.1) and stops when this screen is left.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun PortForwardScreen(
    namespace: String,
    pod: String,
    onBack: () -> Unit,
    vm: PortForwardViewModel = viewModel(key = "forward-$namespace/$pod", factory = factory { PortForwardViewModel(app.kubeBrowser, namespace, pod) }),
) {
    val ports by vm.ports.collectAsStateWithLifecycle()
    val forward by vm.forward.collectAsStateWithLifecycle()
    var typed by rememberSaveable { mutableStateOf("") }
    val declared = (ports as? UiState.Loaded)?.data.orEmpty()
    LaunchedEffect(declared) { if (typed.isEmpty()) declared.firstOrNull()?.let { typed = it.port.toString() } }
    val port = typed.trim().toIntOrNull()?.takeIf { it in 1..65535 }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.kb_forward_title))
                        Text("$namespace/$pod", style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Column(
            Modifier.padding(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            InfoNotice(stringResource(R.string.kb_forward_local_only))
            when (val p = ports) {
                UiState.Loading -> LinearProgressIndicator(Modifier.fillMaxWidth())
                is UiState.Failed -> MutedText(stringResource(R.string.kb_forward_ports_failed))
                is UiState.Loaded -> if (p.data.isEmpty()) {
                    MutedText(stringResource(R.string.kb_forward_no_ports))
                } else {
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        p.data.forEach { c ->
                            FilterChip(
                                selected = typed == c.port.toString(),
                                onClick = { typed = c.port.toString() },
                                label = { Text(if (c.name.isEmpty()) "${c.port}" else "${c.port} · ${c.name}", fontFamily = FontFamily.Monospace) },
                            )
                        }
                    }
                }
            }
            val running = forward?.running == true
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = typed,
                    onValueChange = { v -> typed = v.filter(Char::isDigit).take(5) },
                    label = { Text(stringResource(R.string.kb_forward_port)) },
                    singleLine = true,
                    enabled = !running,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    modifier = Modifier.weight(1f),
                )
                if (running) {
                    OutlinedButton(onClick = vm::stop) { Text(stringResource(R.string.kb_forward_stop)) }
                } else {
                    Button(onClick = { port?.let(vm::start) }, enabled = port != null) { Text(stringResource(R.string.kb_forward_start)) }
                }
            }
            forward?.let { ForwardStatus(it) }
        }
    }
}

@Composable
private fun ForwardStatus(f: ForwardState) {
    val context = LocalContext.current
    val address = f.address
    when {
        f.error != null -> ErrorCard(stringResource(R.string.kb_forward_failed, f.error))
        f.running && address == null -> LinearProgressIndicator(Modifier.fillMaxWidth())
        !f.running -> MutedText(stringResource(R.string.kb_forward_stopped))
    }
    if (f.running && address != null) {
        val url = forwardUrl(address)
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                MutedText(stringResource(R.string.kb_forward_ready, f.port))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    SelectionContainer(Modifier.weight(1f)) { Text(url, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace) }
                    TooltipIconButton(Icons.Outlined.ContentCopy, stringResource(R.string.kb_copy), onClick = { copyWithToast(context, url, url, sensitive = false) })
                }
                Button(onClick = {
                    try {
                        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
                    } catch (_: ActivityNotFoundException) {
                        Toast.makeText(context, R.string.kb_forward_no_browser, Toast.LENGTH_SHORT).show()
                    }
                }) {
                    Icon(Icons.AutoMirrored.Outlined.OpenInNew, contentDescription = null)
                    Text(stringResource(R.string.kb_forward_open), Modifier.padding(start = 8.dp))
                }
            }
        }
    }
    if (f.connectionErrors.isNotEmpty()) {
        Text(stringResource(R.string.kb_forward_connection_errors), style = MaterialTheme.typography.titleSmall)
        f.connectionErrors.asReversed().forEach { MutedText(it, maxLines = 3, overflow = TextOverflow.Ellipsis) }
    }
}
