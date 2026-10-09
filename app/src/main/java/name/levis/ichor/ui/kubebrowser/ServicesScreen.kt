package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.AssistChip
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubeServices
import name.levis.ichor.model.LbController
import name.levis.ichor.model.ServiceRow
import name.levis.ichor.model.filteredServices
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.workloads.KubeFilters
import name.levis.ichor.ui.workloads.NamespacesViewModel
import name.levis.ichor.ui.workloads.rememberKubeScope
import name.levis.ichor.ui.workloads.scopeError

/** The Services of a namespace (null for every one), with their addresses, endpoints and routes. */
class ServicesViewModel(private val browser: KubeBrowserRepository) : LoadingViewModel<KubeServices>() {
    private var namespace: String? = null
    private var started = false

    fun setNamespace(namespace: String?) {
        if (started && namespace == this.namespace) return
        started = true
        this.namespace = namespace
        refresh(reset = true)
    }

    override suspend fun fetch() = browser.services(namespace)
}

/**
 * The Services of the namespace the Kubernetes screens list, problems first: load balancers
 * without an address (with a hint on the pool to check), then Services with no ready
 * endpoint. A tap opens the Service's summary ([onService]); a route opens its URL.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServicesScreen(
    onBack: () -> Unit,
    onService: (KubeObjectRef) -> Unit,
    vm: ServicesViewModel = viewModel(factory = factory { ServicesViewModel(app.kubeBrowser) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val state by vm.state.collectAsStateWithLifecycle()
    val namespaces: NamespacesViewModel = viewModel(factory = factory { NamespacesViewModel(app.kubeRepository) })
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val control = rememberKubeScope(app, namespaces, mask.enabled)
    LaunchedEffect(control.scope, control.ready) { if (control.ready) vm.setNamespace(control.scope.namespace) }
    var query by rememberSaveable { mutableStateOf("") }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.kube_services_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            val loaded = (state as? UiState.Loaded)?.data
            val listed = remember(loaded) { loaded?.services.orEmpty().map { it.namespace }.distinct().sorted() }
            KubeFilters(control, listed, query, { query = it }, R.string.kube_services_search)
            val notes = Modifier.padding(horizontal = 16.dp, vertical = 4.dp)
            if (loaded?.partialAccess == true) MutedText(stringResource(R.string.kube_services_partial), modifier = notes)
            if (loaded?.anyPending == true) MutedText(stringResource(lbHint(loaded.lbController)), modifier = notes)
            HorizontalDivider()
            val rest = Modifier.weight(1f)
            val s = state
            when {
                !control.ready -> EmptyText(stringResource(R.string.kube_scope_type_prompt), rest)
                s is UiState.Failed -> ErrorBox(scopeError(s.message, control.scope), { vm.refresh() }, rest)
                s is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = rest) {
                    val rows = remember(s.data, query) { s.data.services.filteredServices(query) }
                    if (rows.isEmpty()) {
                        EmptyText(emptyOrNoMatch(query, R.string.kube_services_empty, R.string.kube_services_no_match))
                    } else {
                        LazyColumn(Modifier.fillMaxSize()) {
                            items(rows, key = { it.key }) { svc ->
                                ServiceRowItem(
                                    svc,
                                    showNamespace = control.scope.namespace == null,
                                    onClick = { onService(KubeObjectRef.service(svc.namespace, svc.name)) },
                                )
                                HorizontalDivider()
                            }
                        }
                    }
                }
                else -> LoadingBox(rest)
            }
        }
    }
}

/** What to check when a load balancer has no address, by the controller found. */
private fun lbHint(controller: String): Int = when (LbController.of(controller)) {
    LbController.METALLB -> R.string.kube_services_hint_metallb
    LbController.CILIUM -> R.string.kube_services_hint_cilium
    null -> R.string.kube_services_hint_none
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ServiceRowItem(svc: ServiceRow, showNamespace: Boolean, onClick: () -> Unit) {
    val uriHandler = LocalUriHandler.current
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                if (showNamespace) svc.key else svc.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(svc.type, style = MaterialTheme.typography.labelMedium, color = svc.level.color())
        }
        val inside = listOfNotNull(
            stringResource(R.string.kube_services_headless).takeIf { svc.headless } ?: svc.clusterIP.takeIf { it.isNotEmpty() },
            svc.externalName.takeIf { it.isNotEmpty() }?.let { "→ $it" },
            svc.ports.joinToString(", ").takeIf { it.isNotEmpty() },
        )
        if (inside.isNotEmpty()) MutedText(inside.joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        when {
            svc.pending -> Text(
                stringResource(R.string.kube_services_no_address),
                style = MaterialTheme.typography.labelMedium,
                color = svc.level.color(),
            )
            svc.addresses.isNotEmpty() -> Text(
                svc.addresses.joinToString(", "),
                style = MaterialTheme.typography.labelMedium,
                fontFamily = FontFamily.Monospace,
            )
        }
        svc.readyText?.let { MutedText(stringResource(R.string.kube_services_ready, it)) }
        if (svc.routes.isNotEmpty()) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                svc.routes.forEach { route ->
                    AssistChip(
                        onClick = { runCatching { uriHandler.openUri(route.url) } },
                        label = { Text(route.label, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    )
                }
            }
        }
    }
}
