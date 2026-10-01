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

class EtcdViewModel(private val talos: TalosRepository) : LoadingViewModel<EtcdOverview>() {
    override suspend fun fetch() = talos.etcd()
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EtcdScreen(
    onBack: () -> Unit,
    vm: EtcdViewModel = viewModel(factory = factory { EtcdViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

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
                EtcdContent(s.data)
            }
        }
    }
}

@Composable
private fun EtcdContent(etcd: EtcdOverview) {
    val colors = LocalStatusColors.current
    val hostnames = etcd.members.associate { it.id to it.hostname }

    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        etcd.error?.let { item { Text(it, color = colors.bad) } }
        if (etcd.alarms.isNotEmpty()) {
            item { SectionTitle("Alarms") }
            items(etcd.alarms) { alarm ->
                Text("${hostnames[alarm.memberId] ?: alarm.memberId}: ${alarm.alarm}", color = colors.bad)
            }
        }
        item { SectionTitle("Members (${etcd.members.size})") }
        items(etcd.statuses, key = { it.node }) { status ->
            MemberStatusCard(status, hostnames[status.memberId] ?: status.node)
        }
        val unprobed = etcd.members.filter { m -> etcd.statuses.none { it.memberId == m.id } }
        if (unprobed.isNotEmpty()) {
            item { SectionTitle("Members not in this talosconfig") }
            items(unprobed, key = { it.id }) { MemberCard(it) }
        }
    }
}

@Composable
private fun MemberStatusCard(status: EtcdNodeStatus, hostname: String) {
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
