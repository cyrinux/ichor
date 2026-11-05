package name.levis.ichor.ui.join

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenu
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
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
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.AZURE_DEFAULT_DISK_GB
import name.levis.ichor.model.AzureCheck
import name.levis.ichor.model.AzureCreateSpec
import name.levis.ichor.model.AzureCredential
import name.levis.ichor.model.AzureOptions
import name.levis.ichor.model.AzurePrice
import name.levis.ichor.model.AzureSize
import name.levis.ichor.model.AzureSubnet
import name.levis.ichor.model.azureDiskGB
import name.levis.ichor.model.locationLabel
import name.levis.ichor.model.locationOf
import name.levis.ichor.model.sizesFor
import name.levis.ichor.model.subnetsIn
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
import name.levis.ichor.ui.design.IchorSwitch
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** The Azure clouds a principal can belong to; [environment] is the core's name ("" = public). */
private enum class AzureCloud(val label: String, val environment: String) {
    PUBLIC("Azure", ""),
    CHINA("Azure China", "AzureChinaCloud"),
    US_GOVERNMENT("Azure US Government", "AzureUSGovernmentCloud"),
}

/**
 * The stored service principals (one per subscription id), the resource group picked and what a
 * VM can be created from. The secret never reaches the screen.
 */
class AzureCreateViewModel(private val lifecycle: NodeLifecycleRepository, private val store: CloudProviderStore) : ViewModel() {
    private val _subscriptions = MutableStateFlow<List<String>>(emptyList())
    val subscriptions: StateFlow<List<String>> = _subscriptions.asStateFlow()
    private val _subscription = MutableStateFlow("")
    val subscription: StateFlow<String> = _subscription.asStateFlow()
    private val _group = MutableStateFlow("")
    val group: StateFlow<String> = _group.asStateFlow()
    private val _options = MutableStateFlow<UiState<AzureOptions>?>(null)
    val options: StateFlow<UiState<AzureOptions>?> = _options.asStateFlow()
    private val _price = MutableStateFlow<UiState<AzurePrice>?>(null)
    val price: StateFlow<UiState<AzurePrice>?> = _price.asStateFlow()
    private var priceJob: Job? = null

    /** Reads the stored principals; the demo lists its samples without any. */
    fun load(demo: Boolean) {
        viewModelScope.launch {
            val stored = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.AZURE) }
            _subscriptions.value = stored
            when {
                demo -> reload()
                _subscription.value !in stored -> pick(stored.firstOrNull().orEmpty())
            }
        }
    }

    fun pick(subscription: String) {
        _subscription.value = subscription
        _price.value = null
        if (subscription.isBlank()) {
            _group.value = ""
            _options.value = null
            return
        }
        viewModelScope.launch {
            _group.value = stored(subscription)?.resourceGroup.orEmpty()
            reload()
        }
    }

    fun reload() {
        val subscription = _subscription.value
        viewModelScope.launch {
            _options.value = UiState.Loading
            _options.value = uiStateOf { lifecycle.azureOptions(if (subscription.isBlank()) "" else credential(subscription)) }
        }
    }

    /** Asks the Retail Prices API for [size] in [location]; a failure is "price unavailable". */
    fun loadPrice(location: String, size: String) {
        val subscription = _subscription.value
        priceJob?.cancel()
        if (size.isBlank()) {
            _price.value = null
            return
        }
        priceJob = viewModelScope.launch {
            _price.value = UiState.Loading
            val state = uiStateOf { lifecycle.azurePrice(if (subscription.isBlank()) "" else credential(subscription), location, size) }
            _price.value = if (state is UiState.Failed) UiState.Loaded(AzurePrice()) else state
        }
    }

    /**
     * The resource group the VM goes in: kept with the principal (Test checks it there), and the
     * group's location listed when it is another one. The demo only remembers it.
     */
    fun chooseGroup(name: String, location: String, listed: String) {
        _group.value = name
        val subscription = _subscription.value
        if (subscription.isBlank()) return
        viewModelScope.launch {
            if (reseal(subscription) { it.copy(resourceGroup = name, location = location) } && location != listed) reload()
        }
    }

    /** The same principal listing another location. */
    fun switchLocation(location: String) {
        val subscription = _subscription.value
        if (subscription.isBlank()) return
        viewModelScope.launch {
            if (reseal(subscription) { it.copy(location = location) }) reload()
        }
    }

    suspend fun credential(subscription: String): String = withContext(Dispatchers.IO) { store.token(CloudProviderStore.AZURE, subscription) }

    suspend fun test(credentialJson: String): AzureCheck = lifecycle.azureTest(credentialJson)

    suspend fun role(subscription: String, resourceGroup: String): String = lifecycle.azureRole(subscription, resourceGroup)

    /** Seals the principal under its subscription id and picks it; false when the store cannot be read. */
    suspend fun save(subscription: String, credentialJson: String): Boolean {
        if (!withContext(Dispatchers.IO) { store.save(CloudProviderStore.AZURE, subscription, credentialJson) }) return false
        _subscriptions.value = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.AZURE) }
        pick(subscription)
        return true
    }

    fun remove(subscription: String) {
        viewModelScope.launch {
            withContext(Dispatchers.IO) { store.save(CloudProviderStore.AZURE, subscription, "") }
            val left = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.AZURE) }
            _subscriptions.value = left
            pick(left.firstOrNull().orEmpty())
        }
    }

    private suspend fun stored(subscription: String): AzureCredential? =
        runCatching { TalosJson.decodeFromString(AzureCredential.serializer(), credential(subscription)) }.getOrNull()

    private suspend fun reseal(subscription: String, change: (AzureCredential) -> AzureCredential): Boolean {
        val current = stored(subscription) ?: return false
        val updated = TalosJson.encodeToString(AzureCredential.serializer(), change(current))
        return withContext(Dispatchers.IO) { store.save(CloudProviderStore.AZURE, subscription, updated) }
    }
}

/**
 * "Create the VM at Azure": the run makes the custom data itself. [form] is the new member's
 * (null on Omni, which only needs a name). The price, the typed name and the app lock come before
 * the billable create; the run's timeline then replaces the card's form.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun AzureCreateCard(
    form: JoinConfigForm?,
    /** On Omni, the labels to sign into the join token; "" on plain Talos. */
    labelsJson: String,
    demo: Boolean,
    onAllocate: (() -> Unit)? = null,
    vm: AzureCreateViewModel = viewModel(key = "azure-create", factory = factory { AzureCreateViewModel(app.nodeLifecycleRepository, app.cloudProviderStore) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val run by app.joinManager.current.collectAsStateWithLifecycle()
    val subscriptions by vm.subscriptions.collectAsStateWithLifecycle()
    val subscription by vm.subscription.collectAsStateWithLifecycle()
    val group by vm.group.collectAsStateWithLifecycle()
    val options by vm.options.collectAsStateWithLifecycle()
    val price by vm.price.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var adding by remember { mutableStateOf(false) }
    LaunchedEffect(demo) { vm.load(demo) }

    SectionCard(stringResource(R.string.azure_title)) {
        val current = run
        if (current?.request?.azure != null) {
            JoinRunView(current, onStop = app.joinManager::stop, onOpenNode = null, onClose = app.joinManager::dismiss, onAllocate = onAllocate)
        } else {
            if (demo) {
                InfoNotice(stringResource(R.string.hetzner_demo))
            } else {
                if (subscriptions.isNotEmpty()) Text(stringResource(R.string.azure_subscription), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    subscriptions.forEach { id ->
                        IchorFilterChip(selected = id == subscription, onClick = { vm.pick(id) }, label = { Text(id) })
                    }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    IchorOutlinedButton(onClick = { adding = true }) { Text(stringResource(R.string.azure_add_principal)) }
                    if (subscription.isNotBlank()) {
                        IchorTextButton(onClick = { vm.remove(subscription) }) { Text(stringResource(R.string.azure_remove)) }
                    }
                }
            }
            when (val state = options) {
                null -> Unit
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(state.message, vm::reload)
                is UiState.Loaded -> VMForm(
                    state.data, group, form, labelsJson, busy = current?.running == true, price = price,
                    onGroup = { name -> vm.chooseGroup(name, state.data.locationOf(name), state.data.location) },
                    onLocation = if (demo) null else vm::switchLocation,
                    onSize = { size -> vm.loadPrice(state.data.location, size) },
                ) { request ->
                    // The principal is read only now, and goes to the run alone.
                    scope.launch { app.joinManager.start(request, if (subscription.isBlank()) "" else vm.credential(subscription)) }
                }
            }
        }
    }
    if (adding) PrincipalDialog(vm) { adding = false }
}

/** "Standard_B2s · 2 CPU · 4 GB". */
private fun sizeLabel(s: AzureSize): String {
    val memory = if (s.memoryGB % 1.0 == 0.0) s.memoryGB.toLong().toString() else s.memoryGB.toString()
    return "${s.name} · ${s.vcpus} CPU · $memory GB"
}

/** "nodes · 10.0.0.0/24", then "NAT gateway" or "no NSG" when they apply. */
@Composable
private fun subnetLabel(s: AzureSubnet): String {
    val parts = listOfNotNull(
        s.name.ifBlank { s.id },
        s.cidr.ifBlank { null },
        if (s.natGateway) stringResource(R.string.azure_subnet_nat) else null,
        if (s.nsgId.isBlank()) stringResource(R.string.azure_subnet_no_nsg) else null,
    )
    return parts.joinToString(" · ")
}

/**
 * The pickers (resource group → location; image → the sizes that boot it; network → its subnets;
 * the security group), the public address, the disk, the price line and the confirmed Create.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun VMForm(
    options: AzureOptions,
    group: String,
    form: JoinConfigForm?,
    /** On Omni, the labels to sign into the join token; "" on plain Talos. */
    labelsJson: String,
    busy: Boolean,
    price: UiState<AzurePrice>?,
    onGroup: (String) -> Unit,
    onLocation: ((String) -> Unit)?,
    onSize: (String) -> Unit,
    onCreate: (JoinRequest) -> Unit,
) {
    val colors = LocalStatusColors.current
    val authenticated = rememberAuthenticated()
    var imageId by rememberSaveable(options.location) { mutableStateOf("") }
    var sizeName by rememberSaveable(options.location) { mutableStateOf("") }
    var networkId by rememberSaveable(options.location) { mutableStateOf("") }
    var subnetId by rememberSaveable(options.location) { mutableStateOf("") }
    var nsgId by rememberSaveable(options.location) { mutableStateOf("") }
    var publicIp by rememberSaveable(options.location) { mutableStateOf(true) }
    var disk by rememberSaveable { mutableStateOf(AZURE_DEFAULT_DISK_GB.toString()) }
    var omniName by rememberSaveable { mutableStateOf("") }
    var confirm by remember { mutableStateOf(false) }

    val image = options.images.firstOrNull { it.id == imageId }
    val sizes = image?.let { options.sizesFor(it) }.orEmpty()
    val size = sizes.firstOrNull { it.name == sizeName }
    val subnets = options.subnetsIn(networkId)
    val subnet = subnets.firstOrNull { it.id == subnetId }
    val nsg = options.securityGroups.firstOrNull { it.id == nsgId }
    val subnetNsg = options.securityGroups.firstOrNull { it.id == subnet?.nsgId }
    val resourceGroup = options.resourceGroups.firstOrNull { it.name == group }
    val diskGB = azureDiskGB(disk)
    val hostname = form?.hostname?.trim() ?: omniName.trim()
    LaunchedEffect(sizeName) { onSize(sizeName) }

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
    Text(stringResource(R.string.azure_resource_group), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.resourceGroups.forEach { g ->
            IchorFilterChip(selected = g.name == group, onClick = { if (g.name != group) onGroup(g.name) }, label = { Text("${g.name} · ${g.location}") })
        }
    }
    if (onLocation != null && options.locations.size > 1) {
        Choice(
            label = stringResource(R.string.azure_location),
            selected = options.locationLabel(options.location),
            items = options.locations.map { it.name to options.locationLabel(it.name) },
        ) { if (it != options.location) onLocation(it) }
    } else {
        Text(stringResource(R.string.azure_location), style = MaterialTheme.typography.labelLarge)
        Text(options.locationLabel(options.location), style = MaterialTheme.typography.bodyMedium)
    }
    options.warnings.forEach { InfoNotice(it) }

    Text(stringResource(R.string.azure_image), style = MaterialTheme.typography.labelLarge)
    if (options.images.isEmpty()) {
        InfoNotice(stringResource(R.string.azure_no_image))
        IchorOutlinedButton(onClick = {}, enabled = false) { Text(stringResource(R.string.azure_make_image)) }
        MutedText(stringResource(R.string.azure_make_image_soon))
    }
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.images.forEach { i ->
            IchorFilterChip(
                selected = i.id == imageId,
                onClick = {
                    // Another architecture or generation: the picked size may not boot it.
                    if (options.sizesFor(i).none { it.name == sizeName }) sizeName = ""
                    imageId = i.id
                },
                label = { Text("${i.name.ifBlank { i.id }} · ${i.arch} · ${i.generation}") },
            )
        }
    }
    if (image != null) {
        if (sizes.isEmpty()) {
            InfoNotice(stringResource(R.string.azure_no_size, image.arch, image.generation))
        } else {
            Choice(
                label = stringResource(R.string.azure_size),
                selected = size?.let(::sizeLabel).orEmpty(),
                items = sizes.map { it.name to sizeLabel(it) },
            ) { sizeName = it }
        }
    }

    Text(stringResource(R.string.azure_network), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.networks.forEach { n ->
            IchorFilterChip(
                selected = n.id == networkId,
                onClick = {
                    if (n.id != networkId) subnetId = ""
                    networkId = n.id
                },
                label = { Text("${n.name} · ${n.resourceGroup}") },
            )
        }
    }
    if (networkId.isNotBlank()) {
        Text(stringResource(R.string.azure_subnet), style = MaterialTheme.typography.labelLarge)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            subnets.forEach { s ->
                IchorFilterChip(
                    selected = s.id == subnetId,
                    onClick = {
                        subnetId = s.id
                        // A NAT gateway already reaches the Internet: no address needed.
                        publicIp = !s.natGateway
                    },
                    label = { Text(subnetLabel(s)) },
                )
            }
        }
    }
    if (subnet != null) {
        Text(stringResource(R.string.azure_nsg), style = MaterialTheme.typography.labelLarge)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            options.securityGroups.forEach { g ->
                IchorFilterChip(
                    selected = g.id == nsgId,
                    onClick = { nsgId = if (g.id == nsgId) "" else g.id },
                    label = { Text("${g.name} · ${g.resourceGroup}") },
                )
            }
        }
        if (nsg == null) MutedText(stringResource(R.string.azure_nsg_none))
        if ((nsg ?: subnetNsg)?.wideOpen == true) {
            Text(stringResource(R.string.azure_nsg_wide_open), color = colors.warn, style = MaterialTheme.typography.bodySmall)
        }
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.azure_public_ip), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f).padding(end = 8.dp))
            IchorSwitch(checked = publicIp, onCheckedChange = { publicIp = it })
        }
        OutlinedTextField(
            value = disk,
            onValueChange = { disk = it.filter(Char::isDigit) },
            label = { Text(stringResource(R.string.azure_disk)) },
            singleLine = true,
            isError = diskGB == null,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            modifier = Modifier.fillMaxWidth(),
        )
    }

    if (size != null) {
        when (val state = price) {
            null, UiState.Loading -> LoadingBox()
            is UiState.Failed -> Unit
            is UiState.Loaded -> Text(
                if (state.data.hourly.isBlank()) stringResource(R.string.azure_cost_unknown) else stringResource(R.string.aws_cost, state.data.hourly),
                style = MaterialTheme.typography.titleSmall,
                color = colors.warn,
            )
        }
    }
    val ready = resourceGroup != null && image != null && size != null && subnet != null && diskGB != null
    IchorButton(
        onClick = { confirm = true },
        enabled = !busy && hostname.isNotEmpty() && form?.complete != false && ready,
        modifier = Modifier.fillMaxWidth(),
    ) { Text(stringResource(R.string.hetzner_create)) }

    if (confirm && resourceGroup != null && image != null && size != null && subnet != null && diskGB != null) {
        HostnameConfirmDialog(
            title = stringResource(R.string.azure_confirm_title, hostname),
            hostname = hostname,
            confirmLabel = stringResource(R.string.hetzner_create),
            onConfirm = {
                confirm = false
                val spec = AzureCreateSpec(
                    resourceGroup = resourceGroup.name, location = options.location, size = size.name, imageId = image.id,
                    subnetId = subnet.id, securityGroupId = nsg?.id.orEmpty(), associatePublicIp = publicIp, osDiskGB = diskGB,
                    hostname = hostname, role = form?.role.orEmpty(), templateNode = form?.template.orEmpty(), installDisk = form?.disk.orEmpty(), labelsJson = labelsJson,
                )
                authenticated(hostname) {
                    onCreate(
                        JoinRequest(
                            address = "", template = spec.templateNode, role = spec.role, hostname = hostname, disk = spec.installDisk,
                            staticAddress = "", azure = spec, omni = form == null,
                        ),
                    )
                }
            },
            onDismiss = { confirm = false },
            emphasized = true,
        ) {
            Text(stringResource(R.string.azure_confirm_body, size.name, options.locationLabel(options.location)), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

/** A read-only field that opens a menu of [items] (value to label); for the long lists. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Choice(label: String, selected: String, items: List<Pair<String, String>>, onPick: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(expanded = open, onExpandedChange = { open = it }) {
        OutlinedTextField(
            value = selected,
            onValueChange = {},
            readOnly = true,
            singleLine = true,
            label = { Text(label) },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = open) },
            modifier = Modifier.fillMaxWidth().menuAnchor(ExposedDropdownMenuAnchorType.PrimaryNotEditable),
        )
        ExposedDropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            items.forEach { (value, text) ->
                DropdownMenuItem(
                    text = { Text(text) },
                    onClick = {
                        open = false
                        onPick(value)
                    },
                    contentPadding = ExposedDropdownMenuDefaults.ItemContentPadding,
                )
            }
        }
    }
}

/**
 * The principal's ids, its secret (masked), its cloud and resource group, the role to give it, a
 * test, then sealed under the subscription id.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun PrincipalDialog(vm: AzureCreateViewModel, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var tenantId by remember { mutableStateOf("") }
    var clientId by remember { mutableStateOf("") }
    var secret by remember { mutableStateOf("") }
    var subscriptionId by remember { mutableStateOf("") }
    var resourceGroup by remember { mutableStateOf("") }
    var cloud by remember { mutableStateOf(AzureCloud.PUBLIC) }
    var check by remember { mutableStateOf<UiState<AzureCheck>?>(null) }
    var saveFailed by remember { mutableStateOf(false) }
    val tested = (check as? UiState.Loaded)?.data
    val roleTitle = stringResource(R.string.azure_copy_role)

    fun credential() = TalosJson.encodeToString(
        AzureCredential.serializer(),
        AzureCredential(tenantId.trim(), clientId.trim(), secret.trim(), subscriptionId.trim(), cloud.environment, resourceGroup.trim()),
    )

    fun runTest() {
        check = UiState.Loading
        scope.launch { check = uiStateOf { vm.test(credential()) } }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.azure_add_principal)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.azure_warning), color = colors.warn, style = MaterialTheme.typography.bodySmall)
                MutedText(stringResource(R.string.azure_role_hint))
                IchorOutlinedButton(onClick = { scope.launch { copyToClipboard(context, roleTitle, vm.role(subscriptionId.trim(), resourceGroup.trim())) } }) {
                    Text(roleTitle)
                }
                Text(stringResource(R.string.azure_cloud), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    AzureCloud.entries.forEach { c ->
                        IchorFilterChip(
                            selected = c == cloud,
                            onClick = {
                                cloud = c
                                check = null
                            },
                            label = { Text(c.label) },
                        )
                    }
                }
                IdField(tenantId, R.string.azure_tenant_id, secret = false) {
                    tenantId = it
                    check = null
                }
                IdField(clientId, R.string.azure_client_id, secret = false) {
                    clientId = it
                    check = null
                }
                IdField(secret, R.string.azure_client_secret, secret = true) {
                    secret = it
                    check = null
                }
                IdField(subscriptionId, R.string.azure_subscription_id, secret = false) {
                    subscriptionId = it
                    check = null
                }
                IdField(resourceGroup, R.string.azure_resource_group, secret = false) {
                    resourceGroup = it
                    check = null
                }
                MutedText(stringResource(R.string.azure_resource_group_hint))
                IchorOutlinedButton(
                    onClick = ::runTest,
                    enabled = tenantId.isNotBlank() && clientId.isNotBlank() && secret.isNotBlank() && subscriptionId.isNotBlank() && check != UiState.Loading,
                ) {
                    Text(stringResource(R.string.hetzner_test))
                }
                when (val state = check) {
                    null -> Unit
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(state.message, ::runTest)
                    is UiState.Loaded -> {
                        val data = state.data
                        val name = data.subscriptionName.ifBlank { data.subscription }
                        val where = resourceGroup.trim().ifBlank { name }
                        Text(
                            when {
                                !data.checked -> stringResource(R.string.azure_check_unknown, name, where)
                                data.canCreate -> stringResource(R.string.azure_check_ok, name, where)
                                else -> stringResource(R.string.azure_check_no, name, where)
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
                    val subscription = tested?.subscription.orEmpty()
                    scope.launch {
                        if (vm.save(subscription, credential())) onDismiss() else saveFailed = true
                    }
                },
                enabled = tested != null && tested.subscription.isNotBlank() && (tested.canCreate || !tested.checked),
            ) { Text(stringResource(R.string.hetzner_save)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** One field of the principal dialog; the secret is masked and never suggested. */
@Composable
private fun IdField(value: String, @StringRes label: Int, secret: Boolean, onChange: (String) -> Unit) {
    OutlinedTextField(
        value = value,
        onValueChange = onChange,
        label = { Text(stringResource(label)) },
        singleLine = true,
        visualTransformation = if (secret) PasswordVisualTransformation() else VisualTransformation.None,
        keyboardOptions = KeyboardOptions(keyboardType = if (secret) KeyboardType.Password else KeyboardType.Ascii, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
}
