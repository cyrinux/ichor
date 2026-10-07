package name.levis.ichor.ui.resources

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ResourceItem
import name.levis.ichor.model.ResourceList
import name.levis.ichor.model.matching
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.factory
import name.levis.ichor.util.formatDateTime
import name.levis.ichor.ui.components.pageContent

/** A resource type of a node, as addressed by the list and detail screens. */
data class ResourceRef(val node: String, val namespace: String, val type: String, val sensitive: Boolean)

class ResourceListViewModel(private val talos: TalosRepository, private val ref: ResourceRef) : LoadingViewModel<ResourceList>() {
    override suspend fun fetch() = talos.resourceList(ref.node, ref.namespace, ref.type)
}

/** `talosctl get TYPE`: the resources of one type. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ResourceListScreen(
    ref: ResourceRef,
    hostname: String,
    onBack: () -> Unit,
    onItem: (ResourceItem) -> Unit,
    vm: ResourceListViewModel = viewModel(
        key = "resourcelist-${ref.node}-${ref.namespace}-${ref.type}",
        factory = factory { ResourceListViewModel(app.talosRepository, ref) },
    ),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    var query by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            if (ref.sensitive) {
                                Icon(
                                    Icons.Outlined.Lock,
                                    contentDescription = stringResource(R.string.resources_sensitive),
                                    modifier = Modifier.padding(end = 6.dp).size(18.dp),
                                )
                            }
                            Text(ref.type, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                        Text(
                            listOf(ref.namespace, hostname).filter { it.isNotEmpty() }.joinToString(" · "),
                            style = MaterialTheme.typography.labelMedium,
                            fontFamily = FontFamily.Monospace,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            SearchField(query, { query = it }, stringResource(R.string.resources_search_items), Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp))
            Loaded(state, vm::refresh) { ItemList(it, query, onItem) }
        }
    }
}

@Composable
private fun ItemList(list: ResourceList, query: String, onItem: (ResourceItem) -> Unit) {
    val items = remember(list, query) { list.items.matching(query) }
    LazyColumn(Modifier.fillMaxSize()) {
        if (list.truncated) {
            item { InfoNotice(stringResource(R.string.resources_truncated, list.items.size), Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) }
        }
        if (items.isEmpty()) {
            item {
                EmptyText(emptyOrNoMatch(query, R.string.resources_items_empty, R.string.resources_no_match))
            }
        }
        items(items, key = { "${it.namespace}|${it.id}" }) { item ->
            ItemRow(item, onClick = { onItem(item) })
            HorizontalDivider()
        }
    }
}

@Composable
private fun ItemRow(item: ResourceItem, onClick: () -> Unit) {
    val updated = remember(item.updated) {
        item.updated.takeIf { it > 0 }?.let { formatDateTime(it) }
    }
    Column(Modifier.fillMaxWidth().clickable(role = Role.Button, onClickLabel = stringResource(R.string.common_open), onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp)) {
        Text(item.id, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
        val facts = listOfNotNull(
            item.phase.takeIf { it.isNotBlank() },
            item.version.takeIf { it.isNotBlank() }?.let { stringResource(R.string.resources_version, it) },
            updated,
        )
        if (facts.isNotEmpty()) {
            MutedText(facts.joinToString(" · "))
        }
    }
}
