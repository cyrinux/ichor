package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
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
import kotlinx.coroutines.flow.Flow
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
import name.levis.ichor.data.SignInEvent
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.GCPCheck
import name.levis.ichor.model.GCPCreateSpec
import name.levis.ichor.model.GCPCredential
import name.levis.ichor.model.GCPImageEstimate
import name.levis.ichor.model.GCPMachineType
import name.levis.ichor.model.GCPNetworkResult
import name.levis.ichor.model.GCPNetworkSpec
import name.levis.ichor.model.GCPOptions
import name.levis.ichor.model.GCPPrice
import name.levis.ichor.model.GCPSignInResult
import name.levis.ichor.model.GCP_DEFAULT_DISK_GB
import name.levis.ichor.model.GCP_MAX_DISK_GB
import name.levis.ichor.model.GCP_MIN_DISK_GB
import name.levis.ichor.model.arches
import name.levis.ichor.model.gcpCredentialWith
import name.levis.ichor.model.gcpMergeRenewed
import name.levis.ichor.model.gcpValidName
import name.levis.ichor.model.imagesFor
import name.levis.ichor.model.natWarnings
import name.levis.ichor.model.otherWarnings
import name.levis.ichor.model.typesFor
import name.levis.ichor.model.usableNetworks
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

/**
 * The stored service account keys or Google sign-ins (one per project id, with the picked project
 * and zone) and what a VM can be created from. The key or sign-in never reaches the screen.
 */
class GCPCreateViewModel(private val lifecycle: NodeLifecycleRepository, private val store: CloudProviderStore) : ViewModel() {
    private val _accounts = MutableStateFlow<List<String>>(emptyList())
    val accounts: StateFlow<List<String>> = _accounts.asStateFlow()
    private val _account = MutableStateFlow("")
    val account: StateFlow<String> = _account.asStateFlow()
    private val _options = MutableStateFlow<UiState<GCPOptions>?>(null)
    val options: StateFlow<UiState<GCPOptions>?> = _options.asStateFlow()
    private val _price = MutableStateFlow<UiState<GCPPrice>?>(null)
    val price: StateFlow<UiState<GCPPrice>?> = _price.asStateFlow()
    private var priceJob: Job? = null

    /** The picked project's Google sign-in (its state is opaque, never shown), null for a key. */
    private val _signedIn = MutableStateFlow<GCPCredential?>(null)
    val signedIn: StateFlow<GCPCredential?> = _signedIn.asStateFlow()

    /** Reads the stored keys; the demo lists its samples without any. */
    fun load(demo: Boolean) {
        viewModelScope.launch {
            val stored = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.GCP) }
            _accounts.value = stored
            when {
                demo -> reload()
                _account.value !in stored -> pick(stored.firstOrNull().orEmpty())
            }
        }
    }

    fun pick(account: String) {
        _account.value = account
        _price.value = null
        _signedIn.value = null
        if (account.isBlank()) {
            _options.value = null
            return
        }
        viewModelScope.launch {
            _signedIn.value = runCatching { TalosJson.decodeFromString(GCPCredential.serializer(), credential(account)) }
                .getOrNull()?.takeIf { it.googleAccount }
            reload()
        }
    }

    fun reload() {
        val account = _account.value
        viewModelScope.launch {
            _options.value = UiState.Loading
            _options.value = uiStateOf { loadOptions(account) }
        }
    }

    /** The options, sealing again the Google sign-in the core renewed. */
    private suspend fun loadOptions(account: String): GCPOptions {
        if (account.isBlank()) return lifecycle.gcpOptions("")
        return lifecycle.gcpOptions(credential(account)).also { sealRenewed(account, it.credential) }
    }

    /**
     * Seals the sign-in state the core renewed into the stored credential; "" is nothing to do.
     * Only "googleSignIn" is taken: a project or zone picked while the call ran stays.
     */
    private suspend fun sealRenewed(account: String, renewed: String) {
        val merged = gcpMergeRenewed(credential(account), renewed) ?: return
        withContext(Dispatchers.IO) { store.save(CloudProviderStore.GCP, account, merged) }
    }

    suspend fun signInChoices(): List<String> = lifecycle.gcpSignInChoices()

    fun signInStart(kind: String, secrets: Map<String, String>): Flow<SignInEvent> = lifecycle.gcpSignInStart(kind, secrets)

    fun signInComplete(code: String) {
        if (code.isNotBlank()) lifecycle.gcpSignInComplete(code.trim())
    }

    suspend fun signInCredential(): GCPSignInResult = lifecycle.gcpSignInCredential()

    /** Asks the Billing Catalog for [machineType] in [zone]; a failure (the demo has no key) is "price unavailable". */
    fun loadPrice(zone: String, machineType: String) {
        val account = _account.value
        priceJob?.cancel()
        if (machineType.isBlank() || zone.isBlank()) {
            _price.value = null
            return
        }
        priceJob = viewModelScope.launch {
            _price.value = UiState.Loading
            val state = uiStateOf { lifecycle.gcpPrice(if (account.isBlank()) "" else credential(account), zone, machineType) }
            _price.value = if (state is UiState.Failed) UiState.Loaded(GCPPrice()) else state
        }
    }

    /** The same key in another project or zone: re-sealed with them, then their options. */
    fun switchPlace(project: String? = null, zone: String? = null) {
        val account = _account.value
        if (account.isBlank()) return
        // Edited as JSON: a Google sign-in holds a state GCPCredential does not keep.
        val fields = listOfNotNull(project?.let { "project" to it }, zone?.let { "zone" to it }).toTypedArray()
        viewModelScope.launch {
            val updated = gcpCredentialWith(credential(account), *fields) ?: return@launch
            if (withContext(Dispatchers.IO) { store.save(CloudProviderStore.GCP, account, updated) }) pick(account)
        }
    }

    /** Lists the options again, keeping the shown form (and what is picked in it) meanwhile. */
    fun refresh() {
        val account = _account.value
        if (account.isBlank()) return
        viewModelScope.launch {
            val state = uiStateOf { loadOptions(account) }
            if (state is UiState.Loaded) _options.value = state
        }
    }

    /**
     * The guided network (writes at Google Cloud), sealing again the Google sign-in the core
     * renewed, then the options again so its flags are listed.
     */
    suspend fun prepareNetwork(spec: GCPNetworkSpec): GCPNetworkResult {
        val account = _account.value
        return lifecycle.gcpNetworkPrepare(credential(account), spec).also {
            sealRenewed(account, it.credential)
            refresh()
        }
    }

    /** The cost line of an image run. Creates nothing. */
    suspend fun estimate(arch: String, zone: String): GCPImageEstimate =
        lifecycle.gcpImageEstimate(credential(_account.value), arch, zone)

    suspend fun credential(account: String): String = withContext(Dispatchers.IO) { store.token(CloudProviderStore.GCP, account) }

    suspend fun test(credentialJson: String): GCPCheck = lifecycle.gcpTest(credentialJson)

    suspend fun role(project: String): String = lifecycle.gcpRole(project)

    /** Seals the key under its project id and picks it; false when the store cannot be read. */
    suspend fun save(account: String, credentialJson: String): Boolean {
        if (!withContext(Dispatchers.IO) { store.save(CloudProviderStore.GCP, account, credentialJson) }) return false
        _accounts.value = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.GCP) }
        pick(account)
        return true
    }

    fun remove(account: String) {
        viewModelScope.launch {
            withContext(Dispatchers.IO) { store.save(CloudProviderStore.GCP, account, "") }
            val left = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.GCP) }
            _accounts.value = left
            pick(left.firstOrNull().orEmpty())
        }
    }
}

/**
 * "Create the VM at Google Cloud": the run makes the user data itself. [form] is the new member's
 * (null on Omni, which only needs a name). The price, the typed name and the app lock come before
 * the billable create; the run's timeline then replaces the card's form.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun GCPCreateCard(
    form: JoinConfigForm?,
    /** On Omni, the labels to sign into the join token; "" on plain Talos. */
    labelsJson: String,
    demo: Boolean,
    onAllocate: (() -> Unit)? = null,
    vm: GCPCreateViewModel = viewModel(key = "gcp-create", factory = factory { GCPCreateViewModel(app.nodeLifecycleRepository, app.cloudProviderStore) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val run by app.joinManager.current.collectAsStateWithLifecycle()
    val accounts by vm.accounts.collectAsStateWithLifecycle()
    val account by vm.account.collectAsStateWithLifecycle()
    val options by vm.options.collectAsStateWithLifecycle()
    val price by vm.price.collectAsStateWithLifecycle()
    val signedIn by vm.signedIn.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var adding by remember { mutableStateOf(false) }
    var imageFor by remember { mutableStateOf<GCPImageAsk?>(null) }
    var madeImage by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(demo) { vm.load(demo) }

    SectionCard(stringResource(R.string.gcp_title)) {
        val current = run
        if (current?.request?.gcp != null) {
            JoinRunView(current, onStop = app.joinManager::stop, onOpenNode = null, onClose = app.joinManager::dismiss, onAllocate = onAllocate)
        } else {
            if (current?.request?.gcpImage != null) {
                ImageRunView(current, onStop = app.joinManager::stop) {
                    madeImage = current.madeImage?.imageId ?: madeImage
                    app.joinManager.dismiss()
                    vm.refresh()
                }
            }
            if (demo) {
                InfoNotice(stringResource(R.string.hetzner_demo))
            } else {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    accounts.forEach { name ->
                        IchorFilterChip(selected = name == account, onClick = { vm.pick(name) }, label = { Text(name) })
                    }
                }
                signedIn?.let { MutedText(stringResource(R.string.gcp_signin_signed_in, it.account)) }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    IchorOutlinedButton(onClick = { adding = true }) { Text(stringResource(R.string.gcp_add_account)) }
                    if (account.isNotBlank()) {
                        IchorTextButton(onClick = { vm.remove(account) }) {
                            Text(stringResource(if (signedIn != null) R.string.kube_signin_sign_out else R.string.gcp_remove))
                        }
                    }
                }
            }
            when (val state = options) {
                null -> Unit
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> {
                    ErrorBox(state.message, vm::reload)
                    GCPActivationLink(state.message)
                }
                is UiState.Loaded -> VMForm(
                    state.data, form, labelsJson, busy = current?.running == true, price = price,
                    onType = vm::loadPrice,
                    onPlace = if (demo) null else { project, zone -> vm.switchPlace(project, zone) },
                    canMakeImage = !demo && account.isNotBlank(),
                    madeImage = madeImage,
                    onMakeImage = { imageFor = it },
                    onPrepareNetwork = if (demo || account.isBlank()) null else vm::prepareNetwork,
                ) { request ->
                    // The key is read only now, and goes to the run alone.
                    scope.launch { app.joinManager.start(request, if (account.isBlank()) "" else vm.credential(account)) }
                }
            }
        }
    }
    if (adding) GCPAccountDialog(vm) { adding = false }
    imageFor?.let { ask ->
        GCPImageSheet(
            ask = ask,
            template = form?.template.orEmpty(),
            estimate = vm::estimate,
            onStart = { spec ->
                imageFor = null
                val request = JoinRequest(
                    address = "", template = spec.templateNode, role = "", hostname = "Talos ${spec.arch}", disk = "",
                    staticAddress = "", gcpImage = spec,
                )
                // The key is read only now, and goes to the run alone.
                scope.launch { app.joinManager.start(request, vm.credential(account)) }
            },
            onDismiss = { imageFor = null },
        )
    }
}

/** "e2-medium · 2 CPU · 4 GB". */
private fun typeLabel(t: GCPMachineType): String {
    val memory = if (t.memoryGB % 1.0 == 0.0) t.memoryGB.toLong().toString() else t.memoryGB.toString()
    return "${t.name} · ${t.vcpus} CPU · $memory GB"
}

/**
 * The pickers (project → zone → machine type of an arch → image it boots; network → subnet of the
 * zone's region), the public address, the disk, the price line and the confirmed Create.
 * [onPlace] moves the key to another project or zone (null on the demo).
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun VMForm(
    options: GCPOptions,
    form: JoinConfigForm?,
    /** On Omni, the labels to sign into the join token; "" on plain Talos. */
    labelsJson: String,
    busy: Boolean,
    price: UiState<GCPPrice>?,
    onType: (zone: String, machineType: String) -> Unit,
    onPlace: ((project: String?, zone: String?) -> Unit)?,
    canMakeImage: Boolean,
    /** The selfLink of the image the last run made: picked once it is listed. */
    madeImage: String,
    onMakeImage: (GCPImageAsk) -> Unit,
    /** The guided network; null where it cannot run (the demo, no key). */
    onPrepareNetwork: (suspend (GCPNetworkSpec) -> GCPNetworkResult)?,
    onCreate: (JoinRequest) -> Unit,
) {
    val colors = LocalStatusColors.current
    val authenticated = rememberAuthenticated()
    val place = options.project + "/" + options.zone
    var arch by rememberSaveable(place) { mutableStateOf(options.arches().firstOrNull().orEmpty()) }
    var typeName by rememberSaveable(place) { mutableStateOf("") }
    var imageLink by rememberSaveable(place) { mutableStateOf("") }
    var networkName by rememberSaveable(place) { mutableStateOf(options.usableNetworks().singleOrNull()?.name.orEmpty()) }
    var subnetLink by rememberSaveable(place) { mutableStateOf("") }
    var publicIp by rememberSaveable(place) { mutableStateOf(true) }
    var diskText by rememberSaveable { mutableStateOf(GCP_DEFAULT_DISK_GB.toString()) }
    var omniName by rememberSaveable { mutableStateOf("") }
    var confirm by remember { mutableStateOf(false) }

    val type = options.typesFor(arch).firstOrNull { it.name == typeName }
    val images = type?.let { options.imagesFor(it) }.orEmpty()
    val image = images.firstOrNull { it.selfLink == imageLink }
    val networks = options.usableNetworks()
    val network = networks.firstOrNull { it.name == networkName }
    val subnet = network?.subnets?.firstOrNull { it.selfLink == subnetLink }
    val region = options.zones.firstOrNull { it.name == options.zone }?.region ?: options.zone.substringBeforeLast('-')
    val disk = diskText.toIntOrNull()?.takeIf { it in GCP_MIN_DISK_GB..GCP_MAX_DISK_GB }
    val hostname = form?.hostname?.trim() ?: omniName.trim()
    LaunchedEffect(options.zone, typeName) { onType(options.zone, typeName) }
    LaunchedEffect(madeImage, options) {
        val made = options.images.firstOrNull { it.selfLink == madeImage || it.name == madeImage } ?: return@LaunchedEffect
        // Made for the picked type's arch; another arch picked since: its types are listed again.
        if (made.arch != arch) {
            arch = made.arch
            typeName = ""
        }
        imageLink = made.selfLink
    }

    if (form == null) {
        OutlinedTextField(
            value = omniName,
            // The VM is named after it: Compute Engine takes lowercase only.
            onValueChange = { omniName = it.lowercase() },
            label = { Text(stringResource(R.string.join_config_hostname)) },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false),
            modifier = Modifier.fillMaxWidth(),
        )
    }
    if (hostname.isNotEmpty() && !gcpValidName(hostname)) {
        Text(stringResource(R.string.gcp_hostname_invalid), color = colors.warn, style = MaterialTheme.typography.bodySmall)
    }
    GCPDropdown(
        stringResource(R.string.gcp_project),
        options.projects.map { it.id to if (it.name.isBlank() || it.name == it.id) it.id else "${it.name} (${it.id})" },
        options.project,
        enabled = onPlace != null,
    ) { onPlace?.invoke(it, null) }
    GCPDropdown(
        stringResource(R.string.gcp_zone),
        options.zones.map { it.name to it.name },
        options.zone,
        enabled = onPlace != null,
    ) { onPlace?.invoke(null, it) }
    options.otherWarnings().forEach { InfoNotice(it) }

    Text(stringResource(R.string.gcp_machine_type), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.arches().forEach { a ->
            IchorFilterChip(
                selected = a == arch,
                onClick = {
                    // Another arch: the picked type and image do not run it.
                    if (a != arch) {
                        typeName = ""
                        imageLink = ""
                    }
                    arch = a
                },
                label = { Text(a) },
            )
        }
    }
    GCPDropdown(stringResource(R.string.gcp_machine_type), options.typesFor(arch).map { it.name to typeLabel(it) }, typeName) {
        typeName = it
    }
    if (type != null) {
        if (images.isEmpty() && !canMakeImage) InfoNotice(stringResource(R.string.gcp_no_image, type.arch))
        if (canMakeImage) {
            if (images.isEmpty()) InfoNotice(stringResource(R.string.gcp_image_missing, type.arch))
            IchorOutlinedButton(
                onClick = { if (subnet != null) onMakeImage(GCPImageAsk(options.project, options.zone, type.arch, subnet, publicIp)) },
                enabled = !busy && subnet != null,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(if (images.isEmpty()) R.string.gcp_make_image else R.string.azure_image_make_another)) }
            if (subnet == null) MutedText(stringResource(R.string.gcp_image_need_subnet))
        }
        if (images.isNotEmpty()) {
            GCPDropdown(
                stringResource(R.string.gcp_image),
                images.map { i -> i.selfLink to listOf(i.name, i.talosVersion).filter { it.isNotBlank() }.joinToString(" · ") },
                imageLink,
            ) { imageLink = it }
        }
    }

    if (onPrepareNetwork != null) {
        GCPGuidedNetwork(options.project, options.zone, omni = form == null, role = form?.role.orEmpty(), enabled = !busy, prepare = onPrepareNetwork) { ready ->
            // The default network and its subnet of the region fill the pickers.
            networkName = ready.network
            subnetLink = ready.subnetworkLink
        }
    }
    if (networks.isEmpty()) {
        InfoNotice(stringResource(R.string.gcp_no_subnet, region))
    } else {
        GCPDropdown(stringResource(R.string.gcp_network), networks.map { it.name to it.name }, networkName) {
            if (it != networkName) subnetLink = ""
            networkName = it
        }
    }
    if (network?.wideOpen == true) {
        Text(stringResource(R.string.gcp_network_wide_open), color = colors.warn, style = MaterialTheme.typography.bodySmall)
    }
    if (network != null) {
        LaunchedEffect(network.name) { network.subnets.singleOrNull()?.let { subnetLink = it.selfLink } }
        GCPDropdown(
            stringResource(R.string.aws_subnet),
            network.subnets.map { s -> s.selfLink to listOf(s.name, s.region, s.cidr).filter { it.isNotBlank() }.joinToString(" · ") },
            subnetLink,
        ) { subnetLink = it }
    }
    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.gcp_public_ip), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f).padding(end = 8.dp))
        IchorSwitch(checked = publicIp, onCheckedChange = { publicIp = it })
    }
    if (!publicIp) options.natWarnings().forEach { InfoNotice(it) }
    OutlinedTextField(
        value = diskText,
        onValueChange = { diskText = it.filter(Char::isDigit).take(5) },
        label = { Text(stringResource(R.string.gcp_disk)) },
        supportingText = { Text(stringResource(R.string.gcp_disk_hint)) },
        isError = disk == null,
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
        modifier = Modifier.fillMaxWidth(),
    )

    if (type != null) {
        when (val state = price) {
            null, UiState.Loading -> LoadingBox()
            is UiState.Failed -> Unit
            is UiState.Loaded -> Text(
                if (state.data.hourly.isBlank()) stringResource(R.string.gcp_cost_unknown) else stringResource(R.string.aws_cost, state.data.hourly),
                style = MaterialTheme.typography.titleSmall,
                color = colors.warn,
            )
        }
    }
    IchorButton(
        onClick = { confirm = true },
        enabled = !busy && gcpValidName(hostname) && form?.complete != false && type != null && image != null && subnet != null && disk != null,
        modifier = Modifier.fillMaxWidth(),
    ) { Text(stringResource(R.string.hetzner_create)) }

    if (confirm && type != null && image != null && subnet != null && disk != null) {
        HostnameConfirmDialog(
            title = stringResource(R.string.gcp_confirm_title, hostname),
            hostname = hostname,
            confirmLabel = stringResource(R.string.hetzner_create),
            onConfirm = {
                confirm = false
                val spec = GCPCreateSpec(
                    project = options.project, zone = options.zone, machineType = type.name, image = image.selfLink.ifBlank { image.name },
                    subnetwork = subnet.selfLink.ifBlank { subnet.name }, associatePublicIp = publicIp, diskGB = disk,
                    hostname = hostname, role = form?.role.orEmpty(), templateNode = form?.template.orEmpty(), installDisk = form?.disk.orEmpty(), labelsJson = labelsJson,
                )
                authenticated(hostname) {
                    onCreate(
                        JoinRequest(
                            address = "", template = spec.templateNode, role = spec.role, hostname = hostname, disk = spec.installDisk,
                            staticAddress = "", gcp = spec, omni = form == null,
                        ),
                    )
                }
            },
            onDismiss = { confirm = false },
            emphasized = true,
        ) {
            Text(stringResource(R.string.gcp_confirm_body, type.name, options.zone), style = MaterialTheme.typography.bodyMedium)
        }
    }
}
