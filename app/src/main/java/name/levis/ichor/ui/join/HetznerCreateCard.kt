package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
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
import androidx.compose.ui.text.input.PasswordVisualTransformation
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
import name.levis.ichor.data.CloudProviderStore
import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.NodeLifecycleRepository
import name.levis.ichor.model.HetznerCreateSpec
import name.levis.ichor.model.HetznerOptions
import name.levis.ichor.model.HetznerSnapshotCost
import name.levis.ichor.model.HetznerTokenCheck
import name.levis.ichor.model.snapshotsFor
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** The project's token and what a server can be created from. The token never reaches the screen. */
class HetznerCreateViewModel(private val lifecycle: NodeLifecycleRepository, private val store: CloudProviderStore) : ViewModel() {
    private val _projects = MutableStateFlow<List<String>>(emptyList())
    val projects: StateFlow<List<String>> = _projects.asStateFlow()
    private val _project = MutableStateFlow("")
    val project: StateFlow<String> = _project.asStateFlow()
    private val _options = MutableStateFlow<UiState<HetznerOptions>?>(null)
    val options: StateFlow<UiState<HetznerOptions>?> = _options.asStateFlow()
    private var loading: Job? = null

    /** Reads the stored projects; the demo lists its samples without a token. */
    fun load(demo: Boolean) {
        viewModelScope.launch {
            val stored = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.HETZNER) }
            _projects.value = stored
            when {
                demo -> loadOptions("")
                _project.value !in stored -> pick(stored.firstOrNull().orEmpty())
            }
        }
    }

    fun pick(project: String) {
        _project.value = project
        if (project.isBlank()) {
            loading?.cancel()
            _options.value = null
        } else {
            viewModelScope.launch { loadOptions(token(project)) }
        }
    }

    fun reload() {
        val project = _project.value
        viewModelScope.launch { loadOptions(if (project.isBlank()) "" else token(project)) }
    }

    /** Lists the snapshots again, keeping the shown form (and what is picked in it) meanwhile. */
    fun refresh() {
        val project = _project.value
        if (project.isBlank()) return
        loading?.cancel()
        loading = viewModelScope.launch {
            val token = token(project)
            val state = uiStateOf { lifecycle.hetznerOptions(token) }
            if (state is UiState.Loaded) _options.value = state
        }
    }

    suspend fun estimate(arch: String, location: String): HetznerSnapshotCost =
        lifecycle.hetznerSnapshotEstimate(token(_project.value), arch, location)

    private fun loadOptions(token: String) {
        loading?.cancel()
        _options.value = UiState.Loading
        loading = viewModelScope.launch { _options.value = uiStateOf { lifecycle.hetznerOptions(token) } }
    }

    suspend fun token(project: String): String =
        withContext(Dispatchers.IO) { store.token(CloudProviderStore.HETZNER, project) }

    suspend fun test(token: String): HetznerTokenCheck = lifecycle.hetznerTest(token)

    /** Saves the token and picks its project; false when the store cannot be read. */
    suspend fun save(project: String, token: String): Boolean {
        if (!withContext(Dispatchers.IO) { store.save(CloudProviderStore.HETZNER, project, token) }) return false
        _projects.value = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.HETZNER) }
        pick(project)
        return true
    }

    fun remove(project: String) {
        viewModelScope.launch {
            withContext(Dispatchers.IO) { store.save(CloudProviderStore.HETZNER, project, "") }
            val left = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.HETZNER) }
            _projects.value = left
            pick(left.firstOrNull().orEmpty())
        }
    }
}

/**
 * "Create the server at Hetzner": under the user data, once it is made. [form] is the new member's
 * (null on Omni, which only needs a server name). The cost, the typed name and the app lock come
 * before the billable create; the run's timeline then replaces the card's form.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun HetznerCreateCard(
    form: JoinConfigForm?,
    demo: Boolean,
    onAllocate: (() -> Unit)? = null,
    vm: HetznerCreateViewModel = viewModel(key = "hetzner-create", factory = factory { HetznerCreateViewModel(app.nodeLifecycleRepository, app.cloudProviderStore) }),
) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val run by app.joinManager.current.collectAsStateWithLifecycle()
    val projects by vm.projects.collectAsStateWithLifecycle()
    val project by vm.project.collectAsStateWithLifecycle()
    val options by vm.options.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var addingToken by remember { mutableStateOf(false) }
    var snapshotFor by remember { mutableStateOf<SnapshotAsk?>(null) }
    var madeSnapshot by rememberSaveable { mutableStateOf(0L) }
    LaunchedEffect(demo) { vm.load(demo) }

    SectionCard(stringResource(R.string.hetzner_title)) {
        val current = run
        if (current?.request?.hetzner != null) {
            JoinRunView(current, onStop = app.joinManager::stop, onOpenNode = null, onClose = app.joinManager::dismiss, onAllocate = onAllocate)
        } else {
            MutedText(stringResource(if (form == null) R.string.hetzner_intro_omni else R.string.hetzner_intro))
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
            if (current?.request?.snapshot != null) {
                SnapshotRunView(current, onStop = app.joinManager::stop) {
                    madeSnapshot = current.snapshotResult?.imageId ?: madeSnapshot
                    app.joinManager.dismiss()
                    vm.refresh()
                }
            }
            when (val state = options) {
                null -> Unit
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(state.message, vm::reload)
                is UiState.Loaded -> ServerForm(
                    options = state.data,
                    form = form,
                    busy = current?.running == true,
                    canMakeSnapshot = !demo,
                    madeSnapshot = madeSnapshot,
                    onMakeSnapshot = { arch, location -> snapshotFor = SnapshotAsk(state.data, arch, location) },
                ) { request ->
                    // The token is read only now, and goes to the run alone.
                    scope.launch { app.joinManager.start(request, if (project.isBlank()) "" else vm.token(project)) }
                }
            }
        }
    }
    if (addingToken) TokenDialog(vm) { addingToken = false }
    snapshotFor?.let { ask ->
        HetznerSnapshotSheet(
            options = ask.options,
            initialArch = ask.arch,
            initialLocation = ask.location,
            template = form?.template.orEmpty(),
            estimate = vm::estimate,
            onStart = { spec ->
                snapshotFor = null
                val request = JoinRequest(
                    address = "", template = spec.templateNode, role = "", hostname = "Talos ${spec.arch}", disk = "",
                    staticAddress = "", snapshot = spec,
                )
                // The token is read only now, and goes to the run alone.
                scope.launch { app.joinManager.start(request, vm.token(project)) }
            },
            onDismiss = { snapshotFor = null },
        )
    }
}

/** What the snapshot sheet opens with. */
private data class SnapshotAsk(val options: HetznerOptions, val arch: String, val location: String)

/** The pickers, the cost line and the confirmed Create. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ServerForm(
    options: HetznerOptions,
    form: JoinConfigForm?,
    busy: Boolean,
    canMakeSnapshot: Boolean,
    madeSnapshot: Long,
    onMakeSnapshot: (arch: String, location: String) -> Unit,
    onCreate: (JoinRequest) -> Unit,
) {
    val colors = LocalStatusColors.current
    val authenticated = rememberAuthenticated()
    var typeName by rememberSaveable { mutableStateOf("") }
    var locationName by rememberSaveable { mutableStateOf("") }
    var snapshotId by rememberSaveable { mutableStateOf(0L) }
    var omniName by rememberSaveable { mutableStateOf("") }
    var confirm by remember { mutableStateOf(false) }
    LaunchedEffect(madeSnapshot) { if (madeSnapshot != 0L) snapshotId = madeSnapshot }

    val type = options.serverTypes.firstOrNull { it.name == typeName }
    val snapshots = options.snapshotsFor(type)
    val snapshot = snapshots.firstOrNull { it.id == snapshotId }
    val price = type?.price(locationName)
    val hostname = form?.hostname?.trim() ?: omniName.trim()

    if (form == null) {
        OutlinedTextField(
            value = omniName,
            onValueChange = { omniName = it },
            label = { Text(stringResource(R.string.join_config_hostname)) },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false),
            modifier = Modifier.fillMaxWidth(),
        )
    }
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
    if (type != null && canMakeSnapshot && snapshots.none { it.talos }) {
        InfoNotice(stringResource(R.string.hetzner_snapshot_missing, type.arch))
        IchorOutlinedButton(onClick = { onMakeSnapshot(type.arch, locationName) }, enabled = !busy, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.hetzner_snapshot_make))
        }
    }
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        snapshots.forEach { s ->
            IchorFilterChip(selected = s.id == snapshotId, onClick = { snapshotId = s.id }, label = { Text(s.description.ifBlank { s.id.toString() }) })
        }
    }
    price?.let {
        Text(
            stringResource(R.string.hetzner_cost, "${it.hourly} ${options.currency}", "${it.monthly} ${options.currency}"),
            style = MaterialTheme.typography.titleSmall,
            color = colors.warn,
        )
    }
    IchorButton(
        onClick = { confirm = true },
        enabled = !busy && hostname.isNotEmpty() && snapshot != null && price != null,
        modifier = Modifier.fillMaxWidth(),
    ) { Text(stringResource(R.string.hetzner_create)) }

    if (confirm && type != null && snapshot != null && price != null) {
        val cost = "${price.monthly} ${options.currency}"
        HostnameConfirmDialog(
            title = stringResource(R.string.hetzner_confirm_title, hostname),
            hostname = hostname,
            confirmLabel = stringResource(R.string.hetzner_create),
            onConfirm = {
                confirm = false
                val spec = HetznerCreateSpec(
                    serverType = type.name, location = locationName, snapshotId = snapshot.id, hostname = hostname,
                    role = form?.role.orEmpty(), templateNode = form?.template.orEmpty(), installDisk = form?.disk.orEmpty(),
                )
                authenticated(hostname) {
                    onCreate(
                        JoinRequest(
                            address = "", template = spec.templateNode, role = spec.role, hostname = hostname, disk = spec.installDisk,
                            staticAddress = "", hetzner = spec, omni = form == null,
                        ),
                    )
                }
            },
            onDismiss = { confirm = false },
            emphasized = true,
        ) {
            Text(stringResource(R.string.hetzner_confirm_body, type.name, locationName, cost), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

/** Project label and token: masked, tested, then sealed. A read-only token cannot be saved for creating. */
@Composable
private fun TokenDialog(vm: HetznerCreateViewModel, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    val scope = rememberCoroutineScope()
    var project by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    var check by remember { mutableStateOf<UiState<HetznerTokenCheck>?>(null) }
    var saveFailed by remember { mutableStateOf(false) }
    val tested = (check as? UiState.Loaded)?.data

    fun runTest() {
        check = UiState.Loading
        scope.launch { check = uiStateOf { vm.test(token.trim()) } }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.hetzner_add_token)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.hetzner_token_warning), color = colors.warn, style = MaterialTheme.typography.bodySmall)
                OutlinedTextField(
                    value = project,
                    onValueChange = { project = it },
                    label = { Text(stringResource(R.string.hetzner_project_label)) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = token,
                    onValueChange = {
                        token = it
                        check = null
                    },
                    label = { Text(stringResource(R.string.hetzner_token_label)) },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                IchorOutlinedButton(onClick = ::runTest, enabled = token.isNotBlank() && check != UiState.Loading) {
                    Text(stringResource(R.string.hetzner_test))
                }
                when (val state = check) {
                    null -> Unit
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(state.message, ::runTest)
                    is UiState.Loaded -> Text(
                        if (state.data.readOnly) stringResource(R.string.hetzner_token_read_only) else stringResource(R.string.hetzner_token_ok, state.data.servers.toString()),
                        color = if (state.data.readOnly) colors.warn else colors.ok,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
                if (saveFailed) Text(stringResource(R.string.hetzner_token_not_saved), color = colors.bad, style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = {
            IchorButton(
                onClick = {
                    scope.launch {
                        if (vm.save(project.trim(), token.trim())) onDismiss() else saveFailed = true
                    }
                },
                enabled = project.isNotBlank() && tested != null && !tested.readOnly,
            ) { Text(stringResource(R.string.hetzner_save)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
