package name.levis.ichor.ui.join

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FileOpen
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenu
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.GCPCheck
import name.levis.ichor.model.GCPCredential
import name.levis.ichor.model.gcpActivationLink
import name.levis.ichor.model.gcpKeyIdentity
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.kubeauth.openInBrowser
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.util.readBounded

/** A service account key is a few KB; anything far larger is not one. */
private const val MAX_KEY_FILE_BYTES = 64 * 1024

/**
 * The service account key (pasted, masked, or read from a file: only its email and project are
 * shown), the custom role to give it, a test, then sealed under its project id.
 */
@Composable
internal fun GCPKeyDialog(vm: GCPCreateViewModel, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var key by remember { mutableStateOf("") }
    var check by remember { mutableStateOf<UiState<GCPCheck>?>(null) }
    var saveFailed by remember { mutableStateOf(false) }
    var fileFailed by remember { mutableStateOf(false) }
    val identity = remember(key) { gcpKeyIdentity(key) }
    val tested = (check as? UiState.Loaded)?.data
    val roleTitle = stringResource(R.string.gcp_copy_role)
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri == null) return@rememberLauncherForActivityResult
        val text = runCatching {
            context.contentResolver.openInputStream(uri)?.use { readBounded(it, MAX_KEY_FILE_BYTES).decodeToString() }
        }.getOrNull()
        fileFailed = text == null
        if (text != null) {
            key = text
            check = null
        }
    }

    fun credential() = TalosJson.encodeToString(
        GCPCredential.serializer(),
        GCPCredential(serviceAccountJson = key.trim(), project = identity?.projectId.orEmpty()),
    )

    fun runTest() {
        check = UiState.Loading
        scope.launch { check = uiStateOf { vm.test(credential()) } }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.gcp_add_key)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.gcp_key_warning), color = colors.warn, style = MaterialTheme.typography.bodySmall)
                MutedText(stringResource(R.string.gcp_role_hint))
                IchorOutlinedButton(onClick = { scope.launch { copyToClipboard(context, roleTitle, vm.role(identity?.projectId.orEmpty())) } }) {
                    Text(roleTitle)
                }
                // Masked: the pasted text holds the private key, and is never shown back.
                OutlinedTextField(
                    value = key,
                    onValueChange = {
                        key = it
                        check = null
                    },
                    label = { Text(stringResource(R.string.gcp_key_field)) },
                    maxLines = 3,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                IchorOutlinedButton(onClick = { picker.launch(arrayOf("application/json", "text/plain", "*/*")) }) {
                    Icon(Icons.Outlined.FileOpen, contentDescription = null)
                    Text(stringResource(R.string.kube_field_from_file))
                }
                if (fileFailed) MutedText(stringResource(R.string.import_could_not_open))
                when {
                    key.isBlank() -> Unit
                    identity == null -> Text(stringResource(R.string.gcp_key_invalid), color = colors.warn, style = MaterialTheme.typography.bodySmall)
                    else -> MutedText(stringResource(R.string.gcp_key_parsed, identity.clientEmail, identity.projectId))
                }
                IchorOutlinedButton(onClick = ::runTest, enabled = identity != null && check != UiState.Loading) {
                    Text(stringResource(R.string.hetzner_test))
                }
                when (val state = check) {
                    null -> Unit
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> {
                        ErrorBox(state.message, ::runTest)
                        GCPActivationLink(state.message)
                    }
                    is UiState.Loaded -> CheckResult(state.data)
                }
                if (saveFailed) Text(stringResource(R.string.hetzner_token_not_saved), color = colors.bad, style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = {
            IchorButton(
                onClick = {
                    val account = tested?.project.orEmpty()
                    scope.launch {
                        if (vm.save(account, credential())) onDismiss() else saveFailed = true
                    }
                },
                enabled = tested != null && tested.project.isNotBlank() && (tested.canCreate || !tested.checked),
            ) { Text(stringResource(R.string.hetzner_save)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** What Test said: may create, may not (with the permissions to grant), or could not tell. */
@Composable
private fun CheckResult(data: GCPCheck) {
    val colors = LocalStatusColors.current
    val missing = data.missing.joinToString(", ")
    Text(
        when {
            !data.checked -> stringResource(R.string.gcp_check_unknown, data.account, data.project)
            data.canCreate -> stringResource(R.string.gcp_check_ok, data.account, data.project)
            else -> stringResource(R.string.gcp_check_no, data.account, data.project, missing)
        },
        color = if (data.checked && !data.canCreate) colors.warn else colors.ok,
        style = MaterialTheme.typography.bodySmall,
    )
    // Allowed to create, but the role is not whole (a public address, the options' reads).
    if (data.checked && data.canCreate && data.missing.isNotEmpty()) {
        MutedText(stringResource(R.string.gcp_check_missing, missing))
    }
}

/** "Open the page to enable it" when the core's message names where to enable an API. */
@Composable
internal fun GCPActivationLink(message: UiText) {
    val context = LocalContext.current
    val link = (message as? UiText.Raw)?.text?.let(::gcpActivationLink) ?: return
    IchorTextButton(onClick = { openInBrowser(context, link) }) { Text(stringResource(R.string.gcp_enable_api)) }
}

/** One choice among [choices] (value to label); a menu, since a project has dozens of zones and types. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun GCPDropdown(
    label: String,
    choices: List<Pair<String, String>>,
    selected: String,
    enabled: Boolean = true,
    onSelect: (String) -> Unit,
) {
    var open by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(expanded = open && enabled, onExpandedChange = { if (enabled) open = it }) {
        OutlinedTextField(
            value = choices.firstOrNull { it.first == selected }?.second ?: selected,
            onValueChange = {},
            readOnly = true,
            enabled = enabled,
            singleLine = true,
            label = { Text(label) },
            trailingIcon = { if (enabled) ExposedDropdownMenuDefaults.TrailingIcon(expanded = open) },
            modifier = Modifier.fillMaxWidth().menuAnchor(ExposedDropdownMenuAnchorType.PrimaryNotEditable, enabled),
        )
        ExposedDropdownMenu(expanded = open && enabled, onDismissRequest = { open = false }) {
            choices.forEach { (value, text) ->
                DropdownMenuItem(
                    text = { Text(text, maxLines = 2, overflow = TextOverflow.Ellipsis) },
                    onClick = {
                        open = false
                        if (value != selected) onSelect(value)
                    },
                    contentPadding = ExposedDropdownMenuDefaults.ItemContentPadding,
                )
            }
        }
    }
}
