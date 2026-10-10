package name.levis.ichor.ui.upgrade

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
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
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.K8sPlanStep
import name.levis.ichor.model.K8sUpgradePlan
import name.levis.ichor.model.K8sVersionChoice
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.k8sVersionAllowed
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.uiText

/** The versions offered and the plan of the one picked. */
class K8sUpgradeViewModel(private val talos: TalosRepository) : ViewModel() {
    private val _choice = MutableStateFlow<UiState<K8sVersionChoice>>(UiState.Loading)
    val choice: StateFlow<UiState<K8sVersionChoice>> = _choice.asStateFlow()
    private val _version = MutableStateFlow("")
    val version: StateFlow<String> = _version.asStateFlow()
    private val _plan = MutableStateFlow<UiState<K8sUpgradePlan>?>(null)
    val plan: StateFlow<UiState<K8sUpgradePlan>?> = _plan.asStateFlow()
    private var planning: Job? = null

    init {
        loadChoice()
    }

    fun loadChoice() {
        _choice.value = UiState.Loading
        viewModelScope.launch {
            val loaded = uiStateOf { talos.k8sUpgradeVersions() }
            _choice.value = loaded
            // The next minor when offered, else the newest patch of the current one.
            (loaded as? UiState.Loaded)?.data?.versions?.lastOrNull()?.let(::pick)
        }
    }

    fun pick(version: String) {
        _version.value = version.trim()
        refresh()
    }

    fun refresh() {
        val version = _version.value.ifEmpty { return }
        planning?.cancel()
        _plan.value = UiState.Loading
        planning = viewModelScope.launch { _plan.value = uiStateOf { talos.k8sUpgradePlan(version) } }
    }
}

/**
 * Upgrade Kubernetes, like `talosctl upgrade-k8s`: pick a version the cluster's Talos supports,
 * review each node's component images and the deprecated APIs still in use, run a dry run,
 * then the upgrade itself; the run goes on when leaving the screen.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun K8sUpgradeScreen(
    onBack: () -> Unit,
    vm: K8sUpgradeViewModel = viewModel(key = "k8s-upgrade", factory = factory { K8sUpgradeViewModel(app.talosRepository) }),
) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val upgrades = app.k8sUpgradeManager
    val current by upgrades.current.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val choice by vm.choice.collectAsStateWithLifecycle()
    val version by vm.version.collectAsStateWithLifecycle()
    val plan by vm.plan.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var dryRunFirst by rememberSaveable { mutableStateOf(true) }
    var confirming by remember { mutableStateOf<Boolean?>(null) } // the dry run flag of the run to confirm
    val cluster = config?.activeContext.orEmpty()

    fun start(dryRun: Boolean) {
        scope.launch {
            val result = runCatching { withContext(Dispatchers.IO) { upgrades.start(version, cluster, dryRun) } }
            result.exceptionOrNull()?.let { snackbar.showSnackbar(it.uiText().resolve(context), withDismissAction = true, duration = SnackbarDuration.Long) }
        }
    }

    // A dry run changes nothing: it starts at once. The real run: typed cluster name, then the app lock.
    fun confirmed(dryRun: Boolean) {
        confirming = null
        val activity = context.findFragmentActivity()
        if (dryRun || !app.appLock.enabled.value || activity == null) {
            start(dryRun)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.k8s_upgrade_auth, cluster), version)) {
                AuthResult.Success -> start(false)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message, withDismissAction = true, duration = SnackbarDuration.Long)
            }
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.k8s_upgrade_title))
                        Text(cluster, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Box(Modifier.pageContent(padding).fillMaxSize()) {
            val run = current
            if (run != null) {
                K8sUpgradeRunView(
                    run,
                    onCancel = upgrades::cancel,
                    onUpgradeNow = {
                        upgrades.dismiss()
                        confirming = false
                    },
                    onClose = {
                        upgrades.dismiss()
                        vm.refresh()
                    },
                )
            } else {
                PlanView(
                    choice = choice,
                    version = version,
                    onVersion = vm::pick,
                    onRetry = { if (choice is UiState.Failed) vm.loadChoice() else vm.refresh() },
                    plan = plan,
                    demo = config?.activeSummary?.isDemo == true,
                    dryRunFirst = dryRunFirst,
                    onDryRunFirst = { dryRunFirst = it },
                    onStart = { if (dryRunFirst) confirmed(true) else confirming = false },
                )
            }
        }
    }

    if (confirming == false) {
        HostnameConfirmDialog(
            title = stringResource(R.string.k8s_upgrade_confirm_title, cluster, version),
            hostname = cluster,
            confirmLabel = stringResource(R.string.k8s_upgrade_now),
            onConfirm = { confirmed(false) },
            onDismiss = { confirming = null },
            emphasized = true,
        ) {
            Text(stringResource(R.string.k8s_upgrade_confirm_body), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

@Composable
private fun PlanView(
    choice: UiState<K8sVersionChoice>,
    version: String,
    onVersion: (String) -> Unit,
    onRetry: () -> Unit,
    plan: UiState<K8sUpgradePlan>?,
    demo: Boolean,
    dryRunFirst: Boolean,
    onDryRunFirst: (Boolean) -> Unit,
    onStart: () -> Unit,
) {
    val colors = LocalStatusColors.current
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (demo) InfoNotice(stringResource(R.string.k8s_upgrade_demo))
        MutedText(stringResource(R.string.k8s_upgrade_intro))
        when (choice) {
            UiState.Loading -> LoadingBox()
            is UiState.Failed -> ErrorBox(choice.message, onRetry)
            is UiState.Loaded -> VersionPicker(choice.data, version, onVersion)
        }
        when (plan) {
            null -> Unit
            UiState.Loading -> LoadingBox()
            is UiState.Failed -> ErrorBox(plan.message, onRetry)
            is UiState.Loaded -> {
                val p = plan.data
                StepGroup(stringResource(R.string.k8s_upgrade_control_planes), p.controlPlaneSteps)
                StepGroup(stringResource(R.string.k8s_upgrade_kubelets), p.kubeletSteps)
                if (p.deprecatedApis.isNotEmpty()) {
                    SectionTitle(stringResource(R.string.k8s_upgrade_deprecated))
                    Card(Modifier.fillMaxWidth()) {
                        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            p.deprecatedApis.forEach { d ->
                                val critical = d.severity == "critical"
                                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                    StatusPill(d.severity.ifEmpty { "info" }, if (critical) colors.bad else colors.warn)
                                    Text(d.api, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
                                }
                                if (d.removedIn.isNotEmpty()) MutedText(stringResource(R.string.k8s_upgrade_removed_in, d.removedIn))
                            }
                        }
                    }
                }
                Card(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        SectionTitle(stringResource(R.string.upgrade_checks))
                        p.blockers.forEach { Text("✕ $it", color = colors.bad, style = MaterialTheme.typography.bodyMedium) }
                        p.warnings.forEach { Text("! $it", color = colors.warn, style = MaterialTheme.typography.bodyMedium) }
                        if (p.blockers.isEmpty() && p.warnings.isEmpty()) {
                            Text(stringResource(R.string.upgrade_no_issues), color = colors.ok, style = MaterialTheme.typography.bodyMedium)
                        }
                    }
                }
                ToggleRow(
                    title = stringResource(R.string.k8s_upgrade_dry_run_first),
                    description = stringResource(R.string.k8s_upgrade_dry_run_first_desc),
                    checked = dryRunFirst,
                    onChange = onDryRunFirst,
                )
                MutedText(stringResource(R.string.cluster_upgrade_keep_open))
                Button(onClick = onStart, enabled = p.canStart, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(if (dryRunFirst) R.string.k8s_upgrade_dry_run else R.string.k8s_upgrade_now))
                }
            }
        }
    }
}

@Composable
private fun VersionPicker(choice: K8sVersionChoice, version: String, onVersion: (String) -> Unit) {
    val colors = LocalStatusColors.current
    Text(stringResource(R.string.k8s_upgrade_current, choice.from.ifEmpty { "?" }, choice.supportedRange.ifEmpty { "?" }), style = MaterialTheme.typography.bodyMedium)
    if (choice.versions.isNotEmpty()) {
        Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            choice.versions.forEach { v ->
                FilterChip(selected = v == version, onClick = { onVersion(v) }, label = { Text(v, fontFamily = FontFamily.Monospace) })
            }
        }
    }
    choice.warning?.let { MutedText(it) }
    // Typed when the release list could not be read; only versions in range are taken.
    var typed by rememberSaveable { mutableStateOf("") }
    val allowed = typed.isNotBlank() && k8sVersionAllowed(choice, typed)
    OutlinedTextField(
        value = typed,
        onValueChange = {
            typed = it
            if (k8sVersionAllowed(choice, it)) onVersion(it)
        },
        label = { Text(stringResource(R.string.k8s_upgrade_other_version)) },
        placeholder = { Text("1.X.Y", fontFamily = FontFamily.Monospace) },
        isError = typed.isNotBlank() && !allowed,
        supportingText = { if (typed.isNotBlank() && !allowed) Text(stringResource(R.string.k8s_upgrade_out_of_range), color = colors.bad) },
        singleLine = true,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun StepGroup(title: String, steps: List<K8sPlanStep>) {
    if (steps.isEmpty()) return
    val colors = LocalStatusColors.current
    SectionTitle(title)
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 8.dp)) {
            steps.groupBy { it.node }.values.forEach { nodeSteps ->
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
                    Text(nodeSteps.first().name, style = MaterialTheme.typography.titleSmall)
                    nodeSteps.forEach { s ->
                        val tag = s.image.substringAfterLast(':')
                        Text(
                            "${s.component}: ${s.currentTag.ifEmpty { "?" }} → $tag",
                            style = MaterialTheme.typography.bodySmall,
                            fontFamily = FontFamily.Monospace,
                            color = if (s.changed) MaterialTheme.colorScheme.onSurface else colors.muted,
                        )
                    }
                }
            }
        }
    }
}
