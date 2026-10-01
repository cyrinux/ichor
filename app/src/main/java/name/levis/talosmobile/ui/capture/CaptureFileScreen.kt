package name.levis.talosmobile.ui.capture

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.talosmobile.R
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.data.CaptureRepository
import name.levis.talosmobile.model.PCAP_PAGE_SIZE
import name.levis.talosmobile.model.PacketSummary
import name.levis.talosmobile.ui.UiText
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.asString
import name.levis.talosmobile.ui.components.ErrorBox
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.uiText
import java.io.File

/** Packets of a saved capture read so far, [PCAP_PAGE_SIZE] at a time. */
data class PcapView(
    val packets: List<PacketSummary> = emptyList(),
    val total: Long = 0,
    val loading: Boolean = false,
    val error: UiText? = null,
    val loadedOnce: Boolean = false,
) {
    val hasMore: Boolean get() = packets.size < total
}

class CaptureFileViewModel(private val captures: CaptureRepository, val file: File?) : ViewModel() {
    private val _state = MutableStateFlow(PcapView())
    val state: StateFlow<PcapView> = _state.asStateFlow()

    fun loadMore() {
        val current = _state.value
        if (current.loading || file == null || (current.loadedOnce && !current.hasMore)) return
        _state.update { it.copy(loading = true, error = null) }
        viewModelScope.launch {
            try {
                val page = captures.read(file, current.packets.size, PCAP_PAGE_SIZE)
                _state.update {
                    it.copy(packets = it.packets + page.packets, total = page.total, loading = false, loadedOnce = true)
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _state.update { it.copy(loading = false, error = e.uiText()) }
            }
        }
    }
}

/** A saved capture: its packets (paged), packet details, save/share/delete. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CaptureFileScreen(
    name: String,
    onBack: () -> Unit,
    vm: CaptureFileViewModel = viewModel(
        key = "capture-file-$name",
        factory = factory { CaptureFileViewModel(app.captureRepository, app.captureRepository.find(name)) },
    ),
) {
    val context = LocalContext.current
    val captures = (context.applicationContext as TalosApp).captureRepository
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var menuOpen by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }
    var detail by remember { mutableStateOf<PacketSummary?>(null) }
    val save = rememberSaveCapture { message -> scope.launch { snackbar.showSnackbar(message.resolve(context)) } }
    val file = vm.file

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.captures_title))
                        Text(name, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
                actions = {
                    if (file != null) {
                        Box {
                            IconButton(onClick = { menuOpen = true }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.common_more)) }
                            DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                                DropdownMenuItem(text = { Text(stringResource(R.string.capture_save)) }, onClick = { menuOpen = false; save(file) })
                                DropdownMenuItem(text = { Text(stringResource(R.string.capture_share)) }, onClick = { menuOpen = false; shareCapture(context, file) })
                                DropdownMenuItem(text = { Text(stringResource(R.string.common_delete)) }, onClick = { menuOpen = false; confirmDelete = true })
                            }
                        }
                    }
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            when {
                file == null -> ErrorBox(UiText.Res(R.string.capture_missing), onBack)
                else -> PcapFileList(vm, onPacket = { detail = it })
            }
        }
    }

    if (file != null) {
        detail?.let { p -> PacketDetailSheet(captures, file, p, onDismiss = { detail = null }) }
        if (confirmDelete) {
            DeleteCaptureDialog(
                file,
                onConfirm = {
                    confirmDelete = false
                    scope.launch {
                        captures.delete(file)
                        onBack()
                    }
                },
                onDismiss = { confirmDelete = false },
            )
        }
    }
}

/** The packets of a saved capture, [PCAP_PAGE_SIZE] more on demand. */
@Composable
fun PcapFileList(vm: CaptureFileViewModel, onPacket: (PacketSummary) -> Unit) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(vm) { if (!state.loadedOnce) vm.loadMore() }
    when {
        !state.loadedOnce && state.error != null -> ErrorBox(state.error!!, vm::loadMore)
        !state.loadedOnce -> LoadingBox()
        else -> Column {
            Text(
                stringResource(R.string.captures_showing, state.packets.size, state.total),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
            PacketList(
                packets = state.packets,
                start = state.packets.firstOrNull()?.ts ?: 0,
                live = false,
                empty = stringResource(R.string.capture_no_packets),
                onPacket = onPacket,
                footer = if (state.hasMore || state.error != null) {
                    { item(key = "more") { LoadMore(state, vm::loadMore) } }
                } else {
                    null
                },
            )
        }
    }
}

@Composable
private fun LoadMore(state: PcapView, onLoad: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        state.error?.let { Text(it.asString(), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
        if (state.loading) {
            CircularProgressIndicator()
        } else {
            OutlinedButton(onClick = onLoad) { Text(stringResource(R.string.captures_load_more, PCAP_PAGE_SIZE)) }
        }
    }
}
