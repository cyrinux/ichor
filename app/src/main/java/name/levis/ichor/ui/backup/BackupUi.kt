package name.levis.ichor.ui.backup

import android.content.Context
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.model.backupFileName
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.readBounded
import name.levis.talosmobile.Talosmobile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.LocalDate

/** A backup is a few KB; anything much bigger is not one. */
private const val MAX_BACKUP_BYTES = 5 * 1024 * 1024

private val minPassphrase = Talosmobile.BackupMinPassphrase.toInt()

/**
 * Launches the file pickers and shows the dialogs of [vm]'s current step; returns a picker to start a restore.
 * [replaceWarning]: the passphrase dialog recalls that a restore replaces what is stored (no confirmation came first).
 */
@Composable
fun BackupFlow(vm: BackupViewModel, onRestored: () -> Unit = {}, replaceWarning: Boolean = false): () -> Unit {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val state by vm.state.collectAsStateWithLifecycle()

    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/octet-stream")) { uri ->
        val ready = vm.state.value as? BackupState.Saving
        if (uri == null || ready == null) return@rememberLauncherForActivityResult vm.reset()
        scope.launch {
            runCatching { writeBytes(context, uri, ready.file) }.fold(onSuccess = { vm.saved() }, onFailure = { vm.fail() })
        }
    }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch { runCatching { readBytes(context, uri) }.fold(onSuccess = vm::picked, onFailure = { vm.fail() }) }
    }

    when (val state = state) {
        BackupState.NewPassphrase -> NewPassphraseDialog(onConfirm = vm::seal, onDismiss = vm::reset)
        is BackupState.ReadyToSave -> LaunchedEffect(state) {
            if (vm.openSaver()) saver.launch(backupFileName(LocalDate.now()))
        }
        is BackupState.Passphrase -> UnlockDialog(state.error, working = false, replaceWarning, onConfirm = vm::restore, onDismiss = vm::reset)
        BackupState.Restoring -> UnlockDialog(null, working = true, replaceWarning, onConfirm = {}, onDismiss = {})
        is BackupState.Restored -> LaunchedEffect(state) {
            val outcome = vm.takeRestored() ?: return@LaunchedEffect
            onRestored()
            if (outcome.languageChanged && AppLocale.apply(context, outcome.language)) {
                context.findFragmentActivity()?.recreate()
            }
        }
        else -> Unit
    }
    return { picker.launch(arrayOf("*/*")) }
}

/** The outcome of the last step, under the buttons that started it. */
@Composable
fun BackupStatus(state: BackupState) {
    val text = when (state) {
        BackupState.Sealing -> stringResource(R.string.backup_sealing)
        BackupState.Saved -> stringResource(R.string.backup_saved)
        is BackupState.Restored, BackupState.RestoreDone -> stringResource(R.string.backup_restored)
        is BackupState.Failed -> state.message.asString()
        else -> return
    }
    Text(
        text,
        style = MaterialTheme.typography.bodySmall,
        color = if (state is BackupState.Failed) LocalStatusColors.current.bad else MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

@Composable
private fun NewPassphraseDialog(onConfirm: (String) -> Unit, onDismiss: () -> Unit) {
    // Not saveable: the passphrase must not land in saved instance state.
    var passphrase by remember { mutableStateOf("") }
    var again by remember { mutableStateOf("") }
    val error = newPassphraseError(passphrase, again)
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.backup_create_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.backup_create_desc, minPassphrase), style = MaterialTheme.typography.bodySmall)
                PassphraseField(passphrase, { passphrase = it }, stringResource(R.string.backup_passphrase))
                PassphraseField(again, { again = it }, stringResource(R.string.backup_passphrase_again))
                if (error != null && again.isNotEmpty()) {
                    Text(error.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            TextButton(onClick = { onConfirm(passphrase) }, enabled = error == null) { Text(stringResource(R.string.backup_create_confirm)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun UnlockDialog(
    error: UiText?,
    working: Boolean,
    replaceWarning: Boolean,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var passphrase by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = { if (!working) onDismiss() },
        title = { Text(stringResource(R.string.backup_restore_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (replaceWarning) {
                    Text(stringResource(R.string.backup_restore_replace_warning), style = MaterialTheme.typography.bodySmall)
                }
                Text(stringResource(R.string.backup_restore_desc), style = MaterialTheme.typography.bodySmall)
                PassphraseField(passphrase, { passphrase = it }, stringResource(R.string.backup_passphrase), enabled = !working)
                if (working) CircularProgressIndicator()
                error?.let { Text(it.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
            }
        },
        confirmButton = {
            TextButton(onClick = { onConfirm(passphrase) }, enabled = !working && passphrase.isNotEmpty()) {
                Text(stringResource(R.string.backup_restore_confirm))
            }
        },
        dismissButton = { TextButton(onClick = onDismiss, enabled = !working) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun PassphraseField(value: String, onValue: (String) -> Unit, label: String, enabled: Boolean = true) {
    var visible by remember { mutableStateOf(false) }
    OutlinedTextField(
        value = value,
        onValueChange = onValue,
        label = { Text(label) },
        singleLine = true,
        enabled = enabled,
        visualTransformation = if (visible) VisualTransformation.None else PasswordVisualTransformation(),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false, imeAction = ImeAction.Next),
        trailingIcon = {
            IconButton(onClick = { visible = !visible }) {
                Icon(
                    if (visible) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility,
                    contentDescription = stringResource(if (visible) R.string.backup_passphrase_hide else R.string.backup_passphrase_show),
                )
            }
        },
        modifier = Modifier.fillMaxWidth(),
    )
}

/** Why [passphrase] cannot seal a backup yet, null when it can. */
internal fun newPassphraseError(passphrase: String, again: String): UiText? = when {
    passphrase.codePointCount(0, passphrase.length) < minPassphrase -> UiText.Res(R.string.backup_err_passphrase_short, minPassphrase)
    passphrase != again -> UiText.Res(R.string.backup_err_passphrase_mismatch)
    else -> null
}

private suspend fun writeBytes(context: Context, uri: Uri, bytes: ByteArray) = withContext(Dispatchers.IO) {
    // "wt" truncates when the user picked an existing file.
    val stream = context.contentResolver.openOutputStream(uri, "wt") ?: throw LocalizedException(UiText.Res(R.string.backup_err_open))
    stream.use { it.write(bytes) }
}

internal suspend fun readBytes(context: Context, uri: Uri): ByteArray = withContext(Dispatchers.IO) {
    context.contentResolver.openInputStream(uri)?.use { readBounded(it, MAX_BACKUP_BYTES) }
        ?: throw LocalizedException(UiText.Res(R.string.backup_err_open))
}
