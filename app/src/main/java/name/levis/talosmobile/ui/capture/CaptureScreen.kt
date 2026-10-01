package name.levis.talosmobile.ui.capture

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import name.levis.talosmobile.R
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.model.PacketSummary
import name.levis.talosmobile.model.formatElapsed
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.asString
import name.levis.talosmobile.ui.components.KeepScreenOn
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.util.formatBytes

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CaptureScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    onCaptures: () -> Unit,
    vm: CaptureViewModel = viewModel(
        key = "capture-$node",
        factory = factory { CaptureViewModel(app.captureRepository, node, hostname) },
    ),
    linksVm: CaptureLinksViewModel = viewModel(
        key = "capture-links-$node",
        factory = factory { CaptureLinksViewModel(app.talosRepository, node) },
    ),
) {
    val context = LocalContext.current
    val captures = (context.applicationContext as TalosApp).captureRepository
    val links by linksVm.state.collectAsStateWithLifecycle()
    val options by vm.options.collectAsStateWithLifecycle()
    val filterError by vm.filterError.collectAsStateWithLifecycle()
    val filterChecking by vm.filterChecking.collectAsStateWithLifecycle()
    val session by vm.session.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var confirmLeave by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }
    var detail by remember { mutableStateOf<PacketSummary?>(null) }
    val save = rememberSaveCapture { message -> scope.launch { snackbar.showSnackbar(message.resolve(context)) } }

    LaunchedEffect(Unit) { if (links == UiState.Loading) linksVm.refresh() }
    LaunchedEffect(links) { (links as? UiState.Loaded)?.let { vm.linksLoaded(it.data) } }

    val running = session?.running == true
    if (running) KeepScreenOn()
    // Leaving while capturing asks first; leaving clears the view model, which stops it.
    BackHandler(enabled = running) { confirmLeave = true }
    fun back() {
        if (running) confirmLeave = true else onBack()
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.capture_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { IconButton(onClick = ::back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
                actions = {
                    if (!running) {
                        IconButton(onClick = onCaptures) { Icon(Icons.Outlined.FolderOpen, stringResource(R.string.captures_title)) }
                    }
                },
            )
        },
        bottomBar = { session?.let { s -> CaptureBar(s, onStop = vm::stop) } },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            val s = session
            if (s == null) {
                CaptureSetup(links, options, filterError, filterChecking, onChange = vm::update, onStart = vm::start)
            } else {
                if (!s.running) {
                    FinishedActions(
                        session = s,
                        onSave = { save(s.file) },
                        onShare = { shareCapture(context, s.file) },
                        onDelete = { confirmDelete = true },
                        onNew = vm::reset,
                    )
                }
                if (s.running) {
                    PacketList(
                        packets = s.packets,
                        start = s.firstTs,
                        live = true,
                        empty = stringResource(R.string.capture_waiting),
                        onPacket = null,
                    )
                } else if (s.file.exists()) {
                    // Live summaries are a sample: once finished, list every packet from the file.
                    val fileVm: CaptureFileViewModel = viewModel(
                        key = "capture-file-${s.file.name}",
                        factory = factory { CaptureFileViewModel(app.captureRepository, s.file) },
                    )
                    PcapFileList(fileVm, onPacket = { detail = it })
                } else {
                    Text(
                        stringResource(R.string.capture_no_packets),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(16.dp),
                    )
                }
            }
        }
    }

    detail?.let { p ->
        session?.let { s -> PacketDetailSheet(captures, s.file, p, onDismiss = { detail = null }) }
    }
    if (confirmLeave) {
        AlertDialog(
            onDismissRequest = { confirmLeave = false },
            title = { Text(stringResource(R.string.capture_leave_title)) },
            text = { Text(stringResource(R.string.capture_leave_body)) },
            confirmButton = {
                TextButton(onClick = {
                    confirmLeave = false
                    vm.stop()
                    onBack()
                }) { Text(stringResource(R.string.capture_leave_confirm)) }
            },
            dismissButton = { TextButton(onClick = { confirmLeave = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
    if (confirmDelete) {
        session?.let { s ->
            DeleteCaptureDialog(
                s.file,
                onConfirm = {
                    confirmDelete = false
                    vm.deleteCurrent()
                },
                onDismiss = { confirmDelete = false },
            )
        }
    }
}

/** Packets, bytes and elapsed time; a Stop button while running. */
@Composable
private fun CaptureBar(session: CaptureSession, onStop: () -> Unit) {
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(session.running) {
        while (session.running) {
            now = System.currentTimeMillis()
            delay(500)
        }
    }
    val end = if (session.running) now else session.finishedAt
    val elapsed = formatElapsed((end - session.startedAt) / 1000)
    val colors = LocalStatusColors.current
    Surface(color = MaterialTheme.colorScheme.surfaceContainer, modifier = Modifier.fillMaxWidth()) {
        Row(
            Modifier.navigationBarsPadding().padding(horizontal = 16.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                val count = session.packetCount.coerceAtMost(Int.MAX_VALUE.toLong()).toInt()
                Text(
                    "${pluralStringResource(R.plurals.capture_packets, count, count)} · ${formatBytes(session.bytes)} · $elapsed",
                    style = MaterialTheme.typography.bodyMedium,
                )
                val status = when {
                    session.running -> stringResource(R.string.capture_running, session.options.iface)
                    session.error != null -> stringResource(R.string.capture_failed, session.error.asString())
                    else -> stringResource(R.string.capture_finished)
                }
                Text(
                    status,
                    style = MaterialTheme.typography.labelMedium,
                    color = when {
                        session.running -> colors.ok
                        session.error != null -> colors.bad
                        else -> MaterialTheme.colorScheme.onSurfaceVariant
                    },
                )
            }
            if (session.running) Button(onClick = onStop) { Text(stringResource(R.string.capture_stop)) }
        }
    }
}

@Composable
private fun FinishedActions(session: CaptureSession, onSave: () -> Unit, onShare: () -> Unit, onDelete: () -> Unit, onNew: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            val hasFile = session.file.exists()
            Button(onClick = onSave, enabled = hasFile) { Text(stringResource(R.string.capture_save)) }
            OutlinedButton(onClick = onShare, enabled = hasFile) { Text(stringResource(R.string.capture_share)) }
            TextButton(onClick = onDelete, enabled = hasFile) { Text(stringResource(R.string.common_delete)) }
        }
        TextButton(onClick = onNew) { Text(stringResource(R.string.capture_new)) }
    }
}
