package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import name.levis.ichor.ui.design.IchorAssistChip
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
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
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/** The claims of a namespace (null for every one), with their volume, pods and fill. */
class StorageViewModel(private val browser: KubeBrowserRepository) : NamespaceLoadingViewModel<KubeStorage>() {
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
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()
    var snapshots by remember { mutableStateOf<VolumeSnapshots?>(null) }
    var confirming by remember { mutableStateOf<StorageClaim?>(null) }
    var busy by remember { mutableStateOf(false) }
    var result by remember { mutableStateOf<String?>(null) }
    val actionScope = rememberCoroutineScope()
    val control = rememberListScope(vm)
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
        name.levis.ichor.ui.design.IchorAlertDialog(onDismissRequest = { result = null },
            title = { Text(stringResource(R.string.snapshot_create)) }, text = { Text(it) },
            confirmButton = { name.levis.ichor.ui.design.IchorTextButton(onClick = { result = null }) { Text(stringResource(R.string.common_ok)) } })
    }
    KubeScopedList(
        title = stringResource(R.string.storage_title),
        vm = vm,
        control = control,
        onBack = onBack,
        items = KubeStorage::claims,
        namespaceOf = StorageClaim::namespace,
        filter = { rows, query -> rows.filteredClaims(query) },
        key = StorageClaim::key,
        search = R.string.storage_kube_search,
        empty = R.string.storage_kube_empty,
        noMatch = R.string.storage_kube_no_match,
        above = { if (busy) androidx.compose.material3.LinearProgressIndicator(Modifier.fillMaxWidth()) },
        notes = { loaded ->
            if (loaded?.partialAccess == true) {
                MutedText(stringResource(R.string.storage_kube_partial), modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
            }
        },
    ) { c ->
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
            IchorAssistChip(onClick = { onDataService(kind) }, label = { Text(stringResource(R.string.storage_kube_open, kind.title)) })
        }
        c.managedPlugin?.let { id ->
            IchorAssistChip(onClick = { onPlugin(id) }, label = { Text(stringResource(R.string.storage_kube_open, c.managedName)) })
        }
        if (canSnapshot) {
            IchorAssistChip(onClick = onSnapshot, enabled = denial == null, label = { Text(stringResource(R.string.snapshot_create)) })
            denial?.let { name.levis.ichor.ui.components.KubeDenialNote(it) }
        }
    }
}
