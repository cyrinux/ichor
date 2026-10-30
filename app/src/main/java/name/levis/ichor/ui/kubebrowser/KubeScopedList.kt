package name.levis.ichor.ui.kubebrowser

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import name.levis.ichor.ui.design.IchorScaffold
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
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.CardListPadding
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.components.groupedCardRows
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.workloads.KubeFilters
import name.levis.ichor.ui.workloads.KubeScopeControl
import name.levis.ichor.ui.workloads.NamespacesViewModel
import name.levis.ichor.ui.workloads.rememberKubeScope
import name.levis.ichor.ui.workloads.scopeError

/** A list read for one [namespace] (null for every one); it loads again only when that changes. */
abstract class NamespaceLoadingViewModel<T> : LoadingViewModel<T>() {
    protected var namespace: String? = null
        private set
    private var started = false

    fun setNamespace(namespace: String?) {
        if (started && namespace == this.namespace) return
        started = true
        this.namespace = namespace
        refresh(reset = true)
    }
}

/** The namespace picker of the Kubernetes screens, with [vm] loading the namespace it shows. */
@Composable
internal fun rememberListScope(vm: NamespaceLoadingViewModel<*>): KubeScopeControl {
    val app = LocalContext.current.applicationContext as TalosApp
    val namespaces: NamespacesViewModel = viewModel(factory = factory { NamespacesViewModel(app.kubeRepository) })
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val control = rememberKubeScope(app, namespaces, mask.enabled)
    LaunchedEffect(control.scope, control.ready) { if (control.ready) vm.setNamespace(control.scope.namespace) }
    return control
}

/**
 * A Kubernetes browser list ([title]) of the namespace [control] picks: the search field and
 * picker on top whatever the state, then a prompt to type a namespace, the error, or the rows
 * of [items] that match the search, one card per namespace when every one is listed, under
 * pull-to-refresh. [above] goes over the search field, [notes] under it (both get the loaded
 * data, null before the first load).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun <T, R> KubeScopedList(
    title: String,
    vm: NamespaceLoadingViewModel<T>,
    control: KubeScopeControl,
    onBack: () -> Unit,
    items: (T) -> List<R>,
    namespaceOf: (R) -> String,
    filter: (List<R>, String) -> List<R>,
    key: (R) -> Any,
    @StringRes search: Int,
    @StringRes empty: Int,
    @StringRes noMatch: Int,
    above: @Composable ColumnScope.() -> Unit = {},
    notes: @Composable ColumnScope.(T?) -> Unit = {},
    row: @Composable (R) -> Unit,
) {
    val state by vm.state.collectAsStateWithLifecycle()
    var query by rememberSaveable { mutableStateOf("") }
    IchorScaffold(
        topBar = {
            TopAppBar(
                title = { Text(title) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            above()
            val loaded = (state as? UiState.Loaded)?.data
            val listed = remember(loaded) { loaded?.let(items).orEmpty().map(namespaceOf).distinct().sorted() }
            KubeFilters(control, listed, query, { query = it }, search)
            notes(loaded)
            HorizontalDivider()
            val rest = Modifier.weight(1f)
            val s = state
            when {
                !control.ready -> EmptyText(stringResource(R.string.kube_scope_type_prompt), rest)
                s is UiState.Failed -> ErrorBox(scopeError(s.message, control.scope), { vm.refresh() }, rest)
                s is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = rest) {
                    val rows = remember(s.data, query) { filter(items(s.data), query) }
                    if (rows.isEmpty()) {
                        EmptyText(emptyOrNoMatch(query, empty, noMatch))
                    } else {
                        // All namespaces: one card per namespace, its name as the card's title.
                        val byNamespace = namespaceOf.takeIf { control.scope.namespace == null }
                        LazyColumn(Modifier.fillMaxSize(), contentPadding = CardListPadding) {
                            groupedCardRows(rows, groupOf = byNamespace, key = key, content = row)
                        }
                    }
                }
                else -> LoadingBox(rest)
            }
        }
    }
}
