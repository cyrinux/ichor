package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
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
import androidx.compose.runtime.rememberCoroutineScope
import kotlinx.coroutines.launch
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.VolumeSnapshots
import name.levis.ichor.ui.components.ConfirmDialog
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubeStorage
import name.levis.ichor.model.StorageClaim
import name.levis.ichor.model.StorageLevel
import name.levis.ichor.model.filteredClaims
import name.levis.ichor.model.title
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.CardListPadding
import name.levis.ichor.ui.components.groupedCardRows
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.workloads.KubeFilters
import name.levis.ichor.ui.workloads.NamespacesViewModel
import name.levis.ichor.ui.workloads.rememberKubeScope
import name.levis.ichor.ui.workloads.scopeError
import name.levis.ichor.util.formatBytes

/** The claims of a namespace (null for every one), with their volume, pods and fill. */
class StorageViewModel(private val browser: KubeBrowserRepository) : LoadingViewModel<KubeStorage>() {
    private var namespace: String? = null
    private var started = false

    fun setNamespace(namespace: String?) {
        if (started && namespace == this.namespace) return
        started = true
        this.namespace = namespace
        refresh(reset = true)
    }

    override suspend fun fetch() = browser.storage(namespace)
}

/**
 * The PersistentVolumeClaims of the namespace the Kubernetes screens list, problems first:
 * lost and nearly full ones, then pending, terminating and filling ones. A tap opens the
 * claim's summary ([onClaim]); a claim of Longhorn or CloudNativePG links to that data service
 * ([onDataService]), one of Rook Ceph to its plugin ([onPlugin]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun StorageScreen(
    onBack: () -> Unit,
    onClaim: (KubeObjectRef) -> Unit,
    onDataService: (DataServiceKind) -> Unit,
    onPlugin: (String) -> Unit,
    vm: StorageViewModel = viewModel(factory = factory { StorageViewModel(app.kubeBrowser) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val state by vm.state.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()
    var snapshots by remember { mutableStateOf<VolumeSnapshots?>(null) }
    var confirming by remember { mutableStateOf<StorageClaim?>(null) }
    var busy by remember { mutableStateOf(false) }
    var result by remember { mutableStateOf<String?>(null) }
    val actionScope = rememberCoroutineScope()
    val namespaces: NamespacesViewModel = viewModel(factory = factory { NamespacesViewModel(app.kubeRepository) })
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val control = rememberKubeScope(app, namespaces, mask.enabled)
    LaunchedEffect(control.scope, control.ready) { if (control.ready) vm.setNamespace(control.scope.namespace) }
    LaunchedEffect(control.scope, control.ready, generation, config?.activeContext) {
        snapshots = null
        confirming = null
        if (control.ready) snapshots = runCatching { app.dataServicesRepository.volumeSnapshots(control.scope.namespace.orEmpty()) }.getOrNull()
    }
    confirming?.let { claim ->
        ConfirmDialog(stringResource(R.string.snapshot_create), stringResource(R.string.snapshot_confirm, claim.key),
            stringResource(R.string.snapshot_create), onConfirm = {
                confirming = null
                busy = true
                actionScope.launch {
                    result = runCatching { app.dataServicesRepository.createVolumeSnapshot(claim.namespace, claim.name) }
                        .fold({ it }, { it.localizedMessage.orEmpty() })
                    busy = false
                    vm.refresh()
                }
            }, onDismiss = { confirming = null })
    }
    result?.let {
        androidx.compose.material3.AlertDialog(onDismissRequest = { result = null },
            title = { Text(stringResource(R.string.snapshot_create)) }, text = { Text(it) },
            confirmButton = { androidx.compose.material3.TextButton(onClick = { result = null }) { Text(stringResource(R.string.common_ok)) } })
    }
    var query by rememberSaveable { mutableStateOf("") }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.storage_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            if (busy) androidx.compose.material3.LinearProgressIndicator(Modifier.fillMaxWidth())
            val loaded = (state as? UiState.Loaded)?.data
            val listed = remember(loaded) { loaded?.claims.orEmpty().map { it.namespace }.distinct().sorted() }
            KubeFilters(control, listed, query, { query = it }, R.string.storage_kube_search)
            if (loaded?.partialAccess == true) {
                MutedText(stringResource(R.string.storage_kube_partial), modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
            }
            HorizontalDivider()
            val rest = Modifier.weight(1f)
            val s = state
            when {
                !control.ready -> EmptyText(stringResource(R.string.kube_scope_type_prompt), rest)
                s is UiState.Failed -> ErrorBox(scopeError(s.message, control.scope), { vm.refresh() }, rest)
                s is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = rest) {
                    val rows = remember(s.data, query) { s.data.claims.filteredClaims(query) }
                    if (rows.isEmpty()) {
                        EmptyText(emptyOrNoMatch(query, R.string.storage_kube_empty, R.string.storage_kube_no_match))
                    } else {
                        // All namespaces: one card per namespace, its name as the card's title.
                        val byNamespace = if (control.scope.namespace == null) { r: StorageClaim -> r.namespace } else null
                        LazyColumn(Modifier.fillMaxSize(), contentPadding = CardListPadding) {
                            groupedCardRows(rows, groupOf = byNamespace, key = { it.key }) { c ->
                                ClaimRow(
                                    c,
                                    onClick = { onClaim(KubeObjectRef.pvc(c.namespace, c.name)) },
                                    onDataService = onDataService,
                                    onPlugin = onPlugin,
                                    canSnapshot = !busy && config?.activeSummary?.isDemo == false && snapshots?.canCreate(c) == true,
                                    onSnapshot = { confirming = c },
                                )
                            }
                        }
                    }
                }
                else -> LoadingBox(rest)
            }
        }
    }
}

/** The colour of a row's status on the Kubernetes list screens (Storage, Services, Jobs), by its level. */
@Composable
internal fun StorageLevel.color(): Color = when (this) {
    StorageLevel.CRITICAL -> LocalStatusColors.current.bad
    StorageLevel.WARNING -> LocalStatusColors.current.warn
    StorageLevel.OK -> MaterialTheme.colorScheme.onSurfaceVariant
}

@Composable
private fun ClaimRow(
    c: StorageClaim,
    onClick: () -> Unit,
    onDataService: (DataServiceKind) -> Unit,
    onPlugin: (String) -> Unit,
    canSnapshot: Boolean,
    onSnapshot: () -> Unit,
) {
    val denial = name.levis.ichor.ui.components.rememberKubeDenial(name.levis.ichor.model.KubeAction.CREATE_VOLUME_SNAPSHOT, c.namespace)
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                c.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                if (c.terminating) "Terminating" else c.phase,
                style = MaterialTheme.typography.labelMedium,
                color = c.level.color(),
            )
        }
        if (c.measured) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                UsageBar(c.usedFraction, Modifier.weight(1f), warnAt = 0.85f)
                Text(
                    "${formatBytes(c.used.toLong())} / ${formatBytes(c.capacity.toLong())}",
                    style = MaterialTheme.typography.labelSmall,
                    color = muted,
                )
            }
        } else {
            MutedText(
                listOfNotNull(
                    formatBytes(c.capacity.toLong()).takeIf { c.capacity > 0 },
                    stringResource(R.string.storage_kube_not_measured),
                ).joinToString("  ·  "),
            )
        }
        val details = listOf(c.storageClass, c.volume, c.accessModes.joinToString(",")).filter { it.isNotEmpty() }
        if (details.isNotEmpty()) MutedText(details.joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (c.pods.isNotEmpty()) {
            MutedText(stringResource(R.string.storage_kube_pods, c.pods.joinToString(", ")), maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        c.managedKind?.let { kind ->
            AssistChip(onClick = { onDataService(kind) }, label = { Text(stringResource(R.string.storage_kube_open, kind.title)) })
        }
        c.managedPlugin?.let { id ->
            AssistChip(onClick = { onPlugin(id) }, label = { Text(stringResource(R.string.storage_kube_open, c.managedName)) })
        }
        if (canSnapshot) {
            AssistChip(onClick = onSnapshot, enabled = denial == null, label = { Text(stringResource(R.string.snapshot_create)) })
            denial?.let { name.levis.ichor.ui.components.KubeDenialNote(it) }
        }
    }
}
