package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
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
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.OVHCheck
import name.levis.ichor.model.OVHCreateSpec
import name.levis.ichor.model.OVHCredential
import name.levis.ichor.model.OVHOptions
import name.levis.ichor.model.flavorsIn
import name.levis.ichor.model.imagesIn
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** The stored service accounts and what an instance can be created from. The secret never reaches the screen. */
class OVHCreateViewModel(private val lifecycle: NodeLifecycleRepository, private val store: CloudProviderStore) : ViewModel() {
    private val _accounts = MutableStateFlow<List<String>>(emptyList())
    val accounts: StateFlow<List<String>> = _accounts.asStateFlow()
    private val _account = MutableStateFlow("")
    val account: StateFlow<String> = _account.asStateFlow()
    private val _options = MutableStateFlow<UiState<OVHOptions>?>(null)
    val options: StateFlow<UiState<OVHOptions>?> = _options.asStateFlow()

    /** Reads the stored accounts; the demo lists its samples without one. */
    fun load(demo: Boolean) {
        viewModelScope.launch {
            val stored = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.OVH) }
            _accounts.value = stored
            when {
                demo -> reload()
                _account.value !in stored -> pick(stored.firstOrNull().orEmpty())
            }
        }
    }

    fun pick(account: String) {
        _account.value = account
        if (account.isBlank()) _options.value = null else reload()
    }

    fun reload() {
        val account = _account.value
        viewModelScope.launch {
            _options.value = UiState.Loading
            _options.value = uiStateOf { lifecycle.ovhOptions(if (account.isBlank()) "" else credential(account)) }
        }
    }

    suspend fun credential(account: String): String = withContext(Dispatchers.IO) { store.token(CloudProviderStore.OVH, account) }

    suspend fun test(credentialJson: String): OVHCheck = lifecycle.ovhTest(credentialJson)

    suspend fun policy(project: String): String = lifecycle.ovhPolicy(project)

    /** Seals the account and picks it; false when the store cannot be read. */
    suspend fun save(account: String, credentialJson: String): Boolean {
        if (!withContext(Dispatchers.IO) { store.save(CloudProviderStore.OVH, account, credentialJson) }) return false
        _accounts.value = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.OVH) }
        pick(account)
        return true
    }

    fun remove(account: String) {
        viewModelScope.launch {
            withContext(Dispatchers.IO) { store.save(CloudProviderStore.OVH, account, "") }
            val left = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.OVH) }
            _accounts.value = left
            pick(left.firstOrNull().orEmpty())
        }
    }
}

/**
 * "Create the instance at OVHcloud": the run makes the user data itself. [form] is the new member's
 * (null on Omni, which only needs a name). The cost, the typed name and the app lock come before
 * the billable create; the run's timeline then replaces the card's form.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun OVHCreateCard(
    form: JoinConfigForm?,
    demo: Boolean,
    onAllocate: (() -> Unit)? = null,
    vm: OVHCreateViewModel = viewModel(key = "ovh-create", factory = factory { OVHCreateViewModel(app.nodeLifecycleRepository, app.cloudProviderStore) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val run by app.joinManager.current.collectAsStateWithLifecycle()
    val accounts by vm.accounts.collectAsStateWithLifecycle()
    val account by vm.account.collectAsStateWithLifecycle()
    val options by vm.options.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var adding by remember { mutableStateOf(false) }
    LaunchedEffect(demo) { vm.load(demo) }

    SectionCard(stringResource(R.string.ovh_title)) {
        val current = run
        if (current?.request?.ovh != null) {
            JoinRunView(current, onStop = app.joinManager::stop, onOpenNode = null, onClose = app.joinManager::dismiss, onAllocate = onAllocate)
        } else {
            if (demo) {
                InfoNotice(stringResource(R.string.hetzner_demo))
            } else {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    accounts.forEach { name ->
                        IchorFilterChip(selected = name == account, onClick = { vm.pick(name) }, label = { Text(name) })
                    }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    IchorOutlinedButton(onClick = { adding = true }) { Text(stringResource(R.string.ovh_add_account)) }
                    if (account.isNotBlank()) {
                        IchorTextButton(onClick = { vm.remove(account) }) { Text(stringResource(R.string.ovh_remove)) }
                    }
                }
            }
            when (val state = options) {
                null -> Unit
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(state.message, vm::reload)
                is UiState.Loaded -> InstanceForm(state.data, form, busy = current?.running == true) { request ->
                    // The account is read only now, and goes to the run alone.
                    scope.launch { app.joinManager.start(request, if (account.isBlank()) "" else vm.credential(account)) }
                }
            }
        }
    }
    if (adding) AccountDialog(vm) { adding = false }
}

/** The pickers, the cost line and the confirmed Create. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun InstanceForm(options: OVHOptions, form: JoinConfigForm?, busy: Boolean, onCreate: (JoinRequest) -> Unit) {
    val colors = LocalStatusColors.current
    val authenticated = rememberAuthenticated()
    var region by rememberSaveable { mutableStateOf("") }
    var flavorId by rememberSaveable { mutableStateOf("") }
    var imageId by rememberSaveable { mutableStateOf("") }
    var omniName by rememberSaveable { mutableStateOf("") }
    var confirm by remember { mutableStateOf(false) }

    val flavors = options.flavorsIn(region)
    val images = options.imagesIn(region)
    val flavor = flavors.firstOrNull { it.id == flavorId }
    val image = images.firstOrNull { it.id == imageId }
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
    Text(stringResource(R.string.ovh_region), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.regions.forEach { r -> IchorFilterChip(selected = r == region, onClick = { region = r }, label = { Text(r) }) }
    }
    if (region.isNotBlank()) {
        Text(stringResource(R.string.ovh_flavor), style = MaterialTheme.typography.labelLarge)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            flavors.forEach { f ->
                IchorFilterChip(selected = f.id == flavorId, onClick = { flavorId = f.id }, label = { Text("${f.name} · ${f.vcpus} CPU · ${f.ramGB} GB") })
            }
        }
        Text(stringResource(R.string.ovh_image), style = MaterialTheme.typography.labelLarge)
        if (images.none { it.talos }) InfoNotice(stringResource(R.string.ovh_no_talos_image, region))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            images.forEach { i -> IchorFilterChip(selected = i.id == imageId, onClick = { imageId = i.id }, label = { Text(i.name) }) }
        }
    }
    flavor?.let {
        Text(
            if (it.hourly.isBlank()) stringResource(R.string.ovh_cost_unknown) else stringResource(R.string.ovh_cost, "${it.hourly} ${options.currency}"),
            style = MaterialTheme.typography.titleSmall,
            color = colors.warn,
        )
    }
    IchorButton(
        onClick = { confirm = true },
        enabled = !busy && hostname.isNotEmpty() && form?.complete != false && flavor != null && image != null,
        modifier = Modifier.fillMaxWidth(),
    ) { Text(stringResource(R.string.hetzner_create)) }

    if (confirm && flavor != null && image != null) {
        HostnameConfirmDialog(
            title = stringResource(R.string.ovh_confirm_title, hostname),
            hostname = hostname,
            confirmLabel = stringResource(R.string.hetzner_create),
            onConfirm = {
                confirm = false
                val spec = OVHCreateSpec(
                    region = region, flavorId = flavor.id, imageId = image.id, hostname = hostname,
                    role = form?.role.orEmpty(), templateNode = form?.template.orEmpty(), installDisk = form?.disk.orEmpty(),
                )
                authenticated(hostname) {
                    onCreate(
                        JoinRequest(
                            address = "", template = spec.templateNode, role = spec.role, hostname = hostname, disk = spec.installDisk,
                            staticAddress = "", ovh = spec, omni = form == null,
                        ),
                    )
                }
            },
            onDismiss = { confirm = false },
            emphasized = true,
        ) {
            Text(stringResource(R.string.ovh_confirm_body, flavor.name, region), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

/** A label, the service account (masked), the policy to give it, a test, then sealed. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun AccountDialog(vm: OVHCreateViewModel, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var label by remember { mutableStateOf("") }
    var endpoint by remember { mutableStateOf("eu") }
    var clientId by remember { mutableStateOf("") }
    var secret by remember { mutableStateOf("") }
    var project by remember { mutableStateOf("") }
    var check by remember { mutableStateOf<UiState<OVHCheck>?>(null) }
    var saveFailed by remember { mutableStateOf(false) }
    val tested = (check as? UiState.Loaded)?.data
    val policyTitle = stringResource(R.string.ovh_copy_policy)

    fun credential() = TalosJson.encodeToString(OVHCredential.serializer(), OVHCredential(endpoint, clientId.trim(), secret.trim(), project))

    fun runTest() {
        check = UiState.Loading
        scope.launch {
            val state = uiStateOf { vm.test(credential()) }
            (state as? UiState.Loaded)?.let { project = it.data.project }
            check = state
        }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.ovh_add_account)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.ovh_account_warning), color = colors.warn, style = MaterialTheme.typography.bodySmall)
                MutedText(stringResource(R.string.ovh_policy_hint))
                IchorOutlinedButton(onClick = { scope.launch { copyToClipboard(context, policyTitle, vm.policy(project)) } }) { Text(policyTitle) }
                OutlinedTextField(value = label, onValueChange = { label = it }, label = { Text(stringResource(R.string.hetzner_project_label)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
                Text(stringResource(R.string.ovh_endpoint), style = MaterialTheme.typography.labelLarge)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf("eu" to "EU", "ca" to "CA").forEach { (value, name) ->
                        IchorFilterChip(
                            selected = endpoint == value,
                            onClick = {
                                endpoint = value
                                check = null
                            },
                            label = { Text(name) },
                        )
                    }
                }
                OutlinedTextField(
                    value = clientId,
                    onValueChange = {
                        clientId = it
                        check = null
                    },
                    label = { Text(stringResource(R.string.ovh_client_id)) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = secret,
                    onValueChange = {
                        secret = it
                        check = null
                    },
                    label = { Text(stringResource(R.string.ovh_client_secret)) },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                IchorOutlinedButton(onClick = ::runTest, enabled = clientId.isNotBlank() && secret.isNotBlank() && check != UiState.Loading) {
                    Text(stringResource(R.string.hetzner_test))
                }
                when (val state = check) {
                    null -> Unit
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(state.message, ::runTest)
                    is UiState.Loaded -> {
                        val data = state.data
                        if (data.projects.size > 1) {
                            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                data.projects.forEach { p -> IchorFilterChip(selected = p == project, onClick = { project = p }, label = { Text(p) }) }
                            }
                        }
                        Text(
                            when {
                                !data.checked -> stringResource(R.string.ovh_check_unknown, project)
                                data.canCreate -> stringResource(R.string.ovh_check_ok, project)
                                else -> stringResource(R.string.ovh_check_no)
                            },
                            color = if (data.checked && !data.canCreate) colors.warn else colors.ok,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
                if (saveFailed) Text(stringResource(R.string.hetzner_token_not_saved), color = colors.bad, style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = {
            IchorButton(
                onClick = {
                    scope.launch {
                        if (vm.save(label.trim(), credential())) onDismiss() else saveFailed = true
                    }
                },
                enabled = label.isNotBlank() && tested != null && (tested.canCreate || !tested.checked),
            ) { Text(stringResource(R.string.hetzner_save)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
