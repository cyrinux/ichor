package name.levis.ichor.ui.flux

import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.ShareLinkButton
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.factory

/**
 * Flux's Kustomizations and HelmReleases (first tab) and their sources (second), read through
 * its custom resources with the admin kubeconfig Talos issues (os:admin). Polls while something
 * reconciles; [onApp] opens an app (kind, namespace, name).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FluxScreen(onBack: () -> Unit, onApp: (kind: String, namespace: String, name: String) -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val vm: FluxViewModel = viewModel(factory = factory { FluxViewModel(app.talosRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    val busy by vm.busy.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation, invalidations) { vm.load(Triple(config?.activeContext, generation, invalidations)) }
    FluxPolling(vm)

    val snackbar = remember { SnackbarHostState() }
    FluxActionMessages(vm.results) { snackbar.showSnackbar(it) }
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var confirm by remember { mutableStateOf<FluxConfirm?>(null) }
    val loaded = (state as? UiState.Loaded)?.data

    confirm?.let { c ->
        FluxConfirmDialog(c, onConfirm = { confirm = null; vm.act(c.kind, c.namespace, c.name, c.action) }, onDismiss = { confirm = null })
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.flux_title))
                        loaded?.let { Subtitle(it) }
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    ShareLinkButton(ShareTarget.screen(ShareTarget.FLUX))
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() })
                },
            )
        },
    ) { padding ->
        val modifier = Modifier.padding(padding)
        when (val s = state) {
            UiState.Loading -> LoadingBox(modifier)
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
            is UiState.Loaded -> Column(modifier.fillMaxSize()) {
                if (!s.data.installed && s.data.apps.isEmpty()) {
                    EmptyText(stringResource(R.string.flux_not_installed))
                    return@Column
                }
                PrimaryTabRow(selectedTabIndex = tab) {
                    Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.flux_tab_apps)) })
                    Tab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.flux_tab_sources)) })
                }
                PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                    if (tab == 0) {
                        FluxAppsTab(s.data, busy, onOpen = { onApp(it.kind, it.namespace, it.name) })
                    } else {
                        FluxSourcesTab(s.data, busy, onAct = { src, action -> confirm = FluxConfirm(action, src.kind, src.namespace, src.name) })
                    }
                }
                DataFreshness(s, edgeToEdge = false)
            }
        }
    }
}

/** "v2.7.0 · 8 apps". */
@Composable
private fun Subtitle(status: FluxStatus) {
    Text(
        listOf(status.version, pluralStringResource(R.plurals.argo_apps, status.apps.size, status.apps.size)).filter { it.isNotEmpty() }.joinToString(" · "),
        style = MaterialTheme.typography.labelMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
    )
}
