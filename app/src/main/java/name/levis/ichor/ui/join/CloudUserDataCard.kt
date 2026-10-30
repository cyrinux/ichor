package name.levis.ichor.ui.join

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Share
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
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
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.NodeLifecycleRepository
import name.levis.ichor.model.CloudUserData
import name.levis.ichor.model.joinTemplates
import name.levis.ichor.model.providerLabel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.components.shareText
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.util.formatBytes

/** The cloud user data of the active cluster's new node, made when asked (it carries a credential). */
class CloudUserDataViewModel(private val lifecycle: NodeLifecycleRepository) : ViewModel() {
    private val _data = MutableStateFlow<UiState<CloudUserData>?>(null)
    val data: StateFlow<UiState<CloudUserData>?> = _data.asStateFlow()
    private var loading: Job? = null

    /** The form the shown text was made for: another one makes it stale. */
    var madeFor: JoinConfigForm? = null
        private set

    /** On a plain Talos cluster, the secrets confirmation was given for the shown text. */
    var confirmed = false
        private set

    /** Omni's join config ([form] is not used there), or the new member's config of [form]. */
    fun generate(form: JoinConfigForm) {
        loading?.cancel()
        confirmed = false
        madeFor = form
        _data.value = UiState.Loading
        loading = viewModelScope.launch {
            _data.value = uiStateOf { lifecycle.nodeCloudUserData(form.template, form.role, form.hostname, form.disk) }
        }
    }

    fun confirm() {
        confirmed = true
    }
}

/**
 * "Cloud host: user data": the text to paste as user data when the server is created at a cloud
 * provider, and the Talos cloud images to create it from. On Omni, Omni's join config, made on
 * open; on plain Talos, the role, host name and install disk of the new member first (Add a
 * node's fields, without the static address: clouds hand out addresses by DHCP).
 */
@Composable
fun CloudUserDataCard(
    omni: Boolean,
    vm: CloudUserDataViewModel = viewModel(key = "cloud-user-data", factory = factory { CloudUserDataViewModel(app.nodeLifecycleRepository) }),
    configVm: JoinConfigViewModel = viewModel(key = "cloud-user-data-form", factory = factory { JoinConfigViewModel(app.talosRepository, app.nodeLifecycleRepository) }),
) {
    val made by vm.data.collectAsStateWithLifecycle()
    val form by configVm.form.collectAsStateWithLifecycle()
    val members by configVm.members.collectAsStateWithLifecycle()
    LaunchedEffect(omni) { if (omni && vm.data.value == null) vm.generate(JoinConfigForm()) }
    // On plain Talos the shown text is the one of the form as it is now, else none.
    val data = made.takeIf { omni || vm.madeFor == form }

    SectionCard(stringResource(R.string.cloud_user_data_title)) {
        MutedText(stringResource(if (omni) R.string.cloud_user_data_omni_intro else R.string.cloud_user_data_intro))
        if (!omni) {
            when (val state = members) {
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(state.message, configVm::loadMembers)
                is UiState.Loaded -> {
                    TemplatePicker(joinTemplates(state.data), form.template) { configVm.pick(it, state.data) }
                    if (form.template.isNotBlank()) {
                        ConfigFields(form, emptyList(), configVm, withAddress = false)
                        IchorButton(
                            onClick = { vm.generate(form) },
                            enabled = form.complete && data != UiState.Loading,
                            modifier = Modifier.fillMaxWidth(),
                        ) { Text(stringResource(R.string.cloud_user_data_make)) }
                    }
                }
            }
        }
        when (val state = data) {
            null -> Unit
            UiState.Loading -> LoadingBox()
            is UiState.Failed -> ErrorBox(state.message, { vm.generate(form) })
            is UiState.Loaded -> UserDataBody(state.data, vm)
        }
    }
    (data as? UiState.Loaded)?.data?.takeIf { it.images.isNotEmpty() }?.let { loaded ->
        BootMediaCard(
            stringResource(R.string.cloud_user_data_images),
            loaded.images.map { BootValue(it.providerLabel, it.url) },
            hint = stringResource(R.string.cloud_user_data_images_hint, loaded.talosVersion, loaded.images.first().arch),
        )
    }
}

/** The size, the warnings, the preview (secrets hidden) and Copy / Share of the full text. */
@Composable
private fun UserDataBody(data: CloudUserData, vm: CloudUserDataViewModel) {
    val colors = LocalStatusColors.current
    var expanded by rememberSaveable { mutableStateOf(false) }
    val over = data.size > data.limit
    Text(
        stringResource(R.string.cloud_user_data_size, formatBytes(data.size.toLong()), formatBytes(data.limit.toLong())),
        style = MaterialTheme.typography.titleSmall,
        color = if (over) colors.warn else MaterialTheme.colorScheme.onSurface,
    )
    data.warnings.forEach { InfoNotice(it) }
    if (data.isOmni) Text(stringResource(R.string.boot_media_sensitive), color = colors.warn, style = MaterialTheme.typography.bodySmall)
    IchorTextButton(onClick = { expanded = !expanded }) {
        Text(stringResource(if (expanded) R.string.cloud_user_data_preview_hide else R.string.cloud_user_data_preview_show))
    }
    if (expanded) {
        SelectionContainer {
            Box(Modifier.heightIn(max = 360.dp).verticalScroll(rememberScrollState()).horizontalScroll(rememberScrollState())) {
                Text(data.redactedPreview, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, softWrap = false)
            }
        }
    }
    MutedText(stringResource(R.string.cloud_user_data_no_qr))
    UserDataActions(data, vm)
}

/** Copy and Share the full text; on plain Talos the first one asks to confirm the secrets go out. */
@Composable
private fun UserDataActions(data: CloudUserData, vm: CloudUserDataViewModel) {
    val context = LocalContext.current
    val title = stringResource(R.string.cloud_user_data_title)
    var pending by remember { mutableStateOf<(() -> Unit)?>(null) }
    fun guarded(action: () -> Unit) {
        if (data.isOmni || vm.confirmed) action() else pending = action
    }
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        IchorOutlinedButton(onClick = { guarded { copyToClipboard(context, title, data.userData, sensitive = true) } }, modifier = Modifier.weight(1f)) {
            Icon(Icons.Outlined.ContentCopy, contentDescription = null)
            Text(stringResource(R.string.cloud_user_data_copy))
        }
        IchorOutlinedButton(onClick = { guarded { shareText(context, data.userData, title) } }, modifier = Modifier.weight(1f)) {
            Icon(Icons.Outlined.Share, contentDescription = null)
            Text(stringResource(R.string.cloud_user_data_share))
        }
    }
    pending?.let { action ->
        ConfirmDialog(
            title = stringResource(R.string.cloud_user_data_confirm_title),
            text = stringResource(R.string.cloud_user_data_confirm_body),
            confirm = stringResource(R.string.cloud_user_data_confirm),
            onConfirm = {
                pending = null
                vm.confirm()
                action()
            },
            onDismiss = { pending = null },
        )
    }
}
