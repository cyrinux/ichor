package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.LonghornAction
import name.levis.ichor.model.LonghornNode
import name.levis.ichor.model.LonghornStatus
import name.levis.ichor.model.LonghornVolume
import name.levis.ichor.model.VolumeFilter
import name.levis.ichor.model.defaultEngineVersion
import name.levis.ichor.model.filtered
import name.levis.ichor.model.longhornActionKey
import name.levis.ichor.model.oldEngineVolumes
import name.levis.ichor.model.replicaChoices
import name.levis.ichor.model.sharedDiskPath
import name.levis.ichor.ui.components.CardListPadding
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.cardGroupTitle
import name.levis.ichor.ui.components.cardRows

/**
 * Longhorn volumes (problems first, matching the top bar's [query]), the backup targets and
 * each node's disks. Every action sits in a row's ⋮ menu and confirms in its own dialog or sheet.
 */
@Composable
fun LonghornTab(status: LonghornStatus, actions: LonghornActions, garageDetected: Boolean, onGarage: () -> Unit, query: String = "") {
    val hasProblems = remember(status) { status.volumes.any { it.serviceHealth.needsAttention } }
    var filter by rememberSaveable { mutableStateOf(if (hasProblems) VolumeFilter.PROBLEMS else VolumeFilter.ALL) }
    val rows = remember(status, filter, query) { status.volumes.filtered(filter, query) }
    val sharedPath = remember(status) { status.nodes.sharedDiskPath() }
    val busy by actions.busy.collectAsStateWithLifecycle()
    val attachmentBusy by actions.attachmentBusy.collectAsStateWithLifecycle()
    var attachmentTarget by remember { mutableStateOf<Pair<LonghornVolume, Boolean>?>(null) }
    attachmentTarget?.let { (v, attach) -> LonghornAttachmentDialog(v, attach, actions,
        onDismiss = { attachmentTarget = null }, onConfirm = { node -> actions.attachment(v, node, attach); attachmentTarget = null }) }

    val engineBusy by actions.engineBusy.collectAsStateWithLifecycle()
    var engineTarget by remember { mutableStateOf<LonghornVolume?>(null) }
    engineTarget?.let { v -> LonghornEngineDialog(v, actions,
        onDismiss = { engineTarget = null }, onConfirm = { image -> actions.engineUpgrade(v, image); engineTarget = null }) }
    val defaultEngineVersion = remember(status) { status.defaultEngineVersion }
    val oldEngines = remember(status) { status.oldEngineVolumes }

    val salvageBusy by actions.salvageBusy.collectAsStateWithLifecycle()
    var salvageTarget by remember { mutableStateOf<LonghornVolume?>(null) }
    salvageTarget?.let { v -> LonghornSalvageDialog(v, actions, onDismiss = { salvageTarget = null }, onConfirm = { replicas -> actions.salvage(v, replicas); salvageTarget = null }) }

    // By name: the sheet follows the volume as the status refreshes.
    var snapshotsOf by rememberSaveable { mutableStateOf<String?>(null) }
    snapshotsOf?.let { name ->
        status.volumes.firstOrNull { it.name == name }?.let { v -> LonghornSnapshotsSheet(v, actions, onDismiss = { snapshotsOf = null }) }
    }

    var replicasOf by remember { mutableStateOf<LonghornVolume?>(null) }
    var evicting by remember { mutableStateOf<LonghornNode?>(null) }

    replicasOf?.let { v ->
        ReplicaCountDialog(
            label = v.label,
            current = v.replicasDesired,
            choices = status.replicaChoices(v),
            onConfirm = { count ->
                replicasOf = null
                actions.run(v.key, v.namespace, v.name, v.label, LonghornAction.REPLICAS, count)
            },
            onDismiss = { replicasOf = null },
        )
    }
    evicting?.let { n ->
        EvictConfirmDialog(
            node = n.name,
            onConfirm = {
                evicting = null
                actions.run(n.key, n.namespace, n.name, n.name, LonghornAction.EVICT)
            },
            onDismiss = { evicting = null },
        )
    }
    val onVolume = { v: LonghornVolume, action: LonghornAction ->
        if (action == LonghornAction.REPLICAS) replicasOf = v else actions.run(v.key, v.namespace, v.name, v.label, action)
    }
    val onNode = { n: LonghornNode, action: LonghornAction ->
        if (action == LonghornAction.EVICT) evicting = n else actions.run(n.key, n.namespace, n.name, n.name, action)
    }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = CardListPadding) {
        if (status.error.isNotEmpty()) item {
            InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp))
        }
        if (oldEngines > 0) item(key = "old-engines") {
            MutedText(stringResource(R.string.longhorn_engine_old_count, oldEngines.toString()), Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
        }
        cardRows(status.backupTargets, key = { "bt:${it.name}|${it.url}" }) { BackupTargetRow(it, garageDetected, onGarage) }
        item(key = "filters") { VolumeFilterRow(filter, onSelect = { filter = it }) }
        if (rows.isEmpty()) item(key = "empty") {
            EmptyText(
                when {
                    query.isNotBlank() -> stringResource(R.string.data_services_no_match, query.trim())
                    filter == VolumeFilter.PROBLEMS -> stringResource(R.string.data_services_all_fine)
                    else -> stringResource(R.string.longhorn_empty)
                },
            )
        }
        cardRows(rows, key = { "v:${it.name}" }) { v ->
            VolumeRow(
                v, v.key in busy || v.key in salvageBusy || v.key in attachmentBusy || v.key in engineBusy,
                defaultEngineVersion = defaultEngineVersion,
                onAttachment = { attachmentTarget = v to it }, onSalvage = { salvageTarget = v }, onEngine = { engineTarget = v },
                onSnapshots = { snapshotsOf = v.name }, onAction = { onVolume(v, it) },
            )
        }
        if (status.nodes.isNotEmpty()) item(key = "nodes", contentType = "cardGroupTitle") {
            Column(Modifier.padding(start = 20.dp, end = 20.dp, top = 8.dp)) {
                SectionTitle(stringResource(R.string.longhorn_nodes))
                // The mount every node shares, once rather than on each row.
                sharedPath?.let { MutedText(it) }
            }
        }
        cardRows(status.nodes, key = { "n:${it.name}" }) { n -> LonghornNodeRow(n, n.key in busy, showPath = sharedPath == null) { onNode(n, it) } }
    }
}

private val LonghornVolume.key get() = longhornActionKey(namespace, name)
private val LonghornNode.key get() = longhornActionKey(namespace, "node/$name")

/** Problems, all or detached volumes, as one segmented row. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun VolumeFilterRow(selected: VolumeFilter, onSelect: (VolumeFilter) -> Unit) {
    val options = listOf(
        VolumeFilter.PROBLEMS to stringResource(R.string.data_services_filter_problems),
        VolumeFilter.ALL to stringResource(R.string.data_services_filter_all),
        VolumeFilter.DETACHED to stringResource(R.string.longhorn_filter_detached),
    )
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        options.forEachIndexed { i, (value, label) ->
            SegmentedButton(
                selected = selected == value,
                onClick = { onSelect(value) },
                shape = SegmentedButtonDefaults.itemShape(i, options.size),
            ) { Text(label, maxLines = 1) }
        }
    }
}

/** Search field and filter chips, shared by the Postgres tab. */
@Composable
internal fun <T> Filters(query: String, onQuery: (String) -> Unit, chips: List<Pair<T, String>>, selected: T, onSelect: (T) -> Unit) {
    Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        SearchField(query, onQuery, stringResource(R.string.data_services_search), Modifier.fillMaxWidth().padding(horizontal = 16.dp))
        LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(chips, key = { it.first.toString() }) { (value, label) ->
                FilterChip(selected = selected == value, onClick = { onSelect(value) }, label = { Text(label) })
            }
        }
    }
}
