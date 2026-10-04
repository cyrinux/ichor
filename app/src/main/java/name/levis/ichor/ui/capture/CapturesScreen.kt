package name.levis.ichor.ui.capture

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.CaptureFile
import name.levis.ichor.data.CaptureRepository
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import java.io.File
import name.levis.ichor.util.formatDateTime

class CapturesViewModel(private val captures: CaptureRepository) : LoadingViewModel<List<CaptureFile>>() {
    override suspend fun fetch() = captures.list()

    fun delete(file: File) {
        viewModelScope.launch {
            captures.delete(file)
            refresh()
        }
    }
}

/** Saved captures: reopen, save, share or delete them. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CapturesScreen(
    onBack: () -> Unit,
    onOpen: (String) -> Unit,
    vm: CapturesViewModel = viewModel(key = "captures", factory = factory { CapturesViewModel(app.captureRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var deleting by remember { mutableStateOf<File?>(null) }
    val save = rememberSaveCapture { message -> scope.launch { snackbar.showSnackbar(message.resolve(context)) } }
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    // Files may have changed in the viewer: list them again whenever the screen shows.
    LaunchedEffect(lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) { vm.refresh() } }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.captures_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Box(Modifier.padding(padding).fillMaxSize()) {
            when (val s = state) {
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(s.message, vm::refresh)
                is UiState.Loaded -> CaptureFiles(
                    files = s.data,
                    onOpen = { onOpen(it.name) },
                    onSave = { save(it.file) },
                    onShare = { shareCapture(context, it.file) },
                    onDelete = { deleting = it.file },
                )
            }
        }
    }
    deleting?.let { file ->
        DeleteCaptureDialog(
            file,
            onConfirm = {
                deleting = null
                vm.delete(file)
            },
            onDismiss = { deleting = null },
        )
    }
}

@Composable
private fun CaptureFiles(
    files: List<CaptureFile>,
    onOpen: (CaptureFile) -> Unit,
    onSave: (CaptureFile) -> Unit,
    onShare: (CaptureFile) -> Unit,
    onDelete: (CaptureFile) -> Unit,
) {
    LazyColumn(contentPadding = PaddingValues(vertical = 8.dp)) {
        item {
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(
                    pluralStringResource(R.plurals.captures_total, files.size, files.size, formatBytes(files.sumOf { it.size })),
                    style = MaterialTheme.typography.titleSmall,
                )
                Text(stringResource(R.string.capture_sensitive), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.warn)
            }
        }
        if (files.isEmpty()) {
            item {
                EmptyText(stringResource(R.string.captures_empty))
            }
        }
        items(files, key = { it.name }) { f ->
            CaptureFileRow(f, onOpen = { onOpen(f) }, onSave = { onSave(f) }, onShare = { onShare(f) }, onDelete = { onDelete(f) })
        }
    }
}

@Composable
private fun CaptureFileRow(file: CaptureFile, onOpen: () -> Unit, onSave: () -> Unit, onShare: () -> Unit, onDelete: () -> Unit) {
    var menuOpen by remember { mutableStateOf(false) }
    val date = remember(file.modified) { formatDateTime(file.modified) }
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onOpen).padding(start = 16.dp, top = 8.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(file.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            MutedText("${formatBytes(file.size)} · $date")
        }
        Box {
            IconButton(onClick = { menuOpen = true }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.common_more)) }
            DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                DropdownMenuItem(text = { Text(stringResource(R.string.capture_save)) }, onClick = { menuOpen = false; onSave() })
                DropdownMenuItem(text = { Text(stringResource(R.string.capture_share)) }, onClick = { menuOpen = false; onShare() })
                DropdownMenuItem(text = { Text(stringResource(R.string.common_delete)) }, onClick = { menuOpen = false; onDelete() })
            }
        }
    }
}
