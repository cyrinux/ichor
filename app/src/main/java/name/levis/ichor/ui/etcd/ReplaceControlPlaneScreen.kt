package name.levis.ichor.ui.etcd

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.APPLY_CONFIG_COMMAND
import name.levis.ichor.model.CpReplacePlan
import name.levis.ichor.model.CpStep
import name.levis.ichor.model.CpStepState
import name.levis.ichor.model.ResetRequest
import name.levis.ichor.model.confirmToken
import name.levis.ichor.model.memberRef
import name.levis.ichor.model.removalPlan
import name.levis.ichor.model.state
import name.levis.ichor.model.step
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.ResetConfirmDialog
import name.levis.ichor.ui.node.ResetState
import name.levis.ichor.ui.node.ResetViewModel
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The guided replacement of a failed control plane: quorum check, removal of its etcd member,
 * reset (or power-off) of the old node, the new node booted by hand from a template config,
 * then the wait for the new member. Each action keeps its own confirmation.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ReplaceControlPlaneScreen(
    memberId: String,
    node: String,
    hostname: String,
    onBack: () -> Unit,
    onOpenConfig: (addr: String, host: String) -> Unit,
    vm: ReplaceControlPlaneViewModel = viewModel(factory = factory { ReplaceControlPlaneViewModel(app.talosRepository, memberId, node) }),
) {
    val context = LocalContext.current
    val talosApp = context.applicationContext as TalosApp
    val state by vm.plan.collectAsStateWithLifecycle()
    val remove by vm.remove.collectAsStateWithLifecycle()
    val join by vm.join.collectAsStateWithLifecycle()
    val plan = (state as? UiState.Loaded)?.data
    val resetNode = plan?.member?.node?.ifBlank { null } ?: node
    val reset: ResetViewModel = viewModel(key = "reset-$resetNode", factory = factory { ResetViewModel(app.talosRepository, resetNode) })
    val resetState by reset.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var confirmRemove by remember { mutableStateOf(false) }
    var confirmReset by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    LaunchedEffect(resetState) {
        when (val s = resetState) {
            ResetState.Done -> {
                snackbar.showSnackbar(context.getString(R.string.reset_requested, plan?.member?.hostname ?: hostname))
                reset.dismiss()
                vm.refresh()
            }
            is ResetState.Failed -> {
                snackbar.showSnackbar(s.message.resolve(context), withDismissAction = true)
                reset.dismiss()
            }
            else -> Unit
        }
    }

    // With the app lock on, each mutation needs a fresh fingerprint/PIN, like on the etcd screen.
    fun authenticated(title: String, onFailure: (String) -> Unit, action: () -> Unit) {
        val activity = context.findFragmentActivity()
        if (!talosApp.appLock.enabled.value || activity == null) {
            action()
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, title)) {
                AuthResult.Success -> action()
                is AuthResult.Failure -> onFailure(auth.message)
            }
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = { TopAppBar(title = { Text(stringResource(R.string.cp_replace_title)) }, navigationIcon = { BackButton(onBack) }) },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.pageContent(padding))
            is UiState.Loaded -> PullToRefreshBox(
                isRefreshing = s.refreshing,
                onRefresh = vm::refresh,
                modifier = Modifier.pageContent(padding).fillMaxSize(),
            ) {
                ReplaceSteps(
                    plan = s.data,
                    hostname = hostname,
                    remove = remove,
                    join = join,
                    resetRunning = resetState == ResetState.Running,
                    actions = StepActions(
                        onRemove = { confirmRemove = true },
                        onDismissRemove = vm::dismissRemoval,
                        onReset = { confirmReset = true },
                        onOpenConfig = { onOpenConfig(s.data.template.node, s.data.template.hostname) },
                        onCopyCommand = { copyToClipboard(context, "talosctl", APPLY_CONFIG_COMMAND) },
                        onWait = vm::startWait,
                        onStopWait = vm::stopWait,
                    ),
                )
            }
        }
    }

    if (confirmRemove && plan != null) {
        val member = plan.memberRef
        RemoveMemberDialog(
            plan = plan.removalPlan,
            viaNode = plan.template.node.ifBlank { null },
            onConfirm = {
                confirmRemove = false
                authenticated(context.getString(R.string.etcd_auth_remove, member.confirmToken), { vm.failRemoval(UiText.Raw(it)) }) {
                    vm.removeMember()
                }
            },
            onDismiss = { confirmRemove = false },
        )
    }
    if (confirmReset && plan != null) {
        val name = plan.member.hostname.ifBlank { resetNode }
        ResetConfirmDialog(
            vm = reset,
            hostname = name,
            // The member already left etcd: a graceful reset would try to leave it again.
            initial = ResetRequest(graceful = false),
            onConfirm = { request ->
                confirmReset = false
                authenticated(context.getString(R.string.reset_auth_title, name), { scope.launch { snackbar.showSnackbar(it) } }) {
                    reset.run(request)
                }
            },
            onDismiss = { confirmReset = false },
        )
    }
}

/** What the step cards can do. */
private data class StepActions(
    val onRemove: () -> Unit,
    val onDismissRemove: () -> Unit,
    val onReset: () -> Unit,
    val onOpenConfig: () -> Unit,
    val onCopyCommand: () -> Unit,
    val onWait: () -> Unit,
    val onStopWait: () -> Unit,
)

@Composable
private fun ReplaceSteps(
    plan: CpReplacePlan,
    hostname: String,
    remove: CpRemoveState,
    join: CpJoinState,
    resetRunning: Boolean,
    actions: StepActions,
) {
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        item { MemberHeader(plan, hostname) }
        item {
            StepCard(plan, CpStep.CONFIRM_QUORUM, 1, R.string.cp_replace_step_quorum) {
                InfoRow(stringResource(R.string.etcd_remove_members_after), plan.quorum.afterRemoval.toString())
                InfoRow(stringResource(R.string.etcd_remove_healthy_after), plan.quorum.healthyAfter.toString())
            }
        }
        item {
            StepCard(plan, CpStep.REMOVE_MEMBER, 2, R.string.cp_replace_step_remove) {
                when (remove) {
                    CpRemoveState.Running -> LinearProgressIndicator(Modifier.fillMaxWidth())
                    is CpRemoveState.Failed -> {
                        InlineError(remove.message.asString())
                        TextButton(onClick = actions.onDismissRemove) { Text(stringResource(R.string.common_ok)) }
                    }
                    CpRemoveState.Idle -> if (plan.state(CpStep.REMOVE_MEMBER) == CpStepState.READY) {
                        OutlinedButton(onClick = actions.onRemove) { Text(stringResource(R.string.etcd_remove_member)) }
                    }
                }
            }
        }
        item {
            StepCard(plan, CpStep.RESET_OR_POWER_OFF, 3, R.string.cp_replace_step_reset) {
                when {
                    resetRunning -> LinearProgressIndicator(Modifier.fillMaxWidth())
                    plan.state(CpStep.RESET_OR_POWER_OFF) == CpStepState.SKIPPED -> MutedText(stringResource(R.string.cp_replace_power_off))
                    plan.state(CpStep.RESET_OR_POWER_OFF) == CpStepState.READY ->
                        OutlinedButton(onClick = actions.onReset) { Text(stringResource(R.string.node_menu_reset)) }
                }
            }
        }
        item {
            StepCard(plan, CpStep.BOOT_NEW_NODE, 4, R.string.cp_replace_step_boot) {
                if (plan.template.node.isNotBlank()) {
                    MutedText(stringResource(R.string.cp_replace_boot_body, plan.template.hostname.ifBlank { plan.template.node }))
                    Text(APPLY_CONFIG_COMMAND, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedButton(onClick = actions.onOpenConfig) { Text(stringResource(R.string.cp_replace_open_template)) }
                        TextButton(onClick = actions.onCopyCommand) { Text(stringResource(R.string.cp_replace_copy_command)) }
                    }
                }
            }
        }
        item { WaitCard(plan, join, actions) }
    }
}

@Composable
private fun MemberHeader(plan: CpReplacePlan, hostname: String) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(plan.member.hostname.ifBlank { hostname.ifBlank { plan.member.id } }, style = MaterialTheme.typography.titleMedium)
            if (plan.member.node.isNotBlank()) {
                Text(plan.member.node, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
            }
            InfoRow(stringResource(R.string.etcd_member_id), plan.member.id, mono = true)
            MutedText(stringResource(R.string.cp_replace_intro))
        }
    }
}

@Composable
private fun WaitCard(plan: CpReplacePlan, join: CpJoinState, actions: StepActions) {
    val colors = LocalStatusColors.current
    val joined = join == CpJoinState.Joined
    StepCard(plan, CpStep.WAIT_MEMBER, 5, R.string.cp_replace_step_wait, done = joined) {
        when (join) {
            CpJoinState.Joined -> Text(stringResource(R.string.cp_replace_joined), color = colors.ok)
            is CpJoinState.Waiting -> {
                Text(stringResource(R.string.cp_replace_waiting), style = MaterialTheme.typography.bodyMedium)
                LinearProgressIndicator(Modifier.fillMaxWidth())
                if (join.detail.isNotBlank()) MutedText(join.detail)
                TextButton(onClick = actions.onStopWait) { Text(stringResource(R.string.cp_replace_wait_stop)) }
            }
            is CpJoinState.Failed -> {
                InlineError(join.message.asString())
                OutlinedButton(onClick = actions.onWait) { Text(stringResource(R.string.common_retry)) }
            }
            CpJoinState.Idle -> if (plan.state(CpStep.WAIT_MEMBER) == CpStepState.READY) {
                OutlinedButton(onClick = actions.onWait) { Text(stringResource(R.string.cp_replace_wait_start)) }
            }
        }
    }
}

/** One numbered step: title, state, Go's detail, then its [content]. [done] overrides the state. */
@Composable
private fun StepCard(
    plan: CpReplacePlan,
    step: CpStep,
    number: Int,
    @StringRes title: Int,
    done: Boolean = false,
    content: @Composable () -> Unit,
) {
    val colors = LocalStatusColors.current
    val state = if (done) CpStepState.DONE else plan.state(step)
    val (label, color) = when (state) {
        CpStepState.DONE -> R.string.cp_replace_state_done to colors.ok
        CpStepState.READY -> R.string.cp_replace_state_ready to colors.warn
        CpStepState.BLOCKED -> R.string.cp_replace_state_blocked to colors.bad
        CpStepState.SKIPPED -> R.string.cp_replace_state_skipped to colors.muted
        CpStepState.PENDING -> R.string.cp_replace_state_pending to colors.muted
    }
    val detail = plan.step(step).detail
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("$number. " + stringResource(title), style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                StatusPill(stringResource(label), color)
            }
            if (detail.isNotBlank()) {
                Text(detail, style = MaterialTheme.typography.bodySmall, color = if (state == CpStepState.BLOCKED) colors.bad else Color.Unspecified)
            }
            content()
        }
    }
}
