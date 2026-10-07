package name.levis.ichor.ui.etcd

import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.share.ShareLinkButton
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.ETCD
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.EtcdMember
import name.levis.ichor.model.EtcdNodeStatus
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
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
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.defragOrder
import name.levis.ichor.model.EtcdLag
import name.levis.ichor.model.ETCD_LAG_ENTRIES
import name.levis.ichor.model.etcdLag
import name.levis.ichor.model.laggingMembers
import name.levis.ichor.model.reclaimable
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.userMessage
import name.levis.ichor.model.EtcdMemberRef
import name.levis.ichor.model.FeatureSupport
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.VersionNotice
import name.levis.ichor.model.clusterSupport
import name.levis.ichor.model.confirmToken
import name.levis.ichor.model.notice
import name.levis.ichor.model.nodeHostnames
import name.levis.ichor.model.removalNode
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.FeatureGate
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.rememberClusterFeatures
import name.levis.ichor.ui.components.text
import name.levis.ichor.ui.components.pageContent

sealed interface DefragState {
    data object Idle : DefragState
    data class Running(val hostname: String, val index: Int, val total: Int) : DefragState
    data class Done(val members: Int, val reclaimed: Long) : DefragState
    data class Failed(val message: String) : DefragState
}

class EtcdViewModel(private val talos: TalosRepository) : LoadingViewModel<EtcdOverview>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<EtcdOverview>? = talos.cached(ETCD)
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.etcd()

    /** node address -> hostname, from the cached overview when available. */
    fun knownHostnames(): Map<String, String> =
        talos.cached<ClusterOverview>(OVERVIEW)?.value?.nodes?.associate { it.node to it.hostname }.orEmpty()

    private val _defrag = MutableStateFlow<DefragState>(DefragState.Idle)
    val defrag: StateFlow<DefragState> = _defrag.asStateFlow()

    /** Defragments [targets] (named by [hostnames], node -> hostname) strictly one after another; stops at the first failure. */
    fun defragment(targets: List<EtcdNodeStatus>, hostnames: Map<String, String>) {
        if (_defrag.value is DefragState.Running || targets.isEmpty()) return
        viewModelScope.launch {
            targets.forEachIndexed { i, member ->
                val name = hostnames[member.node] ?: member.node
                _defrag.value = DefragState.Running(name, i + 1, targets.size)
                val result = runCatching { talos.etcdDefragment(member.node) }
                refresh()
                result.exceptionOrNull()?.let {
                    _defrag.value = DefragState.Failed("$name: ${it.userMessage()}")
                    return@launch
                }
            }
            val reclaimed = targets.sumOf { it.reclaimable }
            _defrag.value = DefragState.Done(targets.size, reclaimed)
        }
    }

    fun dismissDefrag() {
        _defrag.value = DefragState.Idle
    }

    private val _disarm = MutableStateFlow<DisarmState>(DisarmState.Idle)
    val disarm: StateFlow<DisarmState> = _disarm.asStateFlow()

    /** Clears the (cluster-wide) alarms through [node], then reloads to show what is left. */
    fun disarmAlarms(node: String) {
        if (_disarm.value == DisarmState.Running) return
        _disarm.value = DisarmState.Running
        viewModelScope.launch {
            val result = runCatching { talos.etcdAlarmDisarm(node) }
            refresh()
            _disarm.value = result.fold(
                onSuccess = { DisarmState.Idle },
                onFailure = { DisarmState.Failed(it.userMessage()) },
            )
        }
    }

    fun dismissDisarm() {
        if (_disarm.value != DisarmState.Running) _disarm.value = DisarmState.Idle
    }
}

sealed interface DisarmState {
    data object Idle : DisarmState
    data object Running : DisarmState
    data class Failed(val message: String) : DisarmState
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EtcdScreen(
    onBack: () -> Unit,
    vm: EtcdViewModel = viewModel(factory = factory { EtcdViewModel(app.talosRepository) }),
    snapshotVm: EtcdSnapshotViewModel = viewModel(factory = factory { EtcdSnapshotViewModel(app.talosRepository, app) }),
    memberVm: EtcdMemberActionsViewModel = viewModel(factory = factory { EtcdMemberActionsViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val defrag by vm.defrag.collectAsStateWithLifecycle()
    val disarm by vm.disarm.collectAsStateWithLifecycle()
    val snapshot by snapshotVm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val canDefrag = config?.activeSummary?.allows(Feature.ETCD_DEFRAG) ?: false
    val canSnapshot = config?.activeSummary?.allows(Feature.ETCD_SNAPSHOT) ?: false
    val canMemberActions = config?.activeSummary?.allows(Feature.ETCD_MEMBER_ACTIONS) ?: false
    val clusterFeatures = rememberClusterFeatures()
    val memberSupport = clusterSupport(clusterFeatures, TalosFeature.ETCD_MEMBER_ACTIONS)
    val memberAction by memberVm.state.collectAsStateWithLifecycle()
    var confirmForfeit by remember { mutableStateOf<EtcdNodeStatus?>(null) }
    // A member action changed the cluster (or failed half-way): show what is true now.
    LaunchedEffect(memberAction) {
        if (memberAction is MemberActionState.Done || memberAction is MemberActionState.Failed) vm.refresh()
    }
    val scope = rememberCoroutineScope()
    var confirm by remember { mutableStateOf<DefragRequest?>(null) }
    var confirmDisarm by remember { mutableStateOf<String?>(null) }
    val startSnapshot = rememberSnapshotFlow(snapshotVm, config?.activeSummary?.name.orEmpty(), config?.activeSummary?.fingerprint.orEmpty())

    // With the app lock on, defragmentation needs a fresh fingerprint/PIN, like reboot.
    fun confirmed(request: DefragRequest) {
        confirm = null
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            vm.defragment(request.targets, request.hostnames)
            return
        }
        scope.launch {
            if (authenticate(activity, context.getString(R.string.etcd_auth_defrag)) is AuthResult.Success) {
                vm.defragment(request.targets, request.hostnames)
            }
        }
    }

    // With the app lock on, member actions need a fresh fingerprint/PIN, like reboot.
    fun authenticated(title: String, action: () -> Unit) {
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            action()
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, title)) {
                AuthResult.Success -> action()
                is AuthResult.Failure -> memberVm.fail(UiText.Raw(auth.message))
            }
        }
    }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = { Text("etcd") },
                navigationIcon = { BackButton(onBack) },
                actions = { ShareLinkButton(ShareTarget.screen(ShareTarget.ETCD)) },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.pageContent(padding))
            is UiState.Loaded -> FeatureGate(clusterSupport(clusterFeatures, TalosFeature.ETCD), Modifier.pageContent(padding)) {
                PullToRefreshBox(
                    isRefreshing = s.refreshing,
                    onRefresh = vm::refresh,
                    modifier = Modifier.pageContent(padding).fillMaxSize(),
                ) {
                    EtcdContent(
                        etcd = s.data,
                        knownHostnames = remember { vm.knownHostnames() },
                        defrag = defrag,
                        canDefrag = canDefrag,
                        onDefrag = { confirm = it },
                        onDismissDefrag = vm::dismissDefrag,
                        alarms = AlarmActions(
                            state = disarm,
                            // Alarms are cluster-wide: any reachable member can clear them.
                            onDisarm = s.data.statuses.firstOrNull { it.error == null }?.node
                                ?.takeIf { canDefrag }
                                ?.let { node -> { confirmDisarm = node } },
                            onDismiss = vm::dismissDisarm,
                        ),
                        members = MemberActionsHost(
                            state = memberAction,
                            support = memberSupport,
                            allowed = canMemberActions,
                            onForfeit = { confirmForfeit = it },
                            onRemove = memberVm::plan,
                            onDismiss = memberVm::dismiss,
                        ),
                        snapshot = if (canSnapshot || snapshot != SnapshotState.Idle) {
                            SnapshotActions(
                                notice = clusterSupport(clusterFeatures, TalosFeature.ETCD_SNAPSHOT).notice,
                                state = snapshot,
                                onSave = { startSnapshot(s.data) },
                                onCancel = snapshotVm::cancel,
                                onDismiss = snapshotVm::dismiss,
                            )
                        } else {
                            null
                        },
                    )
                }
            }
        }
    }

    confirmForfeit?.let { leader ->
        val hostname = (state as? UiState.Loaded)?.data?.nodeHostnames(vm.knownHostnames())?.get(leader.node) ?: leader.node
        ForfeitConfirmDialog(
            hostname = hostname,
            onConfirm = {
                confirmForfeit = null
                authenticated(context.getString(R.string.etcd_auth_forfeit)) { memberVm.forfeitLeadership(leader.node, hostname) }
            },
            onDismiss = { confirmForfeit = null },
        )
    }
    when (val action = memberAction) {
        is MemberActionState.Planning -> PlanningDialog(action.member, onDismiss = memberVm::dismiss)
        is MemberActionState.Planned -> {
            val member = action.plan.member
            val via = removalNode((state as? UiState.Loaded)?.data?.statuses.orEmpty(), member.id)
            RemoveMemberDialog(
                plan = action.plan,
                viaNode = via,
                onConfirm = {
                    memberVm.dismiss()
                    if (via != null) {
                        authenticated(context.getString(R.string.etcd_auth_remove, member.confirmToken)) { memberVm.remove(via, member) }
                    }
                },
                onDismiss = memberVm::dismiss,
            )
        }
        else -> Unit
    }

    confirm?.let { request ->
        DefragConfirmDialog(request, onConfirm = { confirmed(request) }, onDismiss = { confirm = null })
    }
    confirmDisarm?.let { node ->
        ConfirmDialog(
            title = stringResource(R.string.etcd_disarm_confirm_title),
            text = stringResource(R.string.etcd_disarm_confirm_body),
            confirm = stringResource(R.string.etcd_disarm),
            onConfirm = {
                confirmDisarm = null
                vm.disarmAlarms(node)
            },
            onDismiss = { confirmDisarm = null },
        )
    }
}

/** The alarms section's disarm action; [onDisarm] is null when not allowed. */
private data class AlarmActions(val state: DisarmState, val onDisarm: (() -> Unit)?, val onDismiss: () -> Unit)

/** Member actions of the screen; not [allowed] by the role: nothing is offered. */
private data class MemberActionsHost(
    val state: MemberActionState,
    val support: FeatureSupport,
    val allowed: Boolean,
    val onForfeit: (EtcdNodeStatus) -> Unit,
    val onRemove: (EtcdMemberRef) -> Unit,
    val onDismiss: () -> Unit,
) {
    /** What the card of the member [id] ([hostname]) offers; [leader] set for the leader's status. */
    fun actionsFor(id: String, hostname: String, leader: EtcdNodeStatus?): MemberActions? {
        if (!allowed || id.isEmpty()) return null
        return MemberActions(
            support = support,
            enabled = !state.busy,
            onForfeit = leader?.let { { onForfeit(it) } },
            onRemove = { onRemove(EtcdMemberRef(id, hostname)) },
        )
    }
}

private data class SnapshotActions(
    val notice: VersionNotice?,
    val state: SnapshotState,
    val onSave: () -> Unit,
    val onCancel: () -> Unit,
    val onDismiss: () -> Unit,
)

/** What the confirmation dialog is about to defragment; [hostnames] maps node -> hostname. */
data class DefragRequest(val targets: List<EtcdNodeStatus>, val hostnames: Map<String, String>)

@Composable
private fun EtcdContent(
    etcd: EtcdOverview,
    knownHostnames: Map<String, String>,
    defrag: DefragState,
    canDefrag: Boolean,
    onDefrag: (DefragRequest) -> Unit,
    onDismissDefrag: () -> Unit,
    alarms: AlarmActions,
    members: MemberActionsHost,
    snapshot: SnapshotActions?,
) {
    val colors = LocalStatusColors.current
    val hostnames = etcd.members.associate { it.id to it.hostname }
    // Nodes are addresses; a node that did not answer has no member id to name it by.
    val nodeNames = remember(etcd, knownHostnames) { etcd.nodeHostnames(knownHostnames) }
    val running = defrag is DefragState.Running

    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        etcd.error?.let { item { Text(it, color = colors.bad) } }
        if (canDefrag || defrag != DefragState.Idle) {
            item {
                DefragPanel(
                    defrag = defrag,
                    order = defragOrder(etcd.statuses),
                    hostnames = nodeNames,
                    canDefrag = canDefrag,
                    onDefrag = onDefrag,
                    onDismiss = onDismissDefrag,
                )
            }
        }
        if (members.state is MemberActionState.Running || members.state is MemberActionState.Done || members.state is MemberActionState.Failed) {
            item { MemberActionPanel(members.state, members.onDismiss) }
        }
        snapshot?.let { snap ->
            item { SnapshotPanel(snap.state, snap.onSave, snap.onCancel, snap.onDismiss, snap.notice) }
        }
        etcd.alarmsError?.let { error ->
            item { SectionTitle(stringResource(R.string.etcd_section_alarms)) }
            item { Text(stringResource(R.string.etcd_alarms_check_failed, error), color = colors.bad) }
        }
        if (etcd.alarms.isNotEmpty()) {
            item { SectionTitle(stringResource(R.string.etcd_section_alarms)) }
            items(etcd.alarms) { alarm ->
                Text("${hostnames[alarm.memberId] ?: alarm.memberId}: ${alarm.alarm}", color = colors.bad)
            }
            alarms.onDisarm?.let { onDisarm ->
                item {
                    OutlinedButton(onClick = onDisarm, enabled = alarms.state != DisarmState.Running, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(if (alarms.state == DisarmState.Running) R.string.etcd_disarming else R.string.etcd_disarm_alarms))
                    }
                }
            }
        }
        (alarms.state as? DisarmState.Failed)?.let { failed ->
            item {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(failed.message, color = colors.bad, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
                    TextButton(onClick = alarms.onDismiss) { Text(stringResource(R.string.common_ok)) }
                }
            }
        }
        item { SectionTitle(stringResource(R.string.etcd_section_members, etcd.members.size)) }
        val lagging = laggingMembers(etcd.statuses)
        if (lagging.isNotEmpty()) {
            item {
                val names = lagging.joinToString(", ") { hostnames[it.memberId] ?: it.node }
                Text(pluralStringResource(R.plurals.etcd_lagging_members, lagging.size, lagging.size, names), color = colors.warn)
            }
        }
        items(etcd.statuses, key = { it.node }) { status ->
            val hostname = nodeNames[status.node] ?: status.node
            MemberStatusCard(
                status = status,
                hostname = hostname,
                lag = etcdLag(status, etcd.statuses),
                actions = members.actionsFor(status.memberId, hostname, status.takeIf { it.isLeader && it.error == null }),
                onDefrag = if (canDefrag && !running && status.error == null) {
                    { onDefrag(DefragRequest(listOf(status), nodeNames)) }
                } else {
                    null
                },
            )
        }
        val unprobed = etcd.members.filter { m -> etcd.statuses.none { it.memberId == m.id } }
        if (unprobed.isNotEmpty()) {
            item { SectionTitle(stringResource(R.string.etcd_section_unprobed)) }
            items(unprobed, key = { it.id }) { MemberCard(it, members.actionsFor(it.id, it.hostname, leader = null)) }
        }
    }
}

@Composable
private fun MemberStatusCard(status: EtcdNodeStatus, hostname: String, lag: EtcdLag?, actions: MemberActions?, onDefrag: (() -> Unit)?) {
    val colors = LocalStatusColors.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(hostname, style = MaterialTheme.typography.titleMedium)
                    Text(status.node, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                }
                when {
                    status.error != null -> StatusPill(stringResource(R.string.etcd_status_error), colors.bad)
                    status.errors.isNotEmpty() -> StatusPill(stringResource(R.string.etcd_status_errors), colors.bad)
                    status.isLeader -> StatusPill(stringResource(R.string.etcd_status_leader), colors.ok)
                    lag?.lagging == true -> StatusPill(stringResource(R.string.etcd_status_lagging), colors.warn)
                    status.isLearner -> StatusPill(stringResource(R.string.etcd_status_learner), colors.warn)
                    else -> StatusPill(stringResource(R.string.etcd_status_follower), colors.muted)
                }
                actions?.let { MemberOverflow(it) }
            }
            if (status.error != null) {
                Text(status.error, color = colors.bad, style = MaterialTheme.typography.bodySmall)
                return@Column
            }
            InfoRow(stringResource(R.string.etcd_member_id), status.memberId, mono = true)
            InfoRow(stringResource(R.string.etcd_db_size), stringResource(R.string.etcd_db_size_value, formatBytes(status.dbSizeInUse), formatBytes(status.dbSize)))
            UsageBar(
                if (status.dbSize > 0) status.dbSizeInUse.toFloat() / status.dbSize else 0f,
                Modifier.padding(vertical = 4.dp),
            )
            InfoRow(stringResource(R.string.etcd_raft), "${status.raftTerm} / ${status.raftIndex}")
            lag?.let { LagRows(it, isLeader = status.isLeader) }
            InfoRow(stringResource(R.string.etcd_storage_version), status.version)
            status.errors.forEach { Text(it, color = colors.bad, style = MaterialTheme.typography.bodySmall) }
            if (onDefrag != null && status.reclaimable > 0) {
                TextButton(onClick = onDefrag) { Text(stringResource(R.string.etcd_defragment_member, formatBytes(status.reclaimable))) }
            }
            actions?.onForfeit?.let { onForfeit ->
                val notice = actions.support.notice
                TextButton(onClick = onForfeit, enabled = actions.enabled && notice == null) { Text(stringResource(R.string.etcd_forfeit)) }
                notice?.let { InfoNotice(it.text()) }
            }
        }
    }
}

/** How far the member trails: behind the leader (followers), and its apply backlog when it matters. */
@Composable
private fun LagRows(lag: EtcdLag, isLeader: Boolean) {
    val colors = LocalStatusColors.current
    val behind = lag.behindLeader
    if (!isLeader && behind != null) {
        InfoRow(
            label = stringResource(R.string.etcd_behind_leader),
            value = if (behind == 0L) stringResource(R.string.etcd_in_sync) else entries(behind),
            valueColor = if (behind >= ETCD_LAG_ENTRIES) colors.warn else Color.Unspecified,
        )
    }
    if (lag.applyBacklog >= ETCD_LAG_ENTRIES) {
        InfoRow(stringResource(R.string.etcd_apply_backlog), entries(lag.applyBacklog), valueColor = colors.warn)
    }
}

@Composable
private fun entries(count: Long): String =
    pluralStringResource(R.plurals.etcd_entries, count.coerceAtMost(Int.MAX_VALUE.toLong()).toInt(), count)

@Composable
private fun MemberCard(member: EtcdMember, actions: MemberActions?) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(member.hostname, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                actions?.let { MemberOverflow(it) }
            }
            InfoRow(stringResource(R.string.etcd_member_id), member.id, mono = true)
            InfoRow(stringResource(R.string.etcd_peer_urls), member.peerUrls.joinToString("\n"), mono = true)
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
                    Text(stringResource(R.string.etcd_defragmenting, defrag.hostname, defrag.index, defrag.total), style = MaterialTheme.typography.titleSmall)
                    LinearProgressIndicator(Modifier.fillMaxWidth())
                }
                is DefragState.Done -> {
                    Text(
                        pluralStringResource(R.plurals.etcd_defrag_done, defrag.members, defrag.members, formatBytes(defrag.reclaimed)),
                        color = colors.ok,
                    )
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
                }
                is DefragState.Failed -> {
                    Text(defrag.message, color = colors.bad)
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
                }
                DefragState.Idle -> {
                    Text(pluralStringResource(R.plurals.etcd_reclaimable_across, order.size, formatBytes(reclaimable), order.size), style = MaterialTheme.typography.titleSmall)
                    MutedText(stringResource(R.string.etcd_defrag_explainer))
                    if (canDefrag) {
                        OutlinedButton(
                            onClick = { onDefrag(DefragRequest(order, hostnames)) },
                            enabled = order.isNotEmpty() && reclaimable > 0,
                            modifier = Modifier.fillMaxWidth(),
                        ) { Text(stringResource(R.string.etcd_defragment_all)) }
                    }
                }
            }
        }
    }
}

@Composable
private fun DefragConfirmDialog(request: DefragRequest, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val names = request.targets.map { request.hostnames[it.node] ?: it.node }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = {
            Text(
                if (names.size == 1) {
                    stringResource(R.string.etcd_confirm_single, names.single())
                } else {
                    pluralStringResource(R.plurals.etcd_confirm_multi, names.size, names.size)
                },
            )
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.etcd_confirm_body))
                if (names.size > 1) Text(stringResource(R.string.etcd_confirm_order, names.joinToString(" → ")), style = MaterialTheme.typography.bodySmall)
                Text(stringResource(R.string.etcd_confirm_reclaim, formatBytes(request.targets.sumOf { it.reclaimable })), style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = { TextButton(onClick = onConfirm) { Text(stringResource(R.string.etcd_defragment)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
