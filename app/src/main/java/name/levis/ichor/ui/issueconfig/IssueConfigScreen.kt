package name.levis.ichor.ui.issueconfig

import android.content.Context
import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.CertValidity
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.ISSUABLE_ROLES
import name.levis.ichor.model.IssueMode
import name.levis.ichor.model.MAX_QR_BYTES
import name.levis.ichor.model.allows
import name.levis.ichor.model.issuedFileName
import name.levis.ichor.model.renewalRoles
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.SecureWhile
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.RoleNotice
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.importconfig.certExpiry
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import java.text.DateFormat
import java.util.Date

/**
 * Issues a talosconfig (os:admin): renews this context's certificate in place, or creates
 * a separate config for another device, shown as a QR code and/or saved to a file.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun IssueConfigScreen(
    onBack: () -> Unit,
    vm: IssueConfigViewModel = viewModel(factory = factory { IssueConfigViewModel(app.talosRepository, app.configRepository) }),
) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val summary = config?.activeSummary
    val state by vm.state.collectAsStateWithLifecycle()
    val form by vm.form.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var confirming by remember { mutableStateOf<IssueRequest?>(null) }

    // The issued config's key must not outlive the screen, nor reach screenshots or recents.
    val issued = state as? IssueState.Issued
    SecureWhile(issued != null)
    fun leave() {
        vm.clear()
        onBack()
    }
    BackHandler(enabled = issued != null, onBack = ::leave)

    // Like power actions: with the app lock on, a fresh fingerprint/PIN before issuing.
    fun confirmed(request: IssueRequest) {
        confirming = null
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            vm.issue(request)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.issue_auth_title), request.context)) {
                AuthResult.Success -> vm.issue(request)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message)
            }
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.issue_title))
                        summary?.let { Text(it.name, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace) }
                    }
                },
                navigationIcon = { IconButton(onClick = ::leave) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
            )
        },
    ) { padding ->
        Column(
            Modifier.padding(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            when {
                summary == null -> Unit
                !summary.allows(Feature.ISSUE_CONFIG) -> RoleNotice(Feature.ISSUE_CONFIG, summary.roles)
                else -> when (val s = state) {
                    is IssueState.Renewed -> RenewedContent(s, onDone = ::leave)
                    is IssueState.Issued -> IssuedContent(s, onDone = ::leave)
                    else -> IssueFormContent(
                        summary = summary,
                        form = form,
                        state = s,
                        vm = vm,
                        onIssue = { confirming = it },
                    )
                }
            }
        }
    }

    confirming?.let { request ->
        IssueConfirmDialog(request, onConfirm = { confirmed(request) }, onDismiss = { confirming = null })
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun IssueFormContent(
    summary: ContextSummary,
    form: IssueForm,
    state: IssueState,
    vm: IssueConfigViewModel,
    onIssue: (IssueRequest) -> Unit,
) {
    val busy = state is IssueState.Issuing
    val currentRoles = renewalRoles(summary.roles)
    val roles = when (form.mode) {
        IssueMode.RENEW -> currentRoles.ifEmpty { form.renewRoles }
        IssueMode.OTHER_DEVICE -> form.sharedRoles
    }

    ModeOption(
        selected = form.mode == IssueMode.RENEW,
        title = stringResource(R.string.issue_mode_renew),
        description = stringResource(R.string.issue_mode_renew_desc),
        onClick = { vm.setMode(IssueMode.RENEW) },
    )
    ModeOption(
        selected = form.mode == IssueMode.OTHER_DEVICE,
        title = stringResource(R.string.issue_mode_share),
        description = stringResource(R.string.issue_mode_share_desc),
        onClick = { vm.setMode(IssueMode.OTHER_DEVICE) },
    )

    SectionTitle(stringResource(R.string.issue_roles))
    if (form.mode == IssueMode.RENEW && currentRoles.isNotEmpty()) {
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp)) {
                InfoRow(stringResource(R.string.common_label_roles), currentRoles.joinToString())
                InfoRow(stringResource(R.string.issue_current_cert), certExpiry(summary.certNotAfter))
            }
        }
    } else {
        ISSUABLE_ROLES.forEach { role ->
            RoleOption(
                role = role,
                checked = role in roles,
                onToggle = {
                    if (form.mode == IssueMode.RENEW) vm.toggleRenewRole(role) else vm.toggleSharedRole(role)
                },
            )
        }
        if (roles.isEmpty()) {
            Text(stringResource(R.string.issue_roles_pick_one), color = LocalStatusColors.current.warn, style = MaterialTheme.typography.bodySmall)
        }
    }

    SectionTitle(stringResource(R.string.issue_validity))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        CertValidity.entries.forEach { v ->
            FilterChip(selected = form.validity == v, onClick = { vm.setValidity(v) }, label = { Text(stringResource(v.label)) })
        }
    }
    Text(
        stringResource(R.string.issue_valid_until, formatDate(form.validity.expiresAt(System.currentTimeMillis()))),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )

    (state as? IssueState.Failed)?.let {
        Text(it.message.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium)
    }

    Button(
        onClick = { onIssue(IssueRequest(form.mode, summary.name, roles, form.validity)) },
        enabled = !busy && roles.isNotEmpty(),
        modifier = Modifier.fillMaxWidth(),
    ) {
        if (busy) {
            CircularProgressIndicator(Modifier.size(18.dp).padding(end = 8.dp), strokeWidth = 2.dp)
            Text(stringResource(R.string.issue_issuing))
        } else {
            Text(stringResource(if (form.mode == IssueMode.RENEW) R.string.issue_renew_action else R.string.issue_share_action))
        }
    }
}

@Composable
private fun ModeOption(selected: Boolean, title: String, description: String, onClick: () -> Unit) {
    Card(Modifier.fillMaxWidth().selectable(selected = selected, role = Role.RadioButton, onClick = onClick)) {
        Row(Modifier.padding(12.dp), verticalAlignment = Alignment.Top) {
            RadioButton(selected = selected, onClick = null)
            Column(Modifier.padding(start = 12.dp)) {
                Text(title, style = MaterialTheme.typography.titleMedium)
                Text(description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun RoleOption(role: String, checked: Boolean, onToggle: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().selectable(selected = checked, role = Role.Checkbox, onClick = onToggle),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = checked, onCheckedChange = null, modifier = Modifier.padding(end = 12.dp))
        Column {
            Text(role, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            Text(stringResource(roleDescription(role)), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

private fun roleDescription(role: String): Int = when (role) {
    "os:admin" -> R.string.issue_role_admin_desc
    "os:operator" -> R.string.issue_role_operator_desc
    "os:etcd:backup" -> R.string.issue_role_etcd_backup_desc
    else -> R.string.issue_role_reader_desc
}

@Composable
private fun IssueConfirmDialog(request: IssueRequest, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val until = formatDate(request.validity.expiresAt(System.currentTimeMillis()))
    val roles = request.roles.joinToString()
    val renew = request.mode == IssueMode.RENEW
    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Icon(Icons.Outlined.Warning, contentDescription = null, tint = LocalStatusColors.current.warn) },
        title = { Text(stringResource(if (renew) R.string.issue_confirm_renew_title else R.string.issue_confirm_share_title)) },
        text = {
            Text(
                stringResource(
                    if (renew) R.string.issue_confirm_renew_body else R.string.issue_confirm_share_body,
                    request.context,
                    roles,
                    until,
                ),
            )
        },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                Text(stringResource(if (renew) R.string.issue_renew_action else R.string.issue_share_action))
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun RenewedContent(state: IssueState.Renewed, onDone: () -> Unit) {
    val ok = LocalStatusColors.current.ok
    Card(colors = CardDefaults.cardColors(containerColor = ok.copy(alpha = 0.12f)), modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(stringResource(R.string.issue_renewed_title), style = MaterialTheme.typography.titleMedium, color = ok)
            Text(
                stringResource(R.string.issue_renewed_body, state.request.context, formatDate(state.certNotAfter * 1000)),
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
    Button(onClick = onDone, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.issue_done)) }
}

private sealed interface SaveState {
    data object Idle : SaveState
    data object Saved : SaveState
    data class Failed(val message: UiText) : SaveState
}

@Composable
private fun IssuedContent(state: IssueState.Issued, onDone: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val bad = LocalStatusColors.current.bad
    var save by remember { mutableStateOf<SaveState>(SaveState.Idle) }
    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/yaml")) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch {
            save = runCatching { withContext(Dispatchers.IO) { writeText(context, uri, state.yaml) } }.fold(
                onSuccess = { SaveState.Saved },
                onFailure = { SaveState.Failed(it.uiText()) },
            )
        }
    }

    Card(colors = CardDefaults.cardColors(containerColor = bad.copy(alpha = 0.12f)), modifier = Modifier.fillMaxWidth()) {
        Text(
            stringResource(R.string.issue_result_warning, state.request.context, state.request.roles.joinToString(), formatDate(state.expiresAt)),
            color = bad,
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.padding(12.dp),
        )
    }
    if (state.fitsInQr) {
        QrCode(
            text = state.yaml,
            contentDescription = stringResource(R.string.issue_qr_description),
            onTooLarge = { TooLargeForQr(state.size) },
        )
        Text(stringResource(R.string.issue_qr_hint), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    } else {
        TooLargeForQr(state.size)
    }
    OutlinedButton(
        onClick = { saver.launch(issuedFileName(state.request.context, state.request.roles)) },
        modifier = Modifier.fillMaxWidth(),
    ) { Text(stringResource(R.string.issue_save_file)) }
    when (val s = save) {
        SaveState.Saved -> Text(stringResource(R.string.issue_saved), style = MaterialTheme.typography.bodySmall)
        is SaveState.Failed -> Text(s.message.asString(), color = bad, style = MaterialTheme.typography.bodySmall)
        SaveState.Idle -> Unit
    }
    Text(stringResource(R.string.issue_memory_only), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    Button(onClick = onDone, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.issue_done)) }
}

@Composable
private fun TooLargeForQr(size: Int) {
    Text(
        stringResource(R.string.issue_qr_too_large, size, MAX_QR_BYTES),
        color = LocalStatusColors.current.warn,
        style = MaterialTheme.typography.bodyMedium,
    )
}

private fun formatDate(epochMillis: Long): String = DateFormat.getDateInstance(DateFormat.MEDIUM).format(Date(epochMillis))

private fun writeText(context: Context, uri: Uri, text: String) {
    // "wt" truncates when the user picked an existing file.
    val stream = context.contentResolver.openOutputStream(uri, "wt") ?: throw LocalizedException(UiText.Res(R.string.issue_open_failed))
    stream.use { it.write(text.encodeToByteArray()) }
}
