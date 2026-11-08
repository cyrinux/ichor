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
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.CloudProviderStore
import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.NodeLifecycleRepository
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.AWSCheck
import name.levis.ichor.model.AWSCreateSpec
import name.levis.ichor.model.AWSCredential
import name.levis.ichor.model.AWSImageCost
import name.levis.ichor.model.AWSInstanceType
import name.levis.ichor.model.AWSNetwork
import name.levis.ichor.model.AWSNetworkSpec
import name.levis.ichor.model.AWSOptions
import name.levis.ichor.model.AWSPrice
import name.levis.ichor.model.AWSSignInChoice
import name.levis.ichor.model.AWSSignInFinish
import name.levis.ichor.model.AWSSignInStart
import name.levis.ichor.model.SsoRoleChoice
import name.levis.ichor.model.AWSSubnet
import name.levis.ichor.model.arches
import name.levis.ichor.model.hasClusterImage
import name.levis.ichor.model.imagesFor
import name.levis.ichor.model.securityGroupsIn
import name.levis.ichor.model.typesFor
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
import name.levis.ichor.ui.design.IchorSwitch
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/**
 * The stored access keys (one per account id, with their region) and what an instance can be
 * created from. The secret never reaches the screen.
 */
class AWSCreateViewModel(private val lifecycle: NodeLifecycleRepository, private val store: CloudProviderStore) : ViewModel() {
    private val _accounts = MutableStateFlow<List<String>>(emptyList())
    val accounts: StateFlow<List<String>> = _accounts.asStateFlow()
    private val _account = MutableStateFlow("")
    val account: StateFlow<String> = _account.asStateFlow()
    private val _options = MutableStateFlow<UiState<AWSOptions>?>(null)
    val options: StateFlow<UiState<AWSOptions>?> = _options.asStateFlow()
    private val _price = MutableStateFlow<UiState<AWSPrice>?>(null)
    val price: StateFlow<UiState<AWSPrice>?> = _price.asStateFlow()
    private var priceJob: Job? = null

    /** The picked account's IAM Identity Center sign-in (no secret in it), null for access keys. */
    private val _signedIn = MutableStateFlow<AWSCredential?>(null)
    val signedIn: StateFlow<AWSCredential?> = _signedIn.asStateFlow()

    /** Reads the stored keys; the demo lists its samples without any. */
    fun load(demo: Boolean) {
        viewModelScope.launch {
            val stored = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.AWS) }
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
        reload()
        viewModelScope.launch {
            val stored = runCatching { TalosJson.decodeFromString(AWSCredential.serializer(), credential(account)) }.getOrNull()
            _signedIn.value = stored?.takeIf { it.identityCenter }
        }
    }

    fun reload() {
        val account = _account.value
        viewModelScope.launch {
            _options.value = UiState.Loading
            _options.value = uiStateOf { lifecycle.awsOptions(if (account.isBlank()) "" else credential(account)).also { reseal(account, it.credential) } }
        }
    }

    /** Seals again what the core renewed (an Identity Center sign-in's tokens); "" is nothing to do. */
    private suspend fun reseal(account: String, updated: String) {
        if (account.isNotBlank() && updated.isNotBlank()) withContext(Dispatchers.IO) { store.save(CloudProviderStore.AWS, account, updated) }
    }

    /**
     * The credential for a run, which uses only what it is given: an Identity Center sign-in is
     * renewed first. A renewal that fails passes the stored one, and the run says why.
     */
    suspend fun runCredential(account: String): String {
        if (account.isBlank()) return ""
        val stored = credential(account)
        val renewed = runCatching { lifecycle.awsRenew(stored).credential }.getOrDefault("")
        reseal(account, renewed)
        return renewed.ifBlank { stored }
    }

    suspend fun signInStart(request: String): AWSSignInStart = lifecycle.awsSignInStart(request)

    suspend fun signInFinish(credential: String): AWSSignInFinish = lifecycle.awsSignInFinish(credential)

    suspend fun signInChoose(credential: String, role: SsoRoleChoice): AWSSignInChoice = lifecycle.awsSignInChoose(credential, role)

    fun cancelSignIn() = lifecycle.awsSignInCancel()

    /** Lists the options again, keeping the shown form (and what is picked in it) meanwhile. */
    fun refresh() {
        val account = _account.value
        if (account.isBlank()) return
        viewModelScope.launch {
            val state = uiStateOf { lifecycle.awsOptions(credential(account)).also { reseal(account, it.credential) } }
            if (state is UiState.Loaded) _options.value = state
        }
    }

    /** The guided network (writes at AWS), then the options again so its group is listed. */
    suspend fun prepareNetwork(spec: AWSNetworkSpec): AWSNetwork {
        val account = _account.value
        return lifecycle.awsNetworkPrepare(credential(account), spec).also {
            reseal(account, it.credential)
            refresh()
        }
    }

    suspend fun estimate(arch: String, region: String): AWSImageCost {
        val account = _account.value
        return lifecycle.awsImageEstimate(credential(account), arch, region).also { reseal(account, it.credential) }
    }

    /** Asks the Price List for [instanceType]; a failure (the demo has no keys) is "price unavailable". */
    fun loadPrice(instanceType: String) {
        val account = _account.value
        priceJob?.cancel()
        if (instanceType.isBlank()) {
            _price.value = null
            return
        }
        priceJob = viewModelScope.launch {
            _price.value = UiState.Loading
            val state = uiStateOf {
                lifecycle.awsPrice(if (account.isBlank()) "" else credential(account), instanceType).also { reseal(account, it.credential) }
            }
            _price.value = if (state is UiState.Failed) UiState.Loaded(AWSPrice()) else state
        }
    }

    /**
     * The same keys in another region: re-sealed with it, then the region's options. Edited as
     * JSON: an Identity Center sign-in holds tokens [AWSCredential] does not keep.
     */
    fun switchRegion(region: String) {
        val account = _account.value
        if (account.isBlank()) return
        viewModelScope.launch {
            val current = runCatching { TalosJson.parseToJsonElement(credential(account)).jsonObject }.getOrNull() ?: return@launch
            val updated = JsonObject(current + ("region" to JsonPrimitive(region))).toString()
            if (withContext(Dispatchers.IO) { store.save(CloudProviderStore.AWS, account, updated) }) pick(account)
        }
    }

    suspend fun credential(account: String): String = withContext(Dispatchers.IO) { store.token(CloudProviderStore.AWS, account) }

    suspend fun test(credentialJson: String): AWSCheck = lifecycle.awsTest(credentialJson)

    suspend fun policy(): String = lifecycle.awsPolicy()

    /** Seals the keys under their account id and picks them; false when the store cannot be read. */
    suspend fun save(account: String, credentialJson: String): Boolean {
        if (!withContext(Dispatchers.IO) { store.save(CloudProviderStore.AWS, account, credentialJson) }) return false
        _accounts.value = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.AWS) }
        pick(account)
        return true
    }

    fun remove(account: String) {
        viewModelScope.launch {
            withContext(Dispatchers.IO) { store.save(CloudProviderStore.AWS, account, "") }
            val left = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.AWS) }
            _accounts.value = left
            pick(left.firstOrNull().orEmpty())
        }
    }
}

/**
 * "Create the instance at AWS": the run makes the user data itself. [form] is the new member's
 * (null on Omni, which only needs a name). The price, the typed name and the app lock come before
 * the billable create; the run's timeline then replaces the card's form.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun AWSCreateCard(
    form: JoinConfigForm?,
    /** On Omni, the labels to sign into the join token; "" on plain Talos. */
    labelsJson: String,
    demo: Boolean,
    onAllocate: (() -> Unit)? = null,
    vm: AWSCreateViewModel = viewModel(key = "aws-create", factory = factory { AWSCreateViewModel(app.nodeLifecycleRepository, app.cloudProviderStore) }),
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
    var amiFor by remember { mutableStateOf<AMIAsk?>(null) }
    var madeImage by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(demo) { vm.load(demo) }

    SectionCard(stringResource(R.string.aws_title)) {
        val current = run
        if (current?.request?.aws != null) {
            JoinRunView(current, onStop = app.joinManager::stop, onOpenNode = null, onClose = app.joinManager::dismiss, onAllocate = onAllocate)
        } else {
            if (current?.request?.awsImage != null) {
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
                signedIn?.let { MutedText(stringResource(R.string.aws_sso_signed_in, it.role.label)) }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    IchorOutlinedButton(onClick = { adding = true }) { Text(stringResource(R.string.aws_add_account)) }
                    if (account.isNotBlank()) {
                        IchorTextButton(onClick = { vm.remove(account) }) {
                            Text(stringResource(if (signedIn != null) R.string.kube_signin_sign_out else R.string.aws_remove))
                        }
                    }
                }
            }
            when (val state = options) {
                null -> Unit
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(state.message, vm::reload)
                is UiState.Loaded -> InstanceForm(
                    state.data, form, labelsJson, busy = current?.running == true, price = price,
                    onType = vm::loadPrice, onRegion = if (demo) null else vm::switchRegion,
                    canMakeImage = !demo && account.isNotBlank(), madeImage = madeImage,
                    onMakeImage = { arch, subnet, group -> amiFor = AMIAsk(state.data, arch, subnet, group) },
                    onPrepareNetwork = if (demo || account.isBlank()) null else vm::prepareNetwork,
                ) { request ->
                    // The keys are read only now, and go to the run alone.
                    scope.launch { app.joinManager.start(request, vm.runCredential(account)) }
                }
            }
        }
    }
    if (adding) KeysDialog(vm) { adding = false }
    amiFor?.let { ask ->
        AWSImageSheet(
            options = ask.options,
            initialArch = ask.arch,
            initialSubnet = ask.subnet,
            initialGroup = ask.group,
            template = form?.template.orEmpty(),
            estimate = vm::estimate,
            onStart = { spec ->
                amiFor = null
                val request = JoinRequest(
                    address = "", template = spec.templateNode, role = "", hostname = "Talos ${spec.arch}", disk = "",
                    staticAddress = "", awsImage = spec,
                )
                // The keys are read only now, and go to the run alone.
                scope.launch { app.joinManager.start(request, vm.runCredential(account)) }
            },
            onDismiss = { amiFor = null },
        )
    }
}

/** What the AMI sheet opens with: the card's current picks. */
private data class AMIAsk(val options: AWSOptions, val arch: String, val subnet: String, val group: String)

/** "t3.small · 2 CPU · 8 GB", and "burstable" when it is. */
@Composable
private fun typeLabel(t: AWSInstanceType): String {
    val memory = if (t.memoryGB % 1.0 == 0.0) t.memoryGB.toLong().toString() else t.memoryGB.toString()
    val base = "${t.name} · ${t.vcpus} CPU · $memory GB"
    return if (t.burstable) "$base · ${stringResource(R.string.aws_burstable)}" else base
}

@Composable
internal fun subnetLabel(s: AWSSubnet): String {
    val base = listOf(s.name.ifBlank { s.id }, s.az, s.cidr).filter { it.isNotBlank() }.joinToString(" · ")
    return if (s.public) "$base · ${stringResource(R.string.aws_subnet_public)}" else base
}

/** The pickers (type → AMI of its arch, subnet → groups of its VPC), the price line and the confirmed Create. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun InstanceForm(
    options: AWSOptions,
    form: JoinConfigForm?,
    /** On Omni, the labels to sign into the join token; "" on plain Talos. */
    labelsJson: String,
    busy: Boolean,
    price: UiState<AWSPrice>?,
    onType: (String) -> Unit,
    onRegion: ((String) -> Unit)?,
    canMakeImage: Boolean,
    /** The AMI the last run made: picked once it is listed. */
    madeImage: String,
    onMakeImage: (arch: String, subnet: String, group: String) -> Unit,
    /** The guided network; null where it cannot run (the demo, no account). */
    onPrepareNetwork: (suspend (AWSNetworkSpec) -> AWSNetwork)?,
    onCreate: (JoinRequest) -> Unit,
) {
    val colors = LocalStatusColors.current
    val authenticated = rememberAuthenticated()
    var typeName by rememberSaveable(options.region) { mutableStateOf("") }
    var imageId by rememberSaveable(options.region) { mutableStateOf("") }
    var subnetId by rememberSaveable(options.region) { mutableStateOf("") }
    var groupIds by rememberSaveable(options.region) { mutableStateOf(listOf<String>()) }
    var publicIp by rememberSaveable(options.region) { mutableStateOf(false) }
    var omniName by rememberSaveable { mutableStateOf("") }
    var confirm by remember { mutableStateOf(false) }
    LaunchedEffect(madeImage) { if (madeImage.isNotBlank()) imageId = madeImage }

    val type = options.types.firstOrNull { it.name == typeName }
    val images = type?.let { options.imagesFor(it.arch) }.orEmpty()
    val image = images.firstOrNull { it.id == imageId }
    val subnet = options.subnets.firstOrNull { it.id == subnetId }
    val groups = subnet?.let { options.securityGroupsIn(it.vpcId) }.orEmpty()
    val hostname = form?.hostname?.trim() ?: omniName.trim()
    LaunchedEffect(typeName) { onType(typeName) }

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
    Text(stringResource(R.string.aws_region), style = MaterialTheme.typography.labelLarge)
    if (onRegion != null && options.regions.size > 1) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            options.regions.forEach { r -> IchorFilterChip(selected = r == options.region, onClick = { if (r != options.region) onRegion(r) }, label = { Text(r) }) }
        }
    } else {
        Text(options.region, style = MaterialTheme.typography.bodyMedium)
    }
    options.warnings.forEach { InfoNotice(it) }

    Text(stringResource(R.string.aws_instance_type), style = MaterialTheme.typography.labelLarge)
    options.arches().forEach { arch ->
        MutedText(arch)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            options.typesFor(arch).forEach { t ->
                IchorFilterChip(
                    selected = t.name == typeName,
                    onClick = {
                        // Another arch: the picked AMI cannot boot it.
                        if (type?.arch != t.arch) imageId = ""
                        typeName = t.name
                    },
                    label = { Text(typeLabel(t)) },
                )
            }
        }
    }
    if (type != null) {
        Text(stringResource(R.string.aws_image), style = MaterialTheme.typography.labelLarge)
        if (images.isEmpty() && !canMakeImage) InfoNotice(stringResource(R.string.aws_no_image, type.arch))
        if (canMakeImage) {
            // An AMI of another version or schematic boots Talos without the cluster's extensions: offer one of its image.
            val matched = options.hasClusterImage(type.arch)
            if (!matched) InfoNotice(stringResource(R.string.aws_image_missing, type.arch, options.region))
            IchorOutlinedButton(
                onClick = { onMakeImage(type.arch, subnetId, groupIds.firstOrNull().orEmpty()) },
                enabled = !busy,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(if (matched) R.string.aws_image_make_another else R.string.aws_make_ami)) }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            images.forEach { i ->
                val name = i.name.ifBlank { i.id }
                IchorFilterChip(
                    selected = i.id == imageId,
                    onClick = { imageId = i.id },
                    label = { Text(if (i.matches(options)) stringResource(R.string.aws_image_matches, name) else name) },
                )
            }
        }
    }

    if (onPrepareNetwork != null) {
        GuidedNetwork(options.region, omni = form == null, role = form?.role.orEmpty(), enabled = !busy, prepare = onPrepareNetwork) { network ->
            // Its subnet and group fill the pickers; the address is asked for even where the subnet gives none.
            subnetId = network.subnetId
            groupIds = listOf(network.securityGroupId)
            publicIp = true
        }
    }
    Text(stringResource(R.string.aws_subnet), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.subnets.forEach { s ->
            IchorFilterChip(
                selected = s.id == subnetId,
                onClick = {
                    // Groups of another VPC are refused with this subnet.
                    if (subnet?.vpcId != s.vpcId) groupIds = emptyList()
                    subnetId = s.id
                    publicIp = s.public
                },
                label = { Text(subnetLabel(s)) },
            )
        }
    }
    if (subnet != null) {
        Text(stringResource(R.string.aws_security_groups), style = MaterialTheme.typography.labelLarge)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            groups.forEach { g ->
                IchorFilterChip(
                    selected = g.id in groupIds,
                    onClick = { groupIds = if (g.id in groupIds) groupIds - g.id else groupIds + g.id },
                    label = { Text(g.name.ifBlank { g.id }) },
                )
            }
        }
        if (groupIds.isEmpty()) MutedText(stringResource(R.string.aws_security_groups_none))
        groups.filter { it.id in groupIds && it.wideOpen }.forEach { g ->
            Text("${g.name.ifBlank { g.id }}: ${stringResource(R.string.aws_network_wide_open)}", color = colors.warn, style = MaterialTheme.typography.bodySmall)
        }
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.aws_public_ip), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f).padding(end = 8.dp))
            IchorSwitch(checked = publicIp, onCheckedChange = { publicIp = it })
        }
    }

    if (type != null) {
        when (val state = price) {
            null, UiState.Loading -> LoadingBox()
            is UiState.Failed -> Unit
            is UiState.Loaded -> Text(
                if (state.data.hourly.isBlank()) stringResource(R.string.aws_cost_unknown) else stringResource(R.string.aws_cost, state.data.hourly),
                style = MaterialTheme.typography.titleSmall,
                color = colors.warn,
            )
        }
    }
    IchorButton(
        onClick = { confirm = true },
        enabled = !busy && hostname.isNotEmpty() && form?.complete != false && type != null && image != null && subnet != null,
        modifier = Modifier.fillMaxWidth(),
    ) { Text(stringResource(R.string.hetzner_create)) }

    if (confirm && type != null && image != null && subnet != null) {
        HostnameConfirmDialog(
            title = stringResource(R.string.aws_confirm_title, hostname),
            hostname = hostname,
            confirmLabel = stringResource(R.string.hetzner_create),
            onConfirm = {
                confirm = false
                val spec = AWSCreateSpec(
                    region = options.region, instanceType = type.name, imageId = image.id, subnetId = subnet.id,
                    securityGroupIds = groupIds.filter { id -> groups.any { it.id == id } }, associatePublicIp = publicIp,
                    hostname = hostname, role = form?.role.orEmpty(), templateNode = form?.template.orEmpty(), installDisk = form?.disk.orEmpty(), labelsJson = labelsJson,
                )
                authenticated(hostname) {
                    onCreate(
                        JoinRequest(
                            address = "", template = spec.templateNode, role = spec.role, hostname = hostname, disk = spec.installDisk,
                            staticAddress = "", aws = spec, omni = form == null,
                        ),
                    )
                }
            },
            onDismiss = { confirm = false },
            emphasized = true,
        ) {
            Text(stringResource(R.string.aws_confirm_body, type.name, options.region), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

/**
 * Access keys (masked, an optional session token, the region) or an IAM Identity Center sign-in,
 * the policy to give them, a test, then sealed under the account id.
 */
@Composable
private fun KeysDialog(vm: AWSCreateViewModel, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var identityCenter by remember { mutableStateOf(false) }
    var ssoReady by remember { mutableStateOf<Pair<String, AWSCheck>?>(null) }
    var accessKeyId by remember { mutableStateOf("") }
    var secret by remember { mutableStateOf("") }
    var sessionToken by remember { mutableStateOf("") }
    var region by remember { mutableStateOf("") }
    var check by remember { mutableStateOf<UiState<AWSCheck>?>(null) }
    var saveFailed by remember { mutableStateOf(false) }
    val tested = (check as? UiState.Loaded)?.data
    val policyTitle = stringResource(R.string.aws_copy_policy)

    fun credential() = TalosJson.encodeToString(
        AWSCredential.serializer(),
        AWSCredential(accessKeyId.trim(), secret.trim(), sessionToken.trim(), region.trim()),
    )

    fun runTest() {
        check = UiState.Loading
        scope.launch { check = uiStateOf { vm.test(credential()) } }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.aws_add_account)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    IchorFilterChip(selected = !identityCenter, onClick = { identityCenter = false }, label = { Text(stringResource(R.string.kube_signin_option_aws_keys)) })
                    IchorFilterChip(selected = identityCenter, onClick = { identityCenter = true }, label = { Text(stringResource(R.string.kube_signin_option_aws_sso)) })
                }
                MutedText(stringResource(R.string.aws_policy_hint))
                IchorOutlinedButton(onClick = { scope.launch { copyToClipboard(context, policyTitle, vm.policy()) } }) { Text(policyTitle) }
                if (identityCenter) {
                    IdentityCenterSide(vm) { credential, check -> ssoReady = if (credential != null && check != null) credential to check else null }
                    if (saveFailed) NotSaved()
                    return@Column
                }
                Text(stringResource(R.string.aws_keys_warning), color = colors.warn, style = MaterialTheme.typography.bodySmall)
                KeyField(accessKeyId, R.string.aws_access_key_id, secret = false) {
                    accessKeyId = it
                    check = null
                }
                KeyField(secret, R.string.aws_secret_access_key, secret = true) {
                    secret = it
                    check = null
                }
                KeyField(sessionToken, R.string.aws_session_token, secret = true) {
                    sessionToken = it
                    check = null
                }
                MutedText(stringResource(R.string.aws_session_token_hint))
                KeyField(region, R.string.aws_region, secret = false) {
                    region = it
                    check = null
                }
                IchorOutlinedButton(
                    onClick = ::runTest,
                    enabled = accessKeyId.isNotBlank() && secret.isNotBlank() && region.isNotBlank() && check != UiState.Loading,
                ) {
                    Text(stringResource(R.string.hetzner_test))
                }
                when (val state = check) {
                    null -> Unit
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(state.message, ::runTest)
                    is UiState.Loaded -> CheckLine(state.data)
                }
                if (saveFailed) NotSaved()
            }
        },
        confirmButton = {
            // What the test passed: the keys (renewed by the core when it says so), or the sign-in.
            val ready = if (identityCenter) ssoReady else tested?.let { (it.credential.ifBlank { credential() }) to it }
            val readyCheck = ready?.second
            IchorButton(
                onClick = {
                    val (json, passed) = ready ?: return@IchorButton
                    scope.launch {
                        if (vm.save(passed.account, json)) onDismiss() else saveFailed = true
                    }
                },
                enabled = readyCheck != null && readyCheck.account.isNotBlank() && (readyCheck.canCreate || !readyCheck.checked),
            ) { Text(stringResource(R.string.hetzner_save)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** The store could not seal the account. */
@Composable
private fun NotSaved() {
    Text(stringResource(R.string.hetzner_token_not_saved), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
}

/** What a test of the keys or the sign-in says: whose they are, and whether they may run instances. */
@Composable
internal fun CheckLine(data: AWSCheck) {
    val colors = LocalStatusColors.current
    Text(
        when {
            !data.checked -> stringResource(R.string.aws_check_unknown, data.account, data.region)
            data.canCreate -> stringResource(R.string.aws_check_ok, data.account, data.region)
            else -> stringResource(R.string.aws_check_no, data.account, data.region)
        },
        color = if (data.checked && !data.canCreate) colors.warn else colors.ok,
        style = MaterialTheme.typography.bodySmall,
    )
}

/** One field of the keys dialog; a secret one is masked and never suggested. */
@Composable
internal fun KeyField(value: String, @StringRes label: Int, secret: Boolean, onChange: (String) -> Unit) {
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
