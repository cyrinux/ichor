package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
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
import androidx.compose.runtime.setValue
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
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
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
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
import name.levis.ichor.model.AzureImageEstimate
import name.levis.ichor.model.AzureNetworkResult
import name.levis.ichor.model.AzureNetworkSpec
import name.levis.ichor.model.AzureOptions
import name.levis.ichor.model.AzurePrice
import name.levis.ichor.model.AzureSignInChoice
import name.levis.ichor.model.AzureSignInFinish
import name.levis.ichor.model.AzureSignInStart
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
internal enum class AzureCloud(val label: String, val environment: String) {
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

    /** The picked subscription's Microsoft-account sign-in (no token in it), null for a principal. */
    private val _signedIn = MutableStateFlow<AzureCredential?>(null)
    val signedIn: StateFlow<AzureCredential?> = _signedIn.asStateFlow()

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
        _signedIn.value = null
        if (subscription.isBlank()) {
            _group.value = ""
            _options.value = null
            return
        }
        viewModelScope.launch {
            val stored = stored(subscription)
            _group.value = stored?.resourceGroup.orEmpty()
            _signedIn.value = stored?.takeIf { it.microsoftAccount }
            reload()
        }
    }

    fun reload() {
        val subscription = _subscription.value
        viewModelScope.launch {
            _options.value = UiState.Loading
            _options.value = uiStateOf { loadOptions(subscription) }
        }
    }

    /** The options, sealing again the credential the core renewed (a Microsoft account's refresh token). */
    private suspend fun loadOptions(subscription: String): AzureOptions {
        if (subscription.isBlank()) return lifecycle.azureOptions("")
        return lifecycle.azureOptions(credential(subscription)).also { sealRenewed(subscription, it.credential) }
    }

    /**
     * Seals the refresh token the core renewed into the stored credential; "" is nothing to do. Only
     * the token is taken: a resource group or location picked while the call ran stays.
     */
    private suspend fun sealRenewed(subscription: String, updated: String) {
        if (subscription.isBlank() || updated.isBlank()) return
        val token = runCatching { TalosJson.parseToJsonElement(updated).jsonObject["refreshToken"] }.getOrNull() ?: return
        val current = runCatching { TalosJson.parseToJsonElement(credential(subscription)).jsonObject }.getOrNull() ?: return
        withContext(Dispatchers.IO) { store.save(CloudProviderStore.AZURE, subscription, JsonObject(current + ("refreshToken" to token)).toString()) }
    }

    /** The guided network (writes at Azure), then the options again so it is listed. */
    suspend fun prepareNetwork(spec: AzureNetworkSpec): AzureNetworkResult {
        val subscription = _subscription.value
        return lifecycle.azureNetworkPrepare(credential(subscription), spec).also {
            sealRenewed(subscription, it.credential)
            refresh()
        }
    }

    suspend fun signInStart(request: String): AzureSignInStart = lifecycle.azureSignInStart(request)

    suspend fun signInFinish(credential: String): AzureSignInFinish = lifecycle.azureSignInFinish(credential)

    suspend fun signInChoose(credential: String, subscriptionId: String): AzureSignInChoice = lifecycle.azureSignInChoose(credential, subscriptionId)

    fun cancelSignIn() = lifecycle.azureSignInCancel()

    /** Lists the options again, keeping the shown form (and what is picked in it) meanwhile. */
    fun refresh() {
        val subscription = _subscription.value
        if (subscription.isBlank()) return
        viewModelScope.launch {
            val state = uiStateOf { loadOptions(subscription) }
            if (state is UiState.Loaded) _options.value = state
        }
    }

    /** The cost line of an image run. Creates nothing. */
    suspend fun estimate(arch: String, location: String): AzureImageEstimate =
        lifecycle.azureImageEstimate(credential(_subscription.value), arch, location)

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
            if (reseal(subscription, "resourceGroup" to name, "location" to location) && location != listed) reload()
        }
    }

    /** The same principal listing another location. */
    fun switchLocation(location: String) {
        val subscription = _subscription.value
        if (subscription.isBlank()) return
        viewModelScope.launch {
            if (reseal(subscription, "location" to location)) reload()
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

    /** Seals the stored credential with [fields] changed, edited as JSON: a sign-in holds a token [AzureCredential] does not keep. */
    private suspend fun reseal(subscription: String, vararg fields: Pair<String, String>): Boolean {
        val current = runCatching { TalosJson.parseToJsonElement(credential(subscription)).jsonObject }.getOrNull() ?: return false
        val updated = JsonObject(current + fields.map { (k, v) -> k to JsonPrimitive(v) }).toString()
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
    val signedIn by vm.signedIn.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var adding by remember { mutableStateOf(false) }
    var imageFor by remember { mutableStateOf<AzureImageAsk?>(null) }
    var madeImage by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(demo) { vm.load(demo) }

    SectionCard(stringResource(R.string.azure_title)) {
        val current = run
        if (current?.request?.azure != null) {
            JoinRunView(current, onStop = app.joinManager::stop, onOpenNode = null, onClose = app.joinManager::dismiss, onAllocate = onAllocate)
        } else {
            if (current?.request?.azureImage != null) {
                ImageRunView(current, onStop = app.joinManager::stop) {
                    madeImage = current.madeImage?.imageId ?: madeImage
                    app.joinManager.dismiss()
                    vm.refresh()
                }
            }
            if (demo) {
                InfoNotice(stringResource(R.string.hetzner_demo))
            } else {
                if (subscriptions.isNotEmpty()) Text(stringResource(R.string.azure_subscription), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    subscriptions.forEach { id ->
                        IchorFilterChip(selected = id == subscription, onClick = { vm.pick(id) }, label = { Text(id) })
                    }
                }
                signedIn?.let { MutedText(stringResource(R.string.azure_signin_signed_in, it.account)) }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    IchorOutlinedButton(onClick = { adding = true }) { Text(stringResource(R.string.azure_add_account)) }
                    if (subscription.isNotBlank()) {
                        IchorTextButton(onClick = { vm.remove(subscription) }) {
                            Text(stringResource(if (signedIn != null) R.string.kube_signin_sign_out else R.string.azure_remove))
                        }
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
                    canMakeImage = !demo && subscription.isNotBlank(),
                    madeImage = madeImage,
                    onMakeImage = { imageFor = it },
                    onPrepareNetwork = if (demo || subscription.isBlank()) null else vm::prepareNetwork,
                ) { request ->
                    // The principal is read only now, and goes to the run alone.
                    scope.launch { app.joinManager.start(request, if (subscription.isBlank()) "" else vm.credential(subscription)) }
                }
            }
        }
    }
    if (adding) PrincipalDialog(vm) { adding = false }
    imageFor?.let { ask ->
        AzureImageSheet(
            ask = ask,
            template = form?.template.orEmpty(),
            estimate = vm::estimate,
            onStart = { spec ->
                imageFor = null
                val request = JoinRequest(
                    address = "", template = spec.templateNode, role = "", hostname = "Talos ${spec.arch}", disk = "",
                    staticAddress = "", azureImage = spec,
                )
                // The principal is read only now, and goes to the run alone.
                scope.launch { app.joinManager.start(request, vm.credential(subscription)) }
            },
            onDismiss = { imageFor = null },
        )
    }
}

/** "Standard_B2s · 2 CPU · 4 GB". */
private fun sizeLabel(s: AzureSize): String {
    val memory = if (s.memoryGB % 1.0 == 0.0) s.memoryGB.toLong().toString() else s.memoryGB.toString()
    return "${s.name} · ${s.vcpus} CPU · $memory GB"
}

/** "nodes · 10.0.0.0/24", then "NAT gateway" or "no NSG" when they apply. */
@Composable
internal fun subnetLabel(s: AzureSubnet): String {
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
    canMakeImage: Boolean,
    /** The image the last run made: picked once it is listed. */
    madeImage: String,
    onMakeImage: (AzureImageAsk) -> Unit,
    /** The guided network; null where it cannot run (the demo, no account). */
    onPrepareNetwork: (suspend (AzureNetworkSpec) -> AzureNetworkResult)?,
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
    LaunchedEffect(madeImage, options) {
        // Another architecture or generation than the picked size's: the size is picked again.
        val made = options.images.firstOrNull { it.id == madeImage } ?: return@LaunchedEffect
        if (options.sizesFor(made).none { it.name == sizeName }) sizeName = ""
        imageId = made.id
    }

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
    if (options.images.isEmpty() && !canMakeImage) InfoNotice(stringResource(R.string.azure_no_image))
    if (canMakeImage) {
        if (options.images.isEmpty()) InfoNotice(stringResource(R.string.azure_image_missing, options.locationLabel(options.location)))
        IchorOutlinedButton(
            onClick = {
                if (resourceGroup != null) {
                    onMakeImage(AzureImageAsk(options, resourceGroup.name, size?.arch ?: image?.arch.orEmpty(), networkId, subnetId, nsgId))
                }
            },
            enabled = !busy && resourceGroup != null,
            modifier = Modifier.fillMaxWidth(),
        ) { Text(stringResource(if (options.images.isEmpty()) R.string.azure_make_image else R.string.azure_image_make_another)) }
        if (resourceGroup == null) MutedText(stringResource(R.string.azure_image_need_group))
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
    if (onPrepareNetwork != null) {
        AzureGuidedNetwork(group, options.location, omni = form == null, noNetwork = options.networks.isEmpty(), enabled = !busy, prepare = onPrepareNetwork) {
            // Its network, subnet and group fill the pickers once listed; the VM gets a public address.
            networkId = it.networkId
            subnetId = it.subnetId
            nsgId = it.securityGroupId
            publicIp = true
        }
    }
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
        (nsg ?: subnetNsg)?.takeIf { it.wideOpen }?.let { open ->
            val rule = open.wideOpenRule.ifBlank { null }?.let { " ($it)" }.orEmpty()
            Text(stringResource(R.string.azure_nsg_wide_open) + rule, color = colors.warn, style = MaterialTheme.typography.bodySmall)
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
