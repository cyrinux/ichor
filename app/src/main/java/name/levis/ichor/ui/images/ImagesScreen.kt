package name.levis.ichor.ui.images

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
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
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.imagesKey
import name.levis.ichor.model.ImageInfo
import name.levis.ichor.model.ImageSort
import name.levis.ichor.model.filteredSorted
import name.levis.ichor.model.shortDigest
import name.levis.ichor.model.totalSize
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.factory
import name.levis.ichor.util.formatBytes
import java.text.DateFormat
import java.util.Date

class ImagesViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<List<ImageInfo>>() {
    override fun cached(): TalosRepository.Timed<List<ImageInfo>>? = talos.cached(imagesKey(node))
    override suspend fun fetch() = talos.images(node)
}

/** Container images in the node's CRI namespace, like `talosctl image list`. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ImagesScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    vm: ImagesViewModel = viewModel(key = "images-$node", factory = factory { ImagesViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var query by rememberSaveable { mutableStateOf("") }
    var sort by rememberSaveable { mutableStateOf(ImageSort.NAME) }

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
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.padding(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.padding(padding))
            is UiState.Loaded -> Column(Modifier.padding(padding).fillMaxSize()) {
                val rows = remember(s.data, query, sort) { s.data.filteredSorted(query, sort) }
                Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(
                        pluralStringResource(R.plurals.images_summary, s.data.size, s.data.size, formatBytes(s.data.totalSize)),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    OutlinedTextField(
                        value = query,
                        onValueChange = { query = it },
                        placeholder = { Text(stringResource(R.string.images_search)) },
                        leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(stringResource(R.string.node_processes_sort), style = MaterialTheme.typography.labelMedium)
                        SortChip(ImageSort.NAME, sort, R.string.images_sort_name) { sort = it }
                        SortChip(ImageSort.SIZE, sort, R.string.images_sort_size) { sort = it }
                        SortChip(ImageSort.CREATED, sort, R.string.images_sort_created) { sort = it }
                    }
                }
                HorizontalDivider()
                PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                    if (rows.isEmpty()) {
                        Text(
                            if (query.isBlank()) stringResource(R.string.images_empty) else stringResource(R.string.images_no_match, query.trim()),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(16.dp),
                        )
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
    }
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
        val created = if (image.created > 0) DateFormat.getDateInstance(DateFormat.MEDIUM).format(Date(image.created)) else null
        Text(
            listOfNotNull(image.digest.takeIf { it.isNotEmpty() }?.let(::shortDigest), created).joinToString("  ·  "),
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            color = muted,
        )
    }
}
