package name.levis.ichor.ui.plugins

import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.PLUGIN_CATALOG
import name.levis.ichor.data.PLUGINS_DETECT
import name.levis.ichor.data.PluginRepository
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.pluginDataKey
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.PluginCard
import name.levis.ichor.model.PluginCatalog
import name.levis.ichor.model.PluginDetect
import name.levis.ichor.model.PluginRef
import name.levis.ichor.model.PluginRow
import name.levis.ichor.model.PluginTree
import name.levis.ichor.model.groupsByNamespace
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.CardListPadding
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.cardGroupTitle
import name.levis.ichor.ui.components.cardRows
import name.levis.ichor.ui.components.groupInOrder
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.dataservices.WarnBanner
import name.levis.ichor.ui.factory
import java.util.Locale

/** The labels' language Go translates the tree into. */
fun pluginLocale(): String = Locale.getDefault().language

/** A plugin's tree and the catalog that names the kinds its rows open. */
data class PluginView(val tree: PluginTree, val catalog: PluginCatalog = PluginCatalog())

/** Which plugins the cluster runs, with their summary tiles; loaded on demand, never polled. */
class PluginDetectViewModel(private val plugins: PluginRepository) : LoadingViewModel<PluginDetect>() {
    private var hints = ""
    private var source: Any? = null

    override fun cached(): TalosRepository.Timed<PluginDetect>? = plugins.cached("$PLUGINS_DETECT|$hints")
    override val restores get() = plugins.restores
    override suspend fun fetch() = plugins.detect(hints)

    /** Loads once per [key] (context, config generation, invalidations); [refresh] forces it. */
    fun load(key: Any, hints: String) {
        if (key == source && hints == this.hints) return
        source = key
        this.hints = hints
        refresh(reset = true)
    }

    /** Nothing to load: the next [load] loads again, even with a key seen before. */
    fun forget() {
        source = null
    }
}

/** One plugin's render tree over every namespace, in the phone's language. */
class PluginTabViewModel(private val plugins: PluginRepository, private val pluginId: String) : LoadingViewModel<PluginView>() {
    private var source: Any? = null

    override fun cached(): TalosRepository.Timed<PluginView>? =
        plugins.cached<PluginTree>(pluginDataKey(pluginId, "", pluginLocale()))?.let { t ->
            TalosRepository.Timed(PluginView(t.value, plugins.cached<PluginCatalog>(PLUGIN_CATALOG)?.value ?: PluginCatalog()), t.at)
        }

    override val restores get() = plugins.restores

    override suspend fun fetch(): PluginView {
        val tree = plugins.data(pluginId, "", pluginLocale())
        // Only the kinds of the objects its rows open: the rows show without it.
        val catalog = cancellableCatching { plugins.catalog() }.getOrDefault(PluginCatalog())
        return PluginView(tree, catalog)
    }

    fun load(key: Any?) {
        if (key == source && state.value !is UiState.Failed) return
        source = key
        refresh()
    }
}

@Composable
private fun pluginTabViewModel(pluginId: String): PluginTabViewModel {
    val app = LocalContext.current.applicationContext as TalosApp
    return viewModel(key = "plugin-$pluginId", factory = factory { PluginTabViewModel(app.pluginRepository, pluginId) })
}

/**
 * A plugin's tab in a hub (Data services): its summary, its cards as grouped cards (by
 * namespace when the rows span several), unreadable parts as a warning banner. Reads again when
 * [reload] changes. [rowExtra] gives a native control to add under a row's details, null for none.
 */
@Composable
fun PluginTab(
    pluginId: String,
    reload: Any?,
    onObject: (KubeObjectRef) -> Unit,
    rowExtra: (PluginRow) -> (@Composable () -> Unit)? = { null },
) {
    val vm = pluginTabViewModel(pluginId)
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(reload) { vm.load(reload) }
    when (val s = state) {
        UiState.Loading -> LoadingBox(Modifier.fillMaxSize())
        is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, Modifier.fillMaxSize())
        is UiState.Loaded -> PluginBody(s.data, onObject, rowExtra)
    }
}

/** A plugin as a screen of its own (a plugin of another category than Data services, from the home). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PluginScreen(pluginId: String, onBack: () -> Unit, onObject: (KubeObjectRef) -> Unit) {
    val vm = pluginTabViewModel(pluginId)
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(pluginId) { vm.load(pluginId) }
    val title = (state as? UiState.Loaded)?.data?.tree?.plugin?.name?.takeIf { it.isNotEmpty() } ?: stringResource(R.string.plugin_screen_title)
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(title) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        val rest = Modifier.pageContent(padding).fillMaxSize()
        when (val s = state) {
            UiState.Loading -> LoadingBox(rest)
            is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, rest)
            is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = rest) {
                PluginBody(s.data, onObject, rowExtra = { null })
            }
        }
    }
}

@Composable
private fun PluginBody(view: PluginView, onObject: (KubeObjectRef) -> Unit, rowExtra: (PluginRow) -> (@Composable () -> Unit)?) {
    val tree = view.tree
    var expanded by rememberSaveable(tree.plugin.id) { mutableStateOf(setOf<String>()) }
    val open: (PluginRef) -> Unit = { ref -> onObject(ref.objectRef(view.catalog.kindOf(tree.plugin.id, ref))) }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = CardListPadding) {
        item(key = "summary") { PluginSummaryHeader(tree.summary, Modifier.padding(horizontal = 20.dp, vertical = 4.dp)) }
        if (tree.errors.isNotEmpty()) item(key = "errors") {
            val lines = tree.errors.map { e -> listOf(e.subject, e.message).filter { it.isNotEmpty() }.joinToString(": ") }
            WarnBanner(
                (listOf(stringResource(R.string.plugin_errors_title)) + lines).joinToString("\n"),
                Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
        if (!tree.active) {
            item(key = "inactive") { EmptyText(stringResource(R.string.plugin_not_detected)) }
            return@LazyColumn
        }
        val shown = tree.cards.filter { it.rows.isNotEmpty() || it.empty.isNotEmpty() }
        if (shown.isEmpty()) item(key = "empty") { EmptyText(stringResource(R.string.plugin_empty)) }
        shown.forEachIndexed { index, card ->
            if (index > 0) item(key = "gap:${card.id}") { Spacer(Modifier.height(12.dp)) }
            pluginCard(card, expanded, { k -> expanded = if (k in expanded) expanded - k else expanded + k }, open, rowExtra)
        }
    }
}

private fun PluginRow.expandKey(card: PluginCard, index: Int): String = "${card.id}|${ref?.key ?: "$index:$title"}"

/** A card's rows as one grouped card, or one per namespace when they span several. */
private fun LazyListScope.pluginCard(
    card: PluginCard,
    expanded: Set<String>,
    onToggle: (String) -> Unit,
    onObject: (PluginRef) -> Unit,
    rowExtra: (PluginRow) -> (@Composable () -> Unit)?,
) {
    if (card.rows.isEmpty()) {
        item(key = "empty:${card.id}") { EmptyText(card.empty) }
        return
    }
    val grouped = card.groupsByNamespace()
    val rows = card.rows.withIndex().toList()
    // All namespaces: one card per namespace, its name as the card's title (keys per plugin card,
    // as several cards can list the same namespaces).
    val groups = if (grouped) groupInOrder(rows) { it.value.ref?.namespace.orEmpty() } else listOf("" to rows)
    groups.forEach { (namespace, group) ->
        if (grouped) cardGroupTitle("group:${card.id}:$namespace", namespace)
        cardRows(group, key = { "row:${card.id}:${it.index}" }) { (index, row) ->
            val key = row.expandKey(card, index)
            PluginRowContent(
                row,
                open = key in expanded,
                onToggle = { onToggle(key) },
                onObject = onObject,
                // The namespace is the card's title when grouped.
                showSubtitle = !grouped || row.subtitle != row.ref?.namespace,
                extra = rowExtra(row),
            )
        }
    }
}
