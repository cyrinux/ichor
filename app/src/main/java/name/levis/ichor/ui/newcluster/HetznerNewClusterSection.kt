package name.levis.ichor.ui.newcluster

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.JoinRequest
import name.levis.ichor.model.HetznerNewClusterCost
import name.levis.ichor.model.HetznerNewClusterSpec
import name.levis.ichor.model.HetznerOptions
import name.levis.ichor.model.snapshotsFor
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.join.HetznerCreateViewModel
import name.levis.ichor.ui.join.HetznerTokenDialog
import name.levis.ichor.ui.join.rememberAuthenticated
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** A cluster name the Go core takes: letters, digits and dashes. */
private val clusterName = Regex("^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$", RegexOption.IGNORE_CASE)

/**
 * "A server at Hetzner": a project's token, a server type, a location and the project's Talos
 * snapshot become a one-node cluster on a server and a Primary IP. The cost of both and the typed
 * name come before anything is billed; the run then stores the talosconfig as it hands it over.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun HetznerNewClusterSection(
    demo: Boolean,
    vm: HetznerCreateViewModel = viewModel(
        key = "hetzner-new-cluster",
        factory = factory { HetznerCreateViewModel(app.nodeLifecycleRepository, app.cloudProviderStore, newCluster = true) },
    ),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val projects by vm.projects.collectAsStateWithLifecycle()
    val project by vm.project.collectAsStateWithLifecycle()
    val options by vm.options.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var addingToken by remember { mutableStateOf(false) }
    LaunchedEffect(demo) { vm.load(demo) }

    MutedText(stringResource(R.string.new_cluster_hetzner_intro))
    if (demo) {
        InfoNotice(stringResource(R.string.hetzner_demo))
    } else {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            projects.forEach { name ->
                IchorFilterChip(selected = name == project, onClick = { vm.pick(name) }, label = { Text(name) })
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            IchorOutlinedButton(onClick = { addingToken = true }) { Text(stringResource(R.string.hetzner_add_token)) }
            if (project.isNotBlank()) {
                IchorTextButton(onClick = { vm.remove(project) }) { Text(stringResource(R.string.hetzner_token_remove)) }
            }
        }
    }
    when (val state = options) {
        null -> Unit
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(state.message, vm::reload)
        is UiState.Loaded -> ClusterForm(state.data, vm) { request ->
            // The token is read only now, and goes to the run alone.
            scope.launch { app.joinManager.start(request, if (project.isBlank()) "" else vm.token(project)) }
        }
    }
    if (addingToken) HetznerTokenDialog(vm) { addingToken = false }
}

/** The name, the pickers, the cost line and the confirmed Create. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ClusterForm(options: HetznerOptions, vm: HetznerCreateViewModel, onCreate: (JoinRequest) -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val run by app.joinManager.current.collectAsStateWithLifecycle()
    val authenticated = rememberAuthenticated()
    var name by rememberSaveable { mutableStateOf("") }
    var typeName by rememberSaveable { mutableStateOf("") }
    var locationName by rememberSaveable { mutableStateOf("") }
    var snapshotId by rememberSaveable { mutableStateOf(0L) }
    var confirm by remember { mutableStateOf(false) }
    var cost by remember { mutableStateOf<UiState<HetznerNewClusterCost>?>(null) }
    var attempt by remember { mutableStateOf(0) }

    val type = options.serverTypes.firstOrNull { it.name == typeName }
    val snapshots = options.snapshotsFor(type).filter { it.talos }
    val snapshot = snapshots.firstOrNull { it.id == snapshotId }
    val taken = config?.summary?.contexts?.any { it.name == name } == true
    LaunchedEffect(typeName, locationName, attempt) {
        cost = null
        if (type != null && type.price(locationName) != null) cost = uiStateOf { vm.clusterEstimate(typeName, locationName) }
    }

    SectionTitle(stringResource(R.string.new_cluster_step_cluster))
    OutlinedTextField(
        value = name,
        onValueChange = { name = it.trim() },
        label = { Text(stringResource(R.string.new_cluster_name)) },
        isError = taken,
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
    if (taken) Text(stringResource(R.string.new_cluster_name_taken, name), color = LocalStatusColors.current.bad)
    Text(stringResource(R.string.hetzner_server_type), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.serverTypes.forEach { t ->
            IchorFilterChip(
                selected = t.name == typeName,
                onClick = { typeName = t.name },
                label = { Text("${t.name} · ${t.cores} CPU · ${t.memoryGB.toInt()} GB · ${t.arch}") },
            )
        }
    }
    Text(stringResource(R.string.hetzner_location), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.locations.filter { type == null || type.price(it.name) != null }.forEach { l ->
            IchorFilterChip(selected = l.name == locationName, onClick = { locationName = l.name }, label = { Text("${l.name} · ${l.city}") })
        }
    }
    Text(stringResource(R.string.hetzner_snapshot), style = MaterialTheme.typography.labelLarge)
    if (type != null && snapshots.isEmpty()) InfoNotice(stringResource(R.string.new_cluster_hetzner_no_snapshot, type.arch))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        snapshots.forEach { s ->
            IchorFilterChip(selected = s.id == snapshotId, onClick = { snapshotId = s.id }, label = { Text(s.description.ifBlank { s.id.toString() }) })
        }
    }
    val price = (cost as? UiState.Loaded)?.data
    when (val state = cost) {
        null -> Unit
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(state.message) { attempt++ }
        is UiState.Loaded -> Text(
            stringResource(
                R.string.new_cluster_hetzner_cost,
                "${state.data.totalMonthly} ${state.data.currency}",
                "${state.data.serverMonthly} ${state.data.currency}",
                "${state.data.primaryIPMonthly} ${state.data.currency}",
            ),
            style = MaterialTheme.typography.titleSmall,
            color = LocalStatusColors.current.warn,
        )
    }
    IchorButton(
        onClick = { confirm = true },
        enabled = run?.running != true && clusterName.matches(name) && !taken && snapshot != null && price != null,
        modifier = Modifier.fillMaxWidth(),
    ) { Text(stringResource(R.string.new_cluster_create)) }

    if (confirm && type != null && snapshot != null && price != null) {
        HostnameConfirmDialog(
            title = stringResource(R.string.new_cluster_confirm_title, name),
            hostname = name,
            confirmLabel = stringResource(R.string.new_cluster_create),
            onConfirm = {
                confirm = false
                val spec = HetznerNewClusterSpec(name = name, serverType = type.name, location = locationName, snapshotId = snapshot.id)
                authenticated(name) {
                    onCreate(
                        JoinRequest(
                            address = "", template = "", role = "controlplane", hostname = name, disk = "", staticAddress = "",
                            hetznerCluster = spec,
                        ),
                    )
                }
            },
            onDismiss = { confirm = false },
            emphasized = true,
        ) {
            Text(
                stringResource(R.string.new_cluster_hetzner_confirm_body, type.name, locationName, "${price.totalMonthly} ${price.currency}", name),
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
}
