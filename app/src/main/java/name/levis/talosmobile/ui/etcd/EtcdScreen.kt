package name.levis.talosmobile.ui.etcd

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.data.ETCD
import name.levis.talosmobile.model.EtcdMember
import name.levis.talosmobile.model.EtcdNodeStatus
import name.levis.talosmobile.model.EtcdOverview
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.ErrorBox
import name.levis.talosmobile.ui.components.InfoRow
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.components.SectionTitle
import name.levis.talosmobile.ui.components.StatusPill
import name.levis.talosmobile.ui.components.UsageBar
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.util.formatBytes
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.TextButton
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.data.activeSummary
import name.levis.talosmobile.model.Feature
import name.levis.talosmobile.model.allows
import name.levis.talosmobile.model.defragOrder
import name.levis.talosmobile.model.reclaimable
import name.levis.talosmobile.security.AuthResult
import name.levis.talosmobile.security.authenticate
import name.levis.talosmobile.security.findFragmentActivity
import name.levis.talosmobile.ui.userMessage

sealed interface DefragState {
    data object Idle : DefragState
    data class Running(val hostname: String, val index: Int, val total: Int) : DefragState
    data class Done(val message: String) : DefragState
    data class Failed(val message: String) : DefragState
}

class EtcdViewModel(private val talos: TalosRepository) : LoadingViewModel<EtcdOverview>() {
    override fun cached(): EtcdOverview? = talos.cached(ETCD)
    override suspend fun fetch() = talos.etcd()

    private val _defrag = MutableStateFlow<DefragState>(DefragState.Idle)
    val defrag: StateFlow<DefragState> = _defrag.asStateFlow()

    /** Defragments [targets] strictly one after another; stops at the first failure. */
    fun defragment(targets: List<EtcdNodeStatus>, hostnames: Map<String, String>) {
        if (_defrag.value is DefragState.Running || targets.isEmpty()) return
        viewModelScope.launch {
            targets.forEachIndexed { i, member ->
                val name = hostnames[member.memberId] ?: member.node
                _defrag.value = DefragState.Running(name, i + 1, targets.size)
                val result = runCatching { talos.etcdDefragment(member.node) }
                refresh()
                result.exceptionOrNull()?.let {
                    _defrag.value = DefragState.Failed("$name: ${it.userMessage()}")
                    return@launch
                }
            }
            val reclaimed = targets.sumOf { it.reclaimable }
            _defrag.value = DefragState.Done(
                "Defragmented ${targets.size} member(s), about ${formatBytes(reclaimed)} reclaimed",
            )
        }
    }

    fun dismissDefrag() {
        _defrag.value = DefragState.Idle
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EtcdScreen(
    onBack: () -> Unit,
    vm: EtcdViewModel = viewModel(factory = factory { EtcdViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val defrag by vm.defrag.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val canDefrag = config?.activeSummary?.allows(Feature.ETCD_DEFRAG) ?: false
    val scope = rememberCoroutineScope()
    var confirm by remember { mutableStateOf<DefragRequest?>(null) }

    // With the app lock on, defragmentation needs a fresh fingerprint/PIN, like reboot.
    fun confirmed(request: DefragRequest) {
        confirm = null
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            vm.defragment(request.targets, request.hostnames)
            return
        }
        scope.launch {
            if (authenticate(activity, "Defragment etcd") is AuthResult.Success) {
                vm.defragment(request.targets, request.hostnames)
            }
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("etcd") },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.padding(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.padding(padding))
            is UiState.Loaded -> PullToRefreshBox(
                isRefreshing = s.refreshing,
                onRefresh = vm::refresh,
                modifier = Modifier.padding(padding).fillMaxSize(),
            ) {
                EtcdContent(
                    etcd = s.data,
                    defrag = defrag,
                    canDefrag = canDefrag,
                    onDefrag = { confirm = it },
                    onDismissDefrag = vm::dismissDefrag,
                )
            }
        }
    }

    confirm?.let { request ->
        DefragConfirmDialog(request, onConfirm = { confirmed(request) }, onDismiss = { confirm = null })
    }
}

/** What the confirmation dialog is about to defragment. */
data class DefragRequest(val targets: List<EtcdNodeStatus>, val hostnames: Map<String, String>)

@Composable
private fun EtcdContent(
    etcd: EtcdOverview,
    defrag: DefragState,
    canDefrag: Boolean,
    onDefrag: (DefragRequest) -> Unit,
    onDismissDefrag: () -> Unit,
) {
    val colors = LocalStatusColors.current
    val hostnames = etcd.members.associate { it.id to it.hostname }
    val running = defrag is DefragState.Running

    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        etcd.error?.let { item { Text(it, color = colors.bad) } }
        if (canDefrag || defrag != DefragState.Idle) {
            item {
                DefragPanel(
                    defrag = defrag,
                    order = defragOrder(etcd.statuses),
                    hostnames = hostnames,
                    canDefrag = canDefrag,
                    onDefrag = onDefrag,
                    onDismiss = onDismissDefrag,
                )
            }
        }
        if (etcd.alarms.isNotEmpty()) {
            item { SectionTitle("Alarms") }
            items(etcd.alarms) { alarm ->
                Text("${hostnames[alarm.memberId] ?: alarm.memberId}: ${alarm.alarm}", color = colors.bad)
            }
        }
        item { SectionTitle("Members (${etcd.members.size})") }
        items(etcd.statuses, key = { it.node }) { status ->
            MemberStatusCard(
                status = status,
                hostname = hostnames[status.memberId] ?: status.node,
                onDefrag = if (canDefrag && !running && status.error == null) {
                    { onDefrag(DefragRequest(listOf(status), hostnames)) }
                } else {
                    null
                },
            )
        }
        val unprobed = etcd.members.filter { m -> etcd.statuses.none { it.memberId == m.id } }
        if (unprobed.isNotEmpty()) {
            item { SectionTitle("Members not in this talosconfig") }
            items(unprobed, key = { it.id }) { MemberCard(it) }
        }
    }
}

@Composable
private fun MemberStatusCard(status: EtcdNodeStatus, hostname: String, onDefrag: (() -> Unit)?) {
    val colors = LocalStatusColors.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(hostname, style = MaterialTheme.typography.titleMedium)
                    Text(status.node, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                }
                when {
                    status.error != null -> StatusPill("Error", colors.bad)
                    status.errors.isNotEmpty() -> StatusPill("Errors", colors.bad)
                    status.isLeader -> StatusPill("Leader", colors.ok)
                    status.isLearner -> StatusPill("Learner", colors.warn)
                    else -> StatusPill("Follower", colors.muted)
                }
            }
            if (status.error != null) {
                Text(status.error, color = colors.bad, style = MaterialTheme.typography.bodySmall)
                return@Column
            }
            InfoRow("Member ID", status.memberId, mono = true)
            InfoRow("DB size", "${formatBytes(status.dbSizeInUse)} in use / ${formatBytes(status.dbSize)}")
            UsageBar(
                if (status.dbSize > 0) status.dbSizeInUse.toFloat() / status.dbSize else 0f,
                Modifier.padding(vertical = 4.dp),
            )
            InfoRow("Raft term / index", "${status.raftTerm} / ${status.raftIndex}")
            InfoRow("Storage version", status.version)
            status.errors.forEach { Text(it, color = colors.bad, style = MaterialTheme.typography.bodySmall) }
            if (onDefrag != null && status.reclaimable > 0) {
                TextButton(onClick = onDefrag) { Text("Defragment (reclaims ${formatBytes(status.reclaimable)})") }
            }
        }
    }
}

@Composable
private fun MemberCard(member: EtcdMember) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text(member.hostname, style = MaterialTheme.typography.titleMedium)
            InfoRow("Member ID", member.id, mono = true)
            InfoRow("Peer URLs", member.peerUrls.joinToString("\n"), mono = true)
        }
    }
}

@Composable
private fun DefragPanel(
    defrag: DefragState,
    order: List<EtcdNodeStatus>,
    hostnames: Map<String, String>,
    canDefrag: Boolean,
    onDefrag: (DefragRequest) -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalStatusColors.current
    val reclaimable = order.sumOf { it.reclaimable }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            when (defrag) {
                is DefragState.Running -> {
                    Text("Defragmenting ${defrag.hostname} (${defrag.index}/${defrag.total})…", style = MaterialTheme.typography.titleSmall)
                    LinearProgressIndicator(Modifier.fillMaxWidth())
                }
                is DefragState.Done -> {
                    Text(defrag.message, color = colors.ok)
                    TextButton(onClick = onDismiss) { Text("OK") }
                }
                is DefragState.Failed -> {
                    Text(defrag.message, color = colors.bad)
                    TextButton(onClick = onDismiss) { Text("OK") }
                }
                DefragState.Idle -> {
                    Text("About ${formatBytes(reclaimable)} reclaimable across ${order.size} member(s)", style = MaterialTheme.typography.titleSmall)
                    Text(
                        "Defragmentation is resource heavy: members are done one at a time, followers first and the leader last.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    if (canDefrag) {
                        OutlinedButton(
                            onClick = { onDefrag(DefragRequest(order, hostnames)) },
                            enabled = order.isNotEmpty() && reclaimable > 0,
                            modifier = Modifier.fillMaxWidth(),
                        ) { Text("Defragment all") }
                    }
                }
            }
        }
    }
}

@Composable
private fun DefragConfirmDialog(request: DefragRequest, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val names = request.targets.map { request.hostnames[it.memberId] ?: it.node }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (names.size == 1) "Defragment ${names.single()}?" else "Defragment ${names.size} members?") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    "Each member is busy while it is defragmented and its requests are slower. " +
                        "Members are done one at a time, never together, so the cluster keeps quorum.",
                )
                if (names.size > 1) Text("Order: ${names.joinToString(" → ")}", style = MaterialTheme.typography.bodySmall)
                Text("About ${formatBytes(request.targets.sumOf { it.reclaimable })} should be reclaimed.", style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = { TextButton(onClick = onConfirm) { Text("Defragment") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
