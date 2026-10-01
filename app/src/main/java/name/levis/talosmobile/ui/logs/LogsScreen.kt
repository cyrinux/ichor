package name.levis.talosmobile.ui.logs

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.model.LogTail
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.DataFreshness
import name.levis.talosmobile.ui.components.ErrorBox
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.factory

/** [service] null means the kernel log (dmesg). */
class LogsViewModel(
    private val talos: TalosRepository,
    private val node: String,
    private val service: String?,
) : LoadingViewModel<LogTail>() {
    override suspend fun fetch() = talos.logs(node, service)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LogsScreen(
    node: String,
    hostname: String,
    service: String?,
    onBack: () -> Unit,
    vm: LogsViewModel = viewModel(
        key = "logs-$node-${service ?: "kernel"}",
        factory = factory { LogsViewModel(app.talosRepository, node, service) },
    ),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    var filter by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(service ?: "Kernel log")
                        Text(hostname, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
                actions = { IconButton(onClick = vm::refresh) { Icon(Icons.Outlined.Refresh, "Refresh") } },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            OutlinedTextField(
                value = filter,
                onValueChange = { filter = it },
                placeholder = { Text("Filter") },
                leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                singleLine = true,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp),
            )
            when (val s = state) {
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(s.message, vm::refresh)
                is UiState.Loaded -> LogLines(s.data, filter)
            }
        }
    }
}

@Composable
private fun LogLines(tail: LogTail, filter: String) {
    val lines = remember(tail, filter) {
        if (filter.isBlank()) tail.lines else tail.lines.filter { it.contains(filter, ignoreCase = true) }
    }
    val listState = rememberLazyListState()
    // Open at the newest lines, like `tail`.
    LaunchedEffect(lines) { if (lines.isNotEmpty()) listState.scrollToItem(lines.lastIndex) }

    if (lines.isEmpty()) {
        Text(
            if (filter.isBlank()) "No log lines." else "No lines match \"$filter\".",
            modifier = Modifier.padding(16.dp),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        return
    }

    SelectionContainer {
        LazyColumn(
            state = listState,
            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 8.dp),
            modifier = Modifier.fillMaxSize(),
        ) {
            if (tail.truncated && filter.isBlank()) {
                item {
                    Text(
                        "… older lines omitted",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            items(lines) { line ->
                Text(line, fontFamily = FontFamily.Monospace, fontSize = 11.sp, lineHeight = 14.sp)
            }
        }
    }
}
