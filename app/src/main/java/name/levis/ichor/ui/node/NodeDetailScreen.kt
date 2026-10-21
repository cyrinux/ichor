package name.levis.ichor.ui.node

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.ui.Alignment
import name.levis.ichor.ui.components.JumpSection
import name.levis.ichor.ui.components.SectionJumpRail
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.ResetRequest
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.allows
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.components.rememberNodeFeatures
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.history.NodeUptime
import name.levis.ichor.ui.share.PinButton
import name.levis.ichor.ui.share.AddToHomeButton
import name.levis.ichor.ui.share.rememberShareLink
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/**
 * A node's screen: a header (role, cordoned, uptime), then everything the node offers as
 * sections on screen: its state (tiles opening [NodeTabsScreen] through [onTab]), the screens
 * that read, those that act on it, the power actions. Power, reset and cordon are confirmed here.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeDetailScreen(
    node: String,
    hostname: String,
    role: String,
    onBack: () -> Unit,
    initialAction: PowerAction? = null,
    /** Opens the cordon / uncordon confirmation once, as [initialAction] does for power. */
    initialCordon: Boolean = false,
    onTab: (Int) -> Unit,
    onMenu: (NodeMenuEntry) -> Unit,
    power: PowerViewModel = viewModel(key = "power-$node", factory = factory { PowerViewModel(app.nodeLifecycleRepository, node) }),
) {
    var confirming by remember { mutableStateOf<PowerAction?>(null) }
    // A reboot/shutdown started from the overview opens its confirmation once (not again on rotation).
    var initialActionShown by rememberSaveable { mutableStateOf(false) }
    if (!initialActionShown && initialAction != null) {
        initialActionShown = true
        confirming = initialAction
    }
    val powerState by power.state.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val app = context.applicationContext as TalosApp
    val appLock = app.appLock
    val share = rememberShareLink()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val canPower = config?.activeSummary?.allows(Feature.POWER) ?: false
    val features = rememberNodeFeatures(node)
    // One upgrade at a time in the app: the entry stays open for the node being upgraded.
    val upgrading by app.upgradeManager.current.collectAsStateWithLifecycle()
    val upgradeBusyElsewhere = upgrading?.let { it.running && it.node != node } ?: false
    // Maintenance and upgrades share the cluster lock: one of them at a time in the app.
    val maintenance by app.maintenanceManager.current.collectAsStateWithLifecycle()
    val maintenanceRunning = maintenance?.running == true
    val cordoned by app.maintenanceManager.cordoned.collectAsStateWithLifecycle()
    var confirmingCordon by remember { mutableStateOf(false) }
    var initialCordonShown by rememberSaveable { mutableStateOf(false) }
    if (!initialCordonShown && initialCordon) {
        initialCordonShown = true
        confirmingCordon = true
    }
    var confirmingReset by remember { mutableStateOf(false) }
    val reset: ResetViewModel = viewModel(key = "reset-$node", factory = factory { ResetViewModel(app.nodeLifecycleRepository, node) })
    val resetState by reset.state.collectAsStateWithLifecycle()
    var cordonBusy by remember { mutableStateOf(false) }

    LaunchedEffect(powerState) {
        when (val s = powerState) {
            is PowerState.Done -> {
                snackbar.showSnackbar(context.getString(R.string.power_requested, hostname, context.getString(s.request.title)))
                power.dismiss()
                onBack()
            }
            is PowerState.Failed -> {
                snackbar.showSnackbar(s.message.resolve(context), withDismissAction = true, duration = SnackbarDuration.Long)
                power.dismiss()
            }
            else -> Unit
        }
    }

    LaunchedEffect(resetState) {
        when (val s = resetState) {
            ResetState.Done -> {
                snackbar.showSnackbar(context.getString(R.string.reset_requested, hostname))
                reset.dismiss()
                onBack()
            }
            is ResetState.Failed -> {
                snackbar.showSnackbar(s.message.resolve(context), withDismissAction = true, duration = SnackbarDuration.Long)
                reset.dismiss()
            }
            else -> Unit
        }
    }

    // Like power actions: with the app lock on, a fresh fingerprint/PIN first.
    fun resetConfirmed(request: ResetRequest) {
        confirmingReset = false
        val activity = context.findFragmentActivity()
        if (!appLock.enabled.value || activity == null) {
            reset.run(request)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.reset_auth_title, hostname))) {
                AuthResult.Success -> reset.run(request)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message, withDismissAction = true, duration = SnackbarDuration.Long)
            }
        }
    }

    // With the app lock on, destructive actions need a fresh fingerprint/PIN.
    fun confirmed(request: PowerRequest) {
        confirming = null
        val activity = context.findFragmentActivity()
        if (!appLock.enabled.value || activity == null) {
            power.run(request)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.power_auth_title, context.getString(request.title), hostname))) {
                AuthResult.Success -> power.run(request)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message, withDismissAction = true, duration = SnackbarDuration.Long)
            }
        }
    }

    fun cordon(on: Boolean) {
        confirmingCordon = false
        cordonBusy = true
        scope.launch {
            val message = runCatching { app.maintenanceManager.cordon(node, on) }.fold(
                onSuccess = { context.getString(if (on) R.string.cordon_done else R.string.uncordon_done, hostname) },
                onFailure = { context.getString(R.string.cordon_failed, hostname, it.uiText().resolve(context)) },
            )
            cordonBusy = false
            snackbar.showSnackbar(message, withDismissAction = true, duration = SnackbarDuration.Long)
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(hostname)
                        Text(node, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (powerState is PowerState.Running || cordonBusy || resetState is ResetState.Running) {
                        CircularProgressIndicator(Modifier.size(20.dp).padding(end = 4.dp), strokeWidth = 2.dp)
                    }
                    PinButton(ShareTarget(target = ShareTarget.NODE, addr = node, host = hostname), hostname)
                    // No tab: the link opens this screen.
                    TooltipIconButton(Icons.Outlined.Share, stringResource(R.string.share_link), onClick = {
                        share(ShareTarget(target = ShareTarget.NODE, addr = node, host = hostname))
                    })
                    AddToHomeButton(ShareTarget(target = ShareTarget.NODE, addr = node, host = hostname), hostname)
                },
            )
        },
    ) { padding ->
        val groups = nodeMenuGroups(config?.activeSummary)
        // Power actions only exist for configs whose role allows them.
        val powerActions = if (canPower) PowerAction.entries else emptyList()
        val jumps = nodeJumpSections(groups, power = powerActions.isNotEmpty())
        val labels = jumps.map { stringResource(it.title) }
        val sections = remember(jumps, labels) { jumps.mapIndexed { i, jump -> JumpSection(jump.key, labels[i], jump.index) } }
        val listState = rememberLazyListState()
        Box(Modifier.pageContent(padding).fillMaxSize()) {
            LazyColumn(
                Modifier.fillMaxSize(),
                state = listState,
                contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                item(key = "header") { NodeHeader(node, role, cordoned[node]) }
                nodeSections(
                    groups = groups,
                    features = features,
                    // One upgrade at a time in the app.
                    busy = buildSet {
                        if (upgradeBusyElsewhere || maintenanceRunning) add(NodeMenuEntry.UPGRADE)
                        if (upgrading?.running == true || (maintenanceRunning && maintenance?.node != node)) {
                            add(NodeMenuEntry.MAINTENANCE)
                            add(NodeMenuEntry.DRAIN)
                        }
                        if (cordonBusy || (maintenanceRunning && maintenance?.node == node)) add(NodeMenuEntry.CORDON)
                        if (resetState is ResetState.Running || upgrading?.running == true || maintenanceRunning) add(NodeMenuEntry.RESET)
                    },
                    cordoned = cordoned[node],
                    powerActions = powerActions,
                    powerEnabled = powerState !is PowerState.Running,
                    onPick = { entry ->
                        val tab = entry.tab
                        when {
                            tab != null -> onTab(tab)
                            entry == NodeMenuEntry.CORDON -> confirmingCordon = true
                            entry == NodeMenuEntry.RESET -> confirmingReset = true
                            else -> onMenu(entry)
                        }
                    },
                    onPower = { confirming = it },
                )
            }
            SectionJumpRail(sections, listState, Modifier.align(Alignment.CenterEnd))
        }
    }

    if (confirmingCordon) {
        CordonDialog(hostname, cordoned[node], onConfirm = ::cordon, onDismiss = { confirmingCordon = false })
    }

    if (confirmingReset) {
        ResetConfirmDialog(vm = reset, hostname = hostname, onConfirm = ::resetConfirmed, onDismiss = { confirmingReset = false })
    }

    confirming?.let { action ->
        PowerConfirmDialog(
            action = action,
            hostname = hostname,
            role = role,
            onConfirm = ::confirmed,
            onDismiss = { confirming = null },
        )
    }
}

/** The node's role and cordoned state as pills (each only when known), then its uptime strip. */
@Composable
private fun NodeHeader(node: String, role: String, cordoned: Boolean?) {
    val roleLabel = when (role) {
        "controlplane" -> R.string.topology_role_controlplane
        "worker" -> R.string.topology_role_worker
        else -> null
    }
    // No card: without a known role or uptime history it would be an empty box.
    Column(Modifier.fillMaxWidth()) {
        if (roleLabel != null || cordoned == true) {
            Row(Modifier.padding(vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                roleLabel?.let { StatusPill(stringResource(it), MaterialTheme.colorScheme.primary) }
                if (cordoned == true) StatusPill(stringResource(R.string.kube_node_cordoned), LocalStatusColors.current.warn)
            }
        }
        NodeUptime(node, Modifier.padding(vertical = 4.dp))
    }
}
