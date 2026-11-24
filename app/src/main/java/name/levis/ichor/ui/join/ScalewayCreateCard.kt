package name.levis.ichor.ui.join

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
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
import name.levis.ichor.model.ScalewayCheck
import name.levis.ichor.model.ScalewayCreateSpec
import name.levis.ichor.model.ScalewayCredential
import name.levis.ichor.model.ScalewayImageCost
import name.levis.ichor.model.ScalewayNetwork
import name.levis.ichor.model.ScalewayNetworkSpec
import name.levis.ichor.model.ScalewayOptions
import name.levis.ichor.model.defaultSecurityGroup
import name.levis.ichor.model.hasClusterImage
import name.levis.ichor.model.hasTalosImage
import name.levis.ichor.model.hourlyPrice
import name.levis.ichor.model.imageArches
import name.levis.ichor.model.imagesTalosFirst
import name.levis.ichor.model.inZone
import name.levis.ichor.model.scalewayArch
import name.levis.ichor.model.typesFor
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
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/**
 * The stored API keys and what an Instance can be created from, read per zone ([zone] "" is the
 * key's own). The secret key never reaches the screen.
 */
class ScalewayCreateViewModel(private val lifecycle: NodeLifecycleRepository, private val store: CloudProviderStore) : ViewModel() {
    private val _accounts = MutableStateFlow<List<String>>(emptyList())
    val accounts: StateFlow<List<String>> = _accounts.asStateFlow()
    private val _account = MutableStateFlow("")
    val account: StateFlow<String> = _account.asStateFlow()
    private val _zone = MutableStateFlow("")
    val zone: StateFlow<String> = _zone.asStateFlow()
    private val _options = MutableStateFlow<UiState<ScalewayOptions>?>(null)
    val options: StateFlow<UiState<ScalewayOptions>?> = _options.asStateFlow()
    private var demo = false

    /** Reads the stored keys; the demo lists its samples without one. */
    fun load(demo: Boolean) {
        this.demo = demo
        viewModelScope.launch {
            val stored = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.SCALEWAY) }
            _accounts.value = stored
            when {
                demo -> reload()
                _account.value !in stored -> pick(stored.firstOrNull().orEmpty())
            }
        }
    }

    fun pick(account: String) {
        _account.value = account
        _zone.value = ""
        if (account.isBlank() && !demo) _options.value = null else reload()
    }

    /** Reads the options of [zone]; the demo keeps its samples and only moves the picked zone. */
    fun pickZone(zone: String) {
        _zone.value = zone
        if (!demo) reload()
    }

    fun reload() {
        val account = _account.value
        viewModelScope.launch {
            _options.value = UiState.Loading
            _options.value = uiStateOf { lifecycle.scalewayOptions(if (account.isBlank()) "" else credential(account)) }
        }
    }

    /** The options again without the loading state, so the form keeps its picks; a failure keeps the old ones. */
    fun refresh() {
        val account = _account.value
        if (account.isBlank()) return
        viewModelScope.launch {
            val state = uiStateOf { lifecycle.scalewayOptions(credential(account)) }
            if (state is UiState.Loaded) _options.value = state
        }
    }

    /** The guided security group (writes at Scaleway), then the options again so the group is listed. */
    suspend fun prepareNetwork(spec: ScalewayNetworkSpec): ScalewayNetwork =
        lifecycle.scalewayNetworkPrepare(credential(_account.value), spec).also { refresh() }

    /** The cost line of an image run in [zone]. Creates nothing. */
    suspend fun estimate(arch: String, zone: String): ScalewayImageCost =
        lifecycle.scalewayImageEstimate(credential(_account.value), arch, zone)

    /** The stored key, in the picked zone when one is picked. */
    suspend fun credential(account: String): String {
        val stored = withContext(Dispatchers.IO) { store.token(CloudProviderStore.SCALEWAY, account) }
        val zone = _zone.value
        if (zone.isBlank() || stored.isBlank()) return stored
        val key = runCatching { TalosJson.decodeFromString(ScalewayCredential.serializer(), stored) }.getOrNull() ?: return stored
        return TalosJson.encodeToString(ScalewayCredential.serializer(), key.inZone(zone))
    }

    suspend fun test(credentialJson: String): ScalewayCheck = lifecycle.scalewayTest(credentialJson)

    suspend fun policy(project: String): String = lifecycle.scalewayPolicy(project)

    /** Seals the key and picks it; false when the store cannot be read. */
    suspend fun save(account: String, credentialJson: String): Boolean {
        if (!withContext(Dispatchers.IO) { store.save(CloudProviderStore.SCALEWAY, account, credentialJson) }) return false
        _accounts.value = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.SCALEWAY) }
        pick(account)
        return true
    }

    fun remove(account: String) {
        viewModelScope.launch {
            withContext(Dispatchers.IO) { store.save(CloudProviderStore.SCALEWAY, account, "") }
            val left = withContext(Dispatchers.IO) { store.projects(CloudProviderStore.SCALEWAY) }
            _accounts.value = left
            pick(left.firstOrNull().orEmpty())
        }
    }
}

/**
 * "Create the Instance at Scaleway": the run makes the user data itself. [form] is the new member's
 * (null on Omni, which only needs a name). The cost, the typed name and the app lock come before
 * the billable create; the run's timeline then replaces the card's form.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ScalewayCreateCard(
    form: JoinConfigForm?,
    /** On Omni, the labels to sign into the join token; "" on plain Talos. */
    labelsJson: String,
    demo: Boolean,
    onAllocate: (() -> Unit)? = null,
    vm: ScalewayCreateViewModel = viewModel(
        key = "scaleway-create",
        factory = factory { ScalewayCreateViewModel(app.nodeLifecycleRepository, app.cloudProviderStore) },
    ),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val run by app.joinManager.current.collectAsStateWithLifecycle()
    val accounts by vm.accounts.collectAsStateWithLifecycle()
    val account by vm.account.collectAsStateWithLifecycle()
    val zone by vm.zone.collectAsStateWithLifecycle()
    val options by vm.options.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var adding by remember { mutableStateOf(false) }
    var imageFor by remember { mutableStateOf<ScalewayImageAsk?>(null) }
    var madeImage by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(demo) { vm.load(demo) }

    SectionCard(stringResource(R.string.scaleway_title)) {
        val current = run
        if (current?.request?.scaleway != null) {
            JoinRunView(current, onStop = app.joinManager::stop, onOpenNode = null, onClose = app.joinManager::dismiss, onAllocate = onAllocate)
        } else {
            if (current?.request?.scalewayImage != null) {
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
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    IchorOutlinedButton(onClick = { adding = true }) { Text(stringResource(R.string.scaleway_add_key)) }
                    if (account.isNotBlank()) {
                        IchorTextButton(onClick = { vm.remove(account) }) { Text(stringResource(R.string.scaleway_remove)) }
                    }
                }
            }
            when (val state = options) {
                null -> Unit
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(state.message, vm::reload)
                is UiState.Loaded -> {
                    val shownZone = zone.ifBlank { state.data.zone }
                    ScalewayInstanceForm(
                        state.data, shownZone, vm::pickZone, form, labelsJson, busy = current?.running == true,
                        onPrepareNetwork = if (demo || account.isBlank()) null else vm::prepareNetwork,
                        // The demo opens the sheet too: the core refuses its run.
                        onMakeImage = if (!demo && account.isBlank()) null else { ask -> imageFor = ask },
                        madeImage = madeImage,
                    ) { request ->
                        // The key is read only now, and goes to the run alone.
                        scope.launch { app.joinManager.start(request, if (account.isBlank()) "" else vm.credential(account)) }
                    }
                }
            }
        }
    }
    if (adding) ScalewayKeyDialog(vm) { adding = false }
    imageFor?.let { ask ->
        ScalewayImageSheet(
            ask = ask,
            template = form?.template.orEmpty(),
            demo = demo,
            estimate = vm::estimate,
            onStart = { spec ->
                imageFor = null
                val request = JoinRequest(
                    address = "", template = spec.templateNode, role = "", hostname = "Talos ${spec.arch}", disk = "",
                    staticAddress = "", scalewayImage = spec,
                )
                // The key is read only now, and goes to the run alone.
                scope.launch { app.joinManager.start(request, if (account.isBlank()) "" else vm.credential(account)) }
            },
            onDismiss = { imageFor = null },
        )
    }
}

/** The zone, image, type and security group pickers, the cost line and the confirmed Create. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ScalewayInstanceForm(
    options: ScalewayOptions,
    zone: String,
    onZone: (String) -> Unit,
    form: JoinConfigForm?,
    labelsJson: String,
    busy: Boolean,
    /** The guided security group; null where it cannot run (the demo, no key). */
    onPrepareNetwork: (suspend (ScalewayNetworkSpec) -> ScalewayNetwork)?,
    /** Opens the image sheet; null where it cannot run (no key outside the demo). */
    onMakeImage: ((ScalewayImageAsk) -> Unit)?,
    /** The id of the image the last run made: picked once it is listed. */
    madeImage: String,
    onCreate: (JoinRequest) -> Unit,
) {
    val colors = LocalStatusColors.current
    val authenticated = rememberAuthenticated()
    var imageId by rememberSaveable { mutableStateOf("") }
    var typeName by rememberSaveable { mutableStateOf("") }
    var groupId by rememberSaveable { mutableStateOf("") }
    var omniName by rememberSaveable { mutableStateOf("") }
    var confirm by remember { mutableStateOf(false) }

    val images = options.imagesTalosFirst
    val image = images.firstOrNull { it.id == imageId }
    val types = options.typesFor(image)
    val type = types.firstOrNull { it.name == typeName }
    val group = options.securityGroups.firstOrNull { it.id == groupId } ?: options.defaultSecurityGroup
    val hostname = form?.hostname?.trim() ?: omniName.trim()
    LaunchedEffect(madeImage, options) {
        val made = options.images.firstOrNull { it.id == madeImage } ?: return@LaunchedEffect
        // Made for another architecture than the picked type's: its types are listed again.
        if (options.types.firstOrNull { it.name == typeName }?.arch?.let(::scalewayArch) != scalewayArch(made.arch)) typeName = ""
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
    Text(stringResource(R.string.scaleway_zone), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.zones.forEach { z -> IchorFilterChip(selected = z == zone, onClick = { onZone(z) }, label = { Text(z) }) }
    }
    options.warnings.forEach { InfoNotice(it) }
    Text(stringResource(R.string.ovh_image), style = MaterialTheme.typography.labelLarge)
    // The image is made for the picked type's architecture, else the picked image's, else the zone's first.
    val makeArch = scalewayArch(options.types.firstOrNull { it.name == typeName }?.arch ?: image?.arch.orEmpty())
    val noClusterImage = !options.hasClusterImage(makeArch)
    val ask = ScalewayImageAsk(zone, options.imageArches,makeArch.ifBlank { options.imageArches.firstOrNull().orEmpty() })
    if (onMakeImage != null && noClusterImage) {
        // No Talos image at all: the core's warning above already says so.
        if (options.hasTalosImage("")) InfoNotice(stringResource(R.string.scaleway_image_missing, ask.arch))
        MakeImageButton(R.string.gcp_make_image, enabled = !busy && ask.arch.isNotBlank()) { onMakeImage(ask) }
    }
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        images.forEach { i ->
            val name = "${i.name} · ${i.arch}"
            IchorFilterChip(
                selected = i.id == imageId,
                onClick = { imageId = i.id },
                label = { Text(if (i.matches(options)) stringResource(R.string.aws_image_matches, name) else name) },
            )
        }
    }
    if (onMakeImage != null && !noClusterImage) {
        MakeImageButton(R.string.azure_image_make_another, enabled = !busy && ask.arch.isNotBlank()) { onMakeImage(ask) }
    }
    Text(stringResource(R.string.scaleway_type), style = MaterialTheme.typography.labelLarge)
    if (image != null) MutedText(stringResource(R.string.scaleway_arch_hint, image.arch))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        types.forEach { t ->
            val label = "${t.name} · ${t.vcpus} CPU · ${t.ramGB} GB"
            IchorFilterChip(
                selected = t.name == typeName,
                onClick = { typeName = t.name },
                label = { Text(if (t.blockOnly) stringResource(R.string.scaleway_block_only, label) else label) },
            )
        }
    }
    if (onPrepareNetwork != null) {
        ScalewayGuidedNetwork(zone, omni = form == null, role = form?.role.orEmpty(), enabled = !busy, prepare = onPrepareNetwork) { network ->
            groupId = network.securityGroupId
        }
    }
    if (options.securityGroups.isNotEmpty()) {
        Text(stringResource(R.string.scaleway_security_group), style = MaterialTheme.typography.labelLarge)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            options.securityGroups.forEach { g ->
                IchorFilterChip(selected = g.id == group?.id, onClick = { groupId = g.id }, label = { Text(g.name) })
            }
        }
    }
    // The managed group already lets this phone in; any other must be checked by hand (plain Talos).
    if (form != null && group?.managed != true && options.groupWarning.isNotBlank()) InfoNotice(options.groupWarning)
    type?.let {
        Text(
            it.hourlyPrice?.let { price -> stringResource(R.string.ovh_cost, price) } ?: stringResource(R.string.scaleway_cost_unknown),
            style = MaterialTheme.typography.titleSmall,
            color = colors.warn,
        )
    }
    IchorButton(
        onClick = { confirm = true },
        enabled = !busy && hostname.isNotEmpty() && form?.complete != false && type != null && image != null && zone.isNotBlank(),
        modifier = Modifier.fillMaxWidth(),
    ) { Text(stringResource(R.string.hetzner_create)) }

    if (confirm && type != null && image != null) {
        HostnameConfirmDialog(
            title = stringResource(R.string.scaleway_confirm_title, hostname),
            hostname = hostname,
            confirmLabel = stringResource(R.string.hetzner_create),
            onConfirm = {
                confirm = false
                val spec = ScalewayCreateSpec(
                    zone = zone, type = type.name, imageId = image.id, securityGroupId = group?.id.orEmpty(), hostname = hostname,
                    role = form?.role.orEmpty(), templateNode = form?.template.orEmpty(), installDisk = form?.disk.orEmpty(), labelsJson = labelsJson,
                )
                authenticated(hostname) {
                    onCreate(
                        JoinRequest(
                            address = "", template = spec.templateNode, role = spec.role, hostname = hostname, disk = spec.installDisk,
                            staticAddress = "", scaleway = spec, omni = form == null,
                        ),
                    )
                }
            },
            onDismiss = { confirm = false },
            emphasized = true,
        ) {
            Text(stringResource(R.string.scaleway_confirm_body, type.name, zone), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

/** "Make a Talos image" (no Talos image listed) or "Make another Talos image". */
@Composable
private fun MakeImageButton(@StringRes label: Int, enabled: Boolean, onClick: () -> Unit) {
    IchorOutlinedButton(onClick = onClick, enabled = enabled, modifier = Modifier.fillMaxWidth()) { Text(stringResource(label)) }
}
