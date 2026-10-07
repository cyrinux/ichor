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
import androidx.compose.material3.Surface
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
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.resourceTypesKey
import name.levis.ichor.model.ResourceType
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.resourceTypeGroups
import name.levis.ichor.model.sensitive
import name.levis.ichor.model.support
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.FeatureGate
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.components.rememberNodeFeatures
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.components.pageContent

class ResourceTypesViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<List<ResourceType>>() {
    override fun cached(): TalosRepository.Timed<List<ResourceType>>? = talos.cached(resourceTypesKey(node))
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.resourceTypes(node)
}

/** `talosctl get rd`: every resource type of the node, searchable, grouped by namespace. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ResourceTypesScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    onType: (ResourceType) -> Unit,
    vm: ResourceTypesViewModel = viewModel(key = "resourcetypes-$node", factory = factory { ResourceTypesViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val support = rememberNodeFeatures(node).support(TalosFeature.RESOURCE_BROWSER)
    var query by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(support.supported) { if (support.supported && state == UiState.Loading) vm.refresh() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.resources_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        FeatureGate(support, Modifier.pageContent(padding)) {
            Column(Modifier.pageContent(padding).fillMaxSize()) {
                SearchField(query, { query = it }, stringResource(R.string.resources_search_types), Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp))
                Loaded(state, vm::refresh) { TypeList(it, query, onType) }
            }
        }
    }
}

@Composable
private fun TypeList(types: List<ResourceType>, query: String, onType: (ResourceType) -> Unit) {
    val groups = remember(types, query) { resourceTypeGroups(types, query) }
    LazyColumn(Modifier.fillMaxSize()) {
        if (groups.isEmpty()) {
            item {
                EmptyText(emptyOrNoMatch(query, R.string.resources_types_empty, R.string.resources_no_match))
            }
        }
        groups.forEach { group ->
            item(key = "ns|${group.namespace}") {
                Surface(color = MaterialTheme.colorScheme.surfaceContainer, modifier = Modifier.fillMaxWidth()) {
                    Text(
                        group.namespace.ifEmpty { "—" },
                        style = MaterialTheme.typography.titleSmall,
                        fontFamily = FontFamily.Monospace,
                        modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                    )
                }
            }
            items(group.types, key = { "t|${it.namespace}|${it.type}" }) { type ->
                TypeRow(type, onClick = { onType(type) })
                HorizontalDivider()
            }
        }
    }
}

@Composable
private fun TypeRow(type: ResourceType, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(role = Role.Button, onClickLabel = stringResource(R.string.common_open), onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(type.type, style = MaterialTheme.typography.bodyMedium)
            if (type.aliases.isNotEmpty()) {
                Text(
                    type.aliases.joinToString(", "),
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        if (type.sensitive) {
            Icon(
                Icons.Outlined.Lock,
                contentDescription = stringResource(R.string.resources_sensitive),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(18.dp),
            )
        }
    }
}
