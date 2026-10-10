package name.levis.ichor.ui.images

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
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
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.compose.ui.platform.LocalContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.imagesKey
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.ImageInfo
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.ImageSort
import name.levis.ichor.model.systemImagesScanId
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.imagescan.ImageScanReportOpen
import name.levis.ichor.model.filteredSorted
import name.levis.ichor.model.shortDigest
import name.levis.ichor.model.totalSize
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.factory
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.formatDate
import name.levis.ichor.ui.components.pageContent

class ImagesViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<List<ImageInfo>>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<List<ImageInfo>>? = talos.cached(imagesKey(node))
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.images(node)
}

/** The node's Talos system images (and their scan), then its CRI namespace's, like `talosctl image list`. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ImagesScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    vm: ImagesViewModel = viewModel(key = "images-$node", factory = factory { ImagesViewModel(app.talosRepository, node) }),
    systemVm: SystemImagesViewModel = viewModel(
        key = "system-images-$node",
        factory = factory { SystemImagesViewModel(app.talosRepository, app.imageScanRepository, node) },
    ),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var query by rememberSaveable { mutableStateOf("") }
    var sort by rememberSaveable { mutableStateOf(ImageSort.NAME) }
    var reportOpen by remember { mutableStateOf<Pair<ImageScanReport, String>?>(null) }
    val system = systemImagesUi(node, systemVm) { report, json -> reportOpen = report to json }
    reportOpen?.let { (report, json) ->
        ImageScanReportOpen(report, json, "$hostname-system", systemVm::export, onDismiss = { reportOpen = null })
    }
    var menuOpen by remember { mutableStateOf(false) }
    var pullAsked by remember { mutableStateOf(false) }
    var pullShown by remember { mutableStateOf(false) }
    val pulls = (LocalContext.current.applicationContext as TalosApp).imagePullManager
    if (pullAsked) {
        ImagePullDialog(
            onPull = { image, namespace ->
                pullAsked = false
                pulls.start(image, namespace)
                pullShown = true // shows the followed pull: this one, or one started before
            },
            onDismiss = { pullAsked = false },
        )
    }
    if (pullShown) ImagePullSheet(pulls, onDismiss = { pullShown = false })

    Scaffold(
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.images_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    Box {
                        TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { menuOpen = true })
                        DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.image_pull_menu)) },
                                onClick = {
                                    menuOpen = false
                                    pullAsked = true
                                },
                            )
                        }
                    }
                },
            )
        },
    ) { padding ->
        Loaded(
            state,
            vm::refresh,
            Modifier.pageContent(padding),
            header = { images ->
                Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    SystemImagesSection(system)
                    SectionTitle(stringResource(R.string.images_kubernetes_title))
                    MutedText(pluralStringResource(R.plurals.images_summary, images.size, images.size, formatBytes(images.totalSize)))
                    SearchField(query, { query = it }, stringResource(R.string.images_search), Modifier.fillMaxWidth())
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(stringResource(R.string.node_processes_sort), style = MaterialTheme.typography.labelMedium)
                        SortChip(ImageSort.NAME, sort, R.string.images_sort_name) { sort = it }
                        SortChip(ImageSort.SIZE, sort, R.string.images_sort_size) { sort = it }
                        SortChip(ImageSort.CREATED, sort, R.string.images_sort_created) { sort = it }
                    }
                }
                HorizontalDivider()
            },
        ) { images ->
            val rows = remember(images, query, sort) { images.filteredSorted(query, sort) }
            if (rows.isEmpty()) {
                EmptyText(emptyOrNoMatch(query, R.string.images_empty, R.string.images_no_match))
            } else {
                LazyColumn(Modifier.fillMaxSize()) {
                    items(rows, key = { "${it.name}|${it.digest}" }) { image ->
                        ImageRow(image)
                        HorizontalDivider()
                    }
                }
            }
        }
    }
}

/** The system images section's state: loaded once, the scan offered to a role with the Kubernetes API. */
@Composable
private fun systemImagesUi(node: String, vm: SystemImagesViewModel, onOpen: (ImageScanReport, String) -> Unit): SystemImagesUi {
    val application = LocalContext.current.applicationContext as TalosApp
    val config by application.configRepository.config.collectAsStateWithLifecycle()
    val state by vm.state.collectAsStateWithLifecycle()
    val session by vm.session.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    val context = config?.activeContext
    val id = systemImagesScanId(node)
    return SystemImagesUi(
        state = state,
        session = session?.takeIf { it.appId == id && it.context == context },
        busyElsewhere = session?.let { it.running && (it.appId != id || it.context != context) } == true,
        canScan = config?.activeSummary?.allows(Feature.WORKLOADS) == true,
        onScan = vm::scan,
        onStop = vm::stop,
        onOpen = onOpen,
    )
}

@Composable
private fun SortChip(value: ImageSort, current: ImageSort, label: Int, onSelect: (ImageSort) -> Unit) {
    FilterChip(selected = current == value, onClick = { onSelect(value) }, label = { Text(stringResource(label)) })
}

@Composable
private fun ImageRow(image: ImageInfo) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                image.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                formatBytes(image.size),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.padding(start = 12.dp),
            )
        }
        val created = if (image.created > 0) formatDate(image.created) else null
        Text(
            listOfNotNull(image.digest.takeIf { it.isNotEmpty() }?.let(::shortDigest), created).joinToString("  ·  "),
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            color = muted,
        )
    }
}
