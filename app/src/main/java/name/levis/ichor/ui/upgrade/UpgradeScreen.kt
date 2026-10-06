package name.levis.ichor.ui.upgrade

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.TalosRelease
import name.levis.ichor.model.UpgradePlan
import name.levis.ichor.model.etcdBlocked
import name.levis.ichor.model.releaseSuggestions
import name.levis.ichor.model.upgradeAcknowledged
import name.levis.ichor.model.upgradeGate
import name.levis.ichor.model.upgradeRisks
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.components.TooltipIconButton

/** Release suggestions shown as chips; any other version can be typed. */
private const val MAX_RELEASE_CHIPS = 8

/** Choices made on the upgrade screen, passed to the confirmation and the run, with the [risks] to accept. */
private data class UpgradeChoice(
    val version: String,
    val image: String,
    val stage: Boolean,
    val force: Boolean,
    val risks: List<String> = emptyList(),
)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun UpgradeScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    initialVersion: String = "",
    planVm: UpgradePlanViewModel = viewModel(key = "upgrade-plan-$node", factory = factory { UpgradePlanViewModel(app.upgradeManager, node) }),
    targetVm: UpgradeTargetViewModel = viewModel(key = "upgrade-target-$node", factory = factory { UpgradeTargetViewModel(app.upgradeManager) }),
) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val upgrades = app.upgradeManager
    val current by upgrades.current.collectAsStateWithLifecycle()
    val plan by planVm.state.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var force by rememberSaveable { mutableStateOf(false) }
    var menuOpen by remember { mutableStateOf(false) }
    var forceWarning by remember { mutableStateOf(false) }
    var confirming by remember { mutableStateOf<UpgradeChoice?>(null) }
    var rollingBack by remember { mutableStateOf(false) }
    val following = current?.takeIf { it.node == node }
    LaunchedEffect(Unit) { if (plan == UiState.Loading) planVm.refresh() }

    // App lock first (a fresh fingerprint/PIN), then the typed-hostname confirmation.
    fun requestStart(choice: UpgradeChoice) {
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            confirming = choice
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.upgrade_auth_title, hostname), choice.version)) {
                AuthResult.Success -> confirming = choice
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message)
            }
        }
    }

    // The same two steps as an upgrade: the node reboots at once, into another Talos.
    fun requestRollback() {
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            rollingBack = true
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.rollback_title, hostname), context.getString(R.string.rollback_confirm))) {
                AuthResult.Success -> rollingBack = true
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message)
            }
        }
    }

    fun rollback() {
        rollingBack = false
        scope.launch {
            val result = runCatching { app.talosRepository.rollback(node) }
            snackbar.showSnackbar(result.exceptionOrNull()?.uiText()?.resolve(context) ?: context.getString(R.string.rollback_started, hostname))
        }
    }

    fun start(choice: UpgradeChoice, fromVersion: String, acknowledged: Boolean) {
        confirming = null
        scope.launch {
            val result = runCatching {
                withContext(Dispatchers.IO) {
                    upgrades.start(node, hostname, fromVersion, choice.image, choice.stage, choice.force, acknowledged)
                }
            }
            result.exceptionOrNull()?.let { snackbar.showSnackbar(it.uiText().resolve(context)) }
            if (result.getOrNull() == false) {
                snackbar.showSnackbar(context.getString(R.string.upgrade_other_running, upgrades.current.value?.hostname.orEmpty()))
            }
        }
    }

    val showForce = (plan as? UiState.Loaded)?.data?.etcdBlocked ?: false
    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.upgrade_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (following == null) {
                        Box {
                            TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { menuOpen = true })
                            DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                                // Force is tucked away and only offered when etcd checks block the upgrade.
                                if (showForce) {
                                    DropdownMenuItem(
                                        text = { Text(stringResource(R.string.upgrade_force)) },
                                        trailingIcon = { Checkbox(checked = force, onCheckedChange = null) },
                                        onClick = {
                                            menuOpen = false
                                            if (force) force = false else forceWarning = true
                                        },
                                    )
                                }
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.rollback_menu)) },
                                    onClick = {
                                        menuOpen = false
                                        requestRollback()
                                    },
                                )
                            }
                        }
                    }
                },
            )
        },
    ) { padding ->
        Box(Modifier.padding(padding).fillMaxSize()) {
            if (following != null) {
                UpgradeProgress(
                    following,
                    onStopFollowing = {
                        upgrades.stopFollowing()
                        onBack()
                    },
                    onClose = {
                        upgrades.dismiss()
                        onBack()
                    },
                )
            } else {
                when (val s = plan) {
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(s.message, planVm::refresh)
                    is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = planVm::refresh) {
                        UpgradeSetup(
                            plan = s.data,
                            initialVersion = initialVersion,
                            targetVm = targetVm,
                            force = force && showForce,
                            otherRunning = current?.takeIf { it.running }?.hostname,
                            onStart = ::requestStart,
                        )
                    }
                }
            }
        }
    }

    if (forceWarning) {
        ConfirmDialog(
            title = stringResource(R.string.upgrade_force_title),
            text = stringResource(R.string.upgrade_force_warning),
            confirm = stringResource(R.string.upgrade_force_enable),
            onConfirm = {
                forceWarning = false
                force = true
            },
            onDismiss = { forceWarning = false },
            destructive = true,
        )
    }
    if (rollingBack) {
        HostnameConfirmDialog(
            title = stringResource(R.string.rollback_title, hostname),
            hostname = hostname,
            confirmLabel = stringResource(R.string.rollback_confirm),
            onConfirm = ::rollback,
            onDismiss = { rollingBack = false },
            emphasized = true,
        ) {
            Text(stringResource(R.string.rollback_body), style = MaterialTheme.typography.bodyMedium)
        }
    }
    confirming?.let { choice ->
        val from = (plan as? UiState.Loaded)?.data?.currentVersion.orEmpty()
        var understood by remember(choice) { mutableStateOf(false) }
        HostnameConfirmDialog(
            title = stringResource(R.string.upgrade_confirm_title, hostname, choice.version),
            hostname = hostname,
            confirmLabel = stringResource(R.string.upgrade_start),
            onConfirm = { start(choice, from, acknowledged = choice.risks.isNotEmpty() && understood) },
            onDismiss = { confirming = null },
            emphasized = choice.force,
            enabled = upgradeAcknowledged(choice.risks, understood),
        ) {
            Text(stringResource(R.string.upgrade_confirm_body, choice.image), style = MaterialTheme.typography.bodyMedium)
            if (choice.force) {
                Text(stringResource(R.string.upgrade_force_on), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium)
            }
            if (choice.risks.isNotEmpty()) RiskAcknowledgment(choice.risks, understood) { understood = it }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun UpgradeSetup(
    plan: UpgradePlan,
    initialVersion: String,
    targetVm: UpgradeTargetViewModel,
    force: Boolean,
    otherRunning: String?,
    onStart: (UpgradeChoice) -> Unit,
) {
    val colors = LocalStatusColors.current
    val releases by targetVm.releases.collectAsStateWithLifecycle()
    val target by targetVm.target.collectAsStateWithLifecycle()
    var version by rememberSaveable { mutableStateOf(initialVersion) }
    var stage by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(plan.currentImage, plan.currentVersion, version) { targetVm.setVersion(plan.currentImage, plan.currentVersion, version) }
    val image = if (target.version == version.trim() && !target.pending) target.image else ""
    val versionRisk = if (image.isNotEmpty()) target.risk else ""
    val gate = upgradeGate(plan, version, image, force, otherRunning != null)

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp)) {
                InfoRow(stringResource(R.string.upgrade_current_version), plan.currentVersion.ifEmpty { "—" })
                if (plan.controlPlane) InfoRow(stringResource(R.string.upgrade_role), stringResource(R.string.upgrade_control_plane))
                Text(stringResource(R.string.upgrade_current_image), style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(top = 8.dp))
                SelectionContainer { Text(plan.currentImage.ifEmpty { "—" }, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
                if (plan.schematic.isNotEmpty()) {
                    Text(stringResource(R.string.upgrade_schematic), style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(top = 8.dp))
                    SelectionContainer { Text(plan.schematic, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
                }
            }
        }

        SectionTitle(stringResource(R.string.upgrade_target_version))
        OutlinedTextField(
            value = version,
            onValueChange = { version = it },
            label = { Text(stringResource(R.string.upgrade_target_version)) },
            placeholder = { Text(stringResource(R.string.upgrade_target_hint)) },
            singleLine = true,
            isError = target.error != null && target.version == version.trim(),
            supportingText = target.error?.takeIf { target.version == version.trim() }?.let { { Text(it.asString()) } },
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth(),
        )
        ReleaseChips(releases, plan.currentVersion, version) { version = it }

        if (image.isNotEmpty()) {
            Text(stringResource(R.string.upgrade_image), style = MaterialTheme.typography.labelMedium)
            SelectionContainer { Text(image, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
        }
        if (version.trim().isNotEmpty() && version.trim() == plan.currentVersion) {
            Text(stringResource(R.string.upgrade_same_version, plan.currentVersion), color = colors.warn, style = MaterialTheme.typography.bodySmall)
        }

        ToggleRow(
            title = stringResource(R.string.upgrade_stage),
            description = stringResource(R.string.upgrade_stage_desc),
            checked = stage,
            onChange = { stage = it },
        )

        PlanChecks(plan, versionRisk)
        if (force) Text(stringResource(R.string.upgrade_force_on), color = colors.bad, style = MaterialTheme.typography.bodyMedium)
        if (otherRunning != null) {
            Text(stringResource(R.string.upgrade_other_running, otherRunning), color = colors.warn, style = MaterialTheme.typography.bodySmall)
        }
        Button(
            onClick = { onStart(UpgradeChoice(version.trim(), image, stage, force, upgradeRisks(plan, versionRisk))) },
            enabled = gate.canStart,
            modifier = Modifier.fillMaxWidth(),
        ) { Text(stringResource(R.string.upgrade_start)) }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ReleaseChips(releases: UiState<List<TalosRelease>>, currentVersion: String, selected: String, onPick: (String) -> Unit) {
    when (releases) {
        UiState.Loading -> Unit
        is UiState.Failed -> MutedText(stringResource(R.string.upgrade_releases_failed, releases.message.asString()))
        is UiState.Loaded -> FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            releaseSuggestions(releases.data).filter { it.version != currentVersion }.take(MAX_RELEASE_CHIPS).forEach { r ->
                FilterChip(
                    selected = selected.trim() == r.version,
                    onClick = { onPick(r.version) },
                    label = {
                        Text(if (r.prerelease) stringResource(R.string.upgrade_prerelease, r.version) else r.version, fontFamily = FontFamily.Monospace)
                    },
                )
            }
        }
    }
}

@Composable
private fun PlanChecks(plan: UpgradePlan, versionRisk: String) {
    val colors = LocalStatusColors.current
    val risks = upgradeRisks(plan, versionRisk)
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            SectionTitle(stringResource(R.string.upgrade_checks))
            plan.etcd?.takeIf { it.thisNodeMember }?.let { e ->
                Text(stringResource(R.string.upgrade_etcd_members, e.healthy, e.members), style = MaterialTheme.typography.bodyMedium)
                Text(
                    stringResource(if (e.quorumAfterLoss) R.string.upgrade_etcd_quorum_ok else R.string.upgrade_etcd_quorum_lost),
                    style = MaterialTheme.typography.bodySmall,
                    color = if (e.quorumAfterLoss) colors.ok else colors.bad,
                )
            }
            plan.blockers.forEach { Text("✕ $it", color = colors.bad, style = MaterialTheme.typography.bodyMedium) }
            if (risks.isNotEmpty()) {
                Text(stringResource(R.string.upgrade_acknowledge_title), style = MaterialTheme.typography.labelMedium)
                risks.forEach { Text("⚠ $it", color = colors.warn, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium) }
            }
            plan.warnings.forEach { Text("! $it", color = colors.warn, style = MaterialTheme.typography.bodyMedium) }
            if (plan.blockers.isEmpty() && risks.isEmpty() && plan.warnings.isEmpty()) {
                Text(stringResource(R.string.upgrade_no_issues), color = colors.ok, style = MaterialTheme.typography.bodyMedium)
            }
        }
    }
}

/** The [risks] the confirmation lists, and the box to tick to accept them (required to start). */
@Composable
private fun RiskAcknowledgment(risks: List<String>, understood: Boolean, onChange: (Boolean) -> Unit) {
    val colors = LocalStatusColors.current
    risks.forEach { Text("⚠ $it", color = colors.warn, style = MaterialTheme.typography.bodyMedium) }
    Row(
        Modifier.fillMaxWidth().toggleable(value = understood, role = Role.Checkbox, onValueChange = onChange),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = understood, onCheckedChange = null, modifier = Modifier.padding(end = 12.dp, top = 8.dp, bottom = 8.dp))
        Text(stringResource(R.string.upgrade_acknowledge_check), style = MaterialTheme.typography.bodyMedium)
    }
}
