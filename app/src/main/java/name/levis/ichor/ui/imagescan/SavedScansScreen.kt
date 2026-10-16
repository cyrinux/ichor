package name.levis.ichor.ui.imagescan

import android.widget.Toast
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.DeleteSweep
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.INVENTORY
import name.levis.ichor.data.ImageScanRepository
import name.levis.ichor.model.ImageScanFormat
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.SavedScan
import name.levis.ichor.model.systemImagesNode
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.formatDateTime

/** The scan reports the core keeps on the phone for [cluster], newest first. No cluster call. */
class SavedScansViewModel(private val scans: ImageScanRepository, private val cluster: String) : LoadingViewModel<List<SavedScan>>() {
    override suspend fun fetch() = scans.history(cluster)

    suspend fun open(id: String): Pair<ImageScanReport, String> = scans.saved(id)

    suspend fun export(report: ImageScanReport, json: String, format: ImageScanFormat): String =
        scans.export(json.ifEmpty { scans.encode(report) }, format)

    fun forget(id: String) {
        viewModelScope.launch {
            runCatching { scans.forget(id) }
            refresh()
        }
    }

    fun clear() {
        viewModelScope.launch {
            runCatching { scans.clear(cluster) }
            refresh()
        }
    }
}

/**
 * The vulnerability reports kept on this phone for [cluster] (encrypted, 90 days): app, date,
 * scanner, severities and what was new; one opens over the list, is forgotten, or all are cleared.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SavedScansScreen(
    cluster: String,
    onBack: () -> Unit,
    vm: SavedScansViewModel = viewModel(key = "saved-scans-$cluster", factory = factory { SavedScansViewModel(app.imageScanRepository, cluster) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { vm.refresh() }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var confirmClear by remember { mutableStateOf(false) }
    var open by remember { mutableStateOf<Triple<ImageScanReport, String, String>?>(null) }
    val scans = (state as? UiState.Loaded)?.data.orEmpty()
    // App names from the inventory already loaded: the core keeps only the app id.
    val application = context.applicationContext as TalosApp
    val names = remember { application.talosRepository.cached<Inventory>(INVENTORY)?.value?.apps.orEmpty().associate { it.id to it.name } }
    val nameOf = @Composable { s: SavedScan ->
        systemImagesNode(s.app)?.let { stringResource(R.string.imagescan_saved_system, it) } ?: names[s.app] ?: s.app
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.imagescan_saved_title))
                        if (cluster.isNotEmpty()) {
                            Text(cluster, style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(
                        Icons.Outlined.DeleteSweep,
                        stringResource(R.string.imagescan_saved_clear),
                        enabled = scans.isNotEmpty(),
                        onClick = { confirmClear = true },
                    )
                },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, Modifier.pageContent(padding))
            is UiState.Loaded -> if (s.data.isEmpty()) {
                EmptyText(
                    stringResource(if (cluster.isEmpty()) R.string.imagescan_saved_empty_all else R.string.imagescan_saved_empty),
                    Modifier.pageContent(padding),
                )
            } else {
                LazyColumn(Modifier.pageContent(padding).fillMaxSize()) {
                    items(s.data, key = { it.id }) { scan ->
                        val name = nameOf(scan)
                        SavedScanRow(scan, name, onForget = { vm.forget(scan.id) }) {
                            scope.launch {
                                runCatching { vm.open(scan.id) }
                                    .onSuccess { (report, json) -> open = Triple(report, json, name) }
                                    .onFailure { e ->
                                        val reason = e.uiText().resolve(context)
                                        Toast.makeText(context, context.getString(R.string.imagescan_saved_open_failed, reason), Toast.LENGTH_LONG).show()
                                    }
                            }
                        }
                        HorizontalDivider()
                    }
                }
            }
        }
    }

    open?.let { (report, json, name) ->
        ImageScanReportOpen(report, json, name, vm::export, onDismiss = { open = null })
    }

    if (confirmClear) {
        ConfirmDialog(
            title = stringResource(R.string.imagescan_saved_clear_title),
            text = stringResource(if (cluster.isEmpty()) R.string.imagescan_saved_clear_body_all else R.string.imagescan_saved_clear_body),
            confirm = stringResource(R.string.imagescan_saved_clear),
            destructive = true,
            onConfirm = {
                confirmClear = false
                vm.clear()
            },
            onDismiss = { confirmClear = false },
        )
    }
}

/** The app, when and with what it was scanned, its severities and what was new; [onForget] deletes it. */
@Composable
private fun SavedScanRow(scan: SavedScan, name: String, onForget: () -> Unit, onOpen: () -> Unit) {
    ListItem(
        modifier = Modifier.clickable(onClick = onOpen),
        headlineContent = { Text(name, maxLines = 1, overflow = TextOverflow.Ellipsis) },
        supportingContent = {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(
                    listOf(formatDateTime(scan.finished), scan.scanner).filter { it.isNotEmpty() }.joinToString(" · "),
                    style = MaterialTheme.typography.bodySmall,
                )
                SeverityPills(scan.summary)
                NewFindingsLine(scan.new, hasPrevious = false)
            }
        },
        trailingContent = {
            TooltipIconButton(Icons.Outlined.Delete, stringResource(R.string.imagescan_saved_forget), onClick = onForget)
        },
    )
}
