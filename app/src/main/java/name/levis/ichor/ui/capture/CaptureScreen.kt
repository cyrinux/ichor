package name.levis.ichor.ui.capture

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FolderOpen
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.design.IchorTopAppBar
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
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.PacketSummary
import name.levis.ichor.model.formatElapsed
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.AllowLandscape
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.readableWidth

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CaptureScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    onCaptures: () -> Unit,
    /** A second node to capture on too, prefilled ("" for none). */
    peer: String = "",
    peerHost: String = "",
    vm: CaptureViewModel = viewModel(
        key = "capture-$node-$peer",
        factory = factory { CaptureViewModel(app.captureRepository, node, hostname, peer, peerHost) },
    ),
    peersVm: CapturePeersViewModel = viewModel(
        key = "capture-peers-$node",
        factory = factory { CapturePeersViewModel(app.talosRepository, node) },
    ),
    linksVm: CaptureLinksViewModel = viewModel(
        key = "capture-links-$node",
        factory = factory { CaptureLinksViewModel(app.talosRepository, node) },
    ),
) {
    AllowLandscape()
    val context = LocalContext.current
    val captures = (context.applicationContext as TalosApp).captureRepository
    val links by linksVm.state.collectAsStateWithLifecycle()
    val peers by peersVm.state.collectAsStateWithLifecycle()
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

    LaunchedEffect(Unit) {
        if (links == UiState.Loading) linksVm.refresh()
        if (peers == UiState.Loading) peersVm.refresh()
    }
    LaunchedEffect(links) { (links as? UiState.Loaded)?.let { vm.linksLoaded(it.data) } }

    val running = session?.running == true
    if (running) KeepScreenOn()
    // Leaving while capturing asks first; leaving clears the view model, which stops it.
    BackHandler(enabled = running) { confirmLeave = true }
    fun back() {
        if (running) confirmLeave = true else onBack()
    }

    IchorScaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            IchorTopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.capture_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { BackButton(::back) },
                actions = {
                    if (!running) {
                        TooltipIconButton(Icons.Outlined.FolderOpen, stringResource(R.string.captures_title), onClick = onCaptures)
                    }
                },
            )
        },
        bottomBar = { session?.let { s -> CaptureBar(s, onStop = vm::stop) } },
    ) { padding ->
        val s = session
        // The packets use the whole width (read sideways too); the setup form stays a readable column.
        Column(Modifier.padding(padding).then(if (s == null) Modifier.readableWidth() else Modifier).fillMaxSize()) {
            if (s == null) {
                CaptureSetup(links, options, filterError, filterChecking, onChange = vm::update, onStart = vm::start, peers = peers)
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
                    EmptyText(stringResource(R.string.capture_no_packets))
                }
            }
        }
    }

    detail?.let { p ->
        session?.let { s -> PacketDetailSheet(captures, s.file, p, onDismiss = { detail = null }) }
    }
    if (confirmLeave) {
        ConfirmDialog(
            title = stringResource(R.string.capture_leave_title),
            text = stringResource(R.string.capture_leave_body),
            confirm = stringResource(R.string.capture_leave_confirm),
            onConfirm = {
                confirmLeave = false
                vm.stop()
                onBack()
            },
            onDismiss = { confirmLeave = false },
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
                if (session.nodeStats.isNotEmpty()) {
                    val perNode = session.nodeStats.map { n ->
                        val c = n.packets.coerceAtMost(Int.MAX_VALUE.toLong()).toInt()
                        "${n.hostname}: ${pluralStringResource(R.plurals.capture_packets, c, c)}"
                    }
                    Text(perNode.joinToString("  ·  "), style = MaterialTheme.typography.labelMedium)
                    session.clockGapMs?.let { ClockGapNote(it) }
                }
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
            if (session.running) IchorButton(onClick = onStop) { Text(stringResource(R.string.capture_stop)) }
        }
    }
}

@Composable
private fun FinishedActions(session: CaptureSession, onSave: () -> Unit, onShare: () -> Unit, onDelete: () -> Unit, onNew: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            val hasFile = session.file.exists()
            IchorButton(onClick = onSave, enabled = hasFile) { Text(stringResource(R.string.capture_save)) }
            IchorOutlinedButton(onClick = onShare, enabled = hasFile) { Text(stringResource(R.string.capture_share)) }
            IchorTextButton(onClick = onDelete, enabled = hasFile) { Text(stringResource(R.string.common_delete)) }
        }
        IchorTextButton(onClick = onNew) { Text(stringResource(R.string.capture_new)) }
    }
}
