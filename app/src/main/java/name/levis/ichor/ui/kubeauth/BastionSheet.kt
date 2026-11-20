package name.levis.ichor.ui.kubeauth

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FileOpen
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KubeBastionRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.BastionProblem
import name.levis.ichor.model.KubeBastionHostKey
import name.levis.ichor.model.KubeBastionInfo
import name.levis.ichor.model.bastionOffered
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.readBounded

/** A private key is a few KB; anything far larger is not one. */
private const val MAX_PRIVATE_KEY_BYTES = 64 * 1024

/**
 * The SSH bastion of the cluster [context] (a kubeconfig cluster, or a Talos one with the choice
 * of what goes through it): host, user and a key or a password; the
 * host key shown for the user to compare before it is trusted; when set, edit (empty secrets
 * kept), replace a changed host key, or remove. [problem] is what the screen that opened it hit.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BastionSheet(context: String, problem: BastionProblem? = null, onDismiss: () -> Unit, onChanged: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    // One per opening: the host screen keeps its view models, a finished one must not come back.
    val opening = rememberSaveable { System.nanoTime() }
    val vm: BastionViewModel = viewModel(
        key = "bastion-$context-$opening",
        factory = factory { BastionViewModel(KubeBastionRepository(app.configRepository), context, problem) },
    )
    val state by vm.state.collectAsStateWithLifecycle()
    // Secrets never outlive the sheet.
    DisposableEffect(vm) { onDispose { vm.clear() } }
    LaunchedEffect(state.done) { if (state.done) onChanged() }
    var confirmRemove by remember { mutableStateOf(false) }
    if (confirmRemove) {
        IchorAlertDialog(
            onDismissRequest = { confirmRemove = false },
            text = { Text(stringResource(R.string.bastion_remove_confirm)) },
            confirmButton = {
                IchorTextButton(onClick = { confirmRemove = false; vm.remove() }) { Text(stringResource(R.string.bastion_remove)) }
            },
            dismissButton = { IchorTextButton(onClick = { confirmRemove = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(
            Modifier.padding(horizontal = 16.dp).padding(bottom = 24.dp).verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(stringResource(R.string.bastion_title), style = MaterialTheme.typography.titleLarge)
            MutedText(stringResource(R.string.bastion_intro))
            when {
                state.loading -> CircularProgressIndicator(Modifier.align(Alignment.CenterHorizontally))
                state.configured && state.pinMismatch -> ReplacePin(state, vm)
                else -> BastionForm(state, vm, onRemove = { confirmRemove = true })
            }
            state.error?.let { Text(it.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
        }
    }
}

@Composable
private fun BastionForm(state: BastionUiState, vm: BastionViewModel, onRemove: () -> Unit) {
    val enabled = !state.busy
    state.info?.takeIf { it.configured }?.let { info ->
        HostKeyLines(stringResource(R.string.bastion_pinned), info.hostKeyType, info.hostKeyFingerprint)
    }
    OutlinedTextField(
        value = state.host,
        onValueChange = vm::host,
        label = { Text(stringResource(R.string.bastion_host)) },
        singleLine = true,
        enabled = enabled,
        keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = KeyboardType.Uri),
        modifier = Modifier.fillMaxWidth(),
    )
    OutlinedTextField(
        value = state.user,
        onValueChange = vm::user,
        label = { Text(stringResource(R.string.bastion_user)) },
        singleLine = true,
        enabled = enabled,
        keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = KeyboardType.Ascii),
        modifier = Modifier.fillMaxWidth(),
    )
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        IchorFilterChip(selected = state.useKey, onClick = { vm.useKey(true) }, label = { Text(stringResource(R.string.bastion_auth_key)) }, enabled = enabled)
        IchorFilterChip(selected = !state.useKey, onClick = { vm.useKey(false) }, label = { Text(stringResource(R.string.bastion_auth_password)) }, enabled = enabled)
    }
    if (state.useKey) {
        PrivateKeyField(state.privateKey, vm::privateKey, enabled)
        SecretField(state.passphrase, vm::passphrase, stringResource(R.string.bastion_passphrase), enabled)
    } else {
        SecretField(state.password, vm::password, stringResource(R.string.bastion_password), enabled)
    }
    if (state.configured) MutedText(stringResource(R.string.bastion_keep_secret))
    if (state.info?.talos == true) ScopeChoice(state.scope, vm::scope, enabled)
    if (state.needsCheck) {
        state.checked?.let { CheckedKey(it) }
            ?: IchorOutlinedButton(onClick = vm::checkHostKey, enabled = state.canCheck) { Text(stringResource(R.string.bastion_check_host_key)) }
    }
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
        IchorButton(onClick = vm::save, enabled = state.canSave) {
            Text(stringResource(if (state.needsCheck) R.string.bastion_trust_save else R.string.bastion_save))
        }
        if (state.configured) IchorTextButton(onClick = onRemove, enabled = enabled) { Text(stringResource(R.string.bastion_remove)) }
        if (state.busy) CircularProgressIndicator(Modifier.padding(start = 8.dp))
    }
}

/** A Talos cluster: whether its Talos API goes through the bastion too, or only Kubernetes. */
@Composable
private fun ScopeChoice(scope: String, onScope: (String) -> Unit, enabled: Boolean) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        IchorFilterChip(
            selected = scope != KubeBastionInfo.SCOPE_KUBE,
            onClick = { onScope(KubeBastionInfo.SCOPE_ALL) },
            label = { Text(stringResource(R.string.bastion_scope_all)) },
            enabled = enabled,
        )
        IchorFilterChip(
            selected = scope == KubeBastionInfo.SCOPE_KUBE,
            onClick = { onScope(KubeBastionInfo.SCOPE_KUBE) },
            label = { Text(stringResource(R.string.bastion_scope_kube)) },
            enabled = enabled,
        )
    }
    MutedText(stringResource(R.string.bastion_scope_help))
}

/** The pinned key no longer matches: the old and the new fingerprints, then trust the new one. */
@Composable
private fun ReplacePin(state: BastionUiState, vm: BastionViewModel) {
    Text(stringResource(R.string.bastion_problem_host_key), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium)
    state.info?.let { HostKeyLines(stringResource(R.string.bastion_old_key), it.hostKeyType, it.hostKeyFingerprint) }
    val checked = state.checked
    if (checked == null) {
        IchorOutlinedButton(onClick = vm::checkHostKey, enabled = !state.busy) { Text(stringResource(R.string.bastion_replace_pin)) }
    } else {
        HostKeyLines(stringResource(R.string.bastion_new_key), checked.hostKeyType, checked.hostKeyFingerprint)
        MutedText(stringResource(R.string.bastion_compare))
        IchorButton(onClick = vm::replacePin, enabled = !state.busy) { Text(stringResource(R.string.bastion_trust_new)) }
    }
    if (state.busy) CircularProgressIndicator()
}

/** The key just read from the bastion, with the sentence asking to compare it before trusting. */
@Composable
private fun CheckedKey(key: KubeBastionHostKey) {
    HostKeyLines(stringResource(R.string.bastion_host_key), key.hostKeyType, key.hostKeyFingerprint)
    MutedText(stringResource(R.string.bastion_compare))
}

@Composable
private fun HostKeyLines(title: String, type: String, fingerprint: String) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(title, style = MaterialTheme.typography.labelLarge)
        Text(type, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        Text(fingerprint, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
    }
}

/** The private key: pasted or read from a file, never shown once entered (only that it is there). */
@Composable
private fun PrivateKeyField(value: String, onValue: (String) -> Unit, enabled: Boolean) {
    val context = LocalContext.current
    var unreadable by remember { mutableStateOf(false) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri == null) return@rememberLauncherForActivityResult
        val text = runCatching {
            context.contentResolver.openInputStream(uri)?.use { readBounded(it, MAX_PRIVATE_KEY_BYTES).decodeToString() }
        }.getOrNull()
        unreadable = text.isNullOrBlank()
        text?.takeIf { it.isNotBlank() }?.let(onValue)
    }
    if (value.isNotBlank()) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.bastion_key_entered), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            IchorTextButton(onClick = { onValue("") }, enabled = enabled) { Text(stringResource(R.string.bastion_key_clear)) }
        }
        return
    }
    OutlinedTextField(
        value = "",
        onValueChange = onValue,
        label = { Text(stringResource(R.string.bastion_private_key)) },
        minLines = 2,
        maxLines = 3,
        enabled = enabled,
        textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
        keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = KeyboardType.Password),
        modifier = Modifier.fillMaxWidth(),
    )
    IchorOutlinedButton(onClick = { picker.launch(arrayOf("*/*")) }, enabled = enabled) {
        Icon(Icons.Outlined.FileOpen, contentDescription = null)
        Text(stringResource(R.string.bastion_key_from_file))
    }
    if (unreadable) {
        Text(stringResource(R.string.import_could_not_open), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
    }
}

/** "Bastion settings" when [message] is a bastion failure: opens the sheet; [onChanged] after a save or removal. */
@Composable
fun BastionAction(message: UiText, onChanged: () -> Unit) {
    if (message !is UiText.Bastion) return
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    // The cluster on screen, when it can have a bastion (not Omni, not the demo).
    val target = config?.activeSummary?.takeIf { bastionOffered(it, masked = false) }?.name ?: return
    var open by remember { mutableStateOf(false) }
    IchorButton(onClick = { open = true }) { Text(stringResource(R.string.bastion_settings)) }
    if (open) {
        BastionSheet(
            context = target,
            problem = message.problem,
            onDismiss = { open = false },
            onChanged = {
                open = false
                onChanged()
            },
        )
    }
}
