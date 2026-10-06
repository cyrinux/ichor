package name.levis.ichor.ui.etcd

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.model.SnapshotEncryption
import name.levis.ichor.model.SnapshotMode
import name.levis.ichor.model.SnapshotRecipient
import name.levis.ichor.ui.backup.PassphraseField
import name.levis.ichor.ui.backup.newPassphraseError
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.theme.LocalStatusColors

/** What the keys field currently holds, as checked by Go. */
private sealed interface KeysCheck {
    data object Empty : KeysCheck
    /** The text changed and Go has not answered yet: nothing may be saved meanwhile. */
    data object Checking : KeysCheck
    data class Valid(val recipients: List<SnapshotRecipient>) : KeysCheck
    data class Invalid(val message: String) : KeysCheck
}

/**
 * Chooses how the snapshot from [hostname] is protected: age public keys (default, prefilled
 * with [savedKeys]), an age passphrase, or nothing (with the clear-text warning).
 */
@Composable
fun SnapshotEncryptionDialog(
    hostname: String,
    savedKeys: String,
    checkKeys: suspend (String) -> List<SnapshotRecipient>,
    onConfirm: (SnapshotEncryption) -> Unit,
    onDismiss: () -> Unit,
) {
    var mode by remember { mutableStateOf(if (savedKeys.isNotBlank()) SnapshotMode.KEYS else SnapshotMode.PASSPHRASE) }
    var keys by remember { mutableStateOf(savedKeys) }
    var check by remember { mutableStateOf<KeysCheck>(KeysCheck.Empty) }
    var passphrase by remember { mutableStateOf("") }
    var again by remember { mutableStateOf("") }

    LaunchedEffect(keys) {
        if (keys.isBlank()) {
            check = KeysCheck.Empty
            return@LaunchedEffect
        }
        check = KeysCheck.Checking
        delay(KEYS_DEBOUNCE_MS)
        check = try {
            KeysCheck.Valid(checkKeys(keys))
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            KeysCheck.Invalid(e.message.orEmpty())
        }
    }

    val passphraseError = newPassphraseError(passphrase, again)
    val ready = when (mode) {
        SnapshotMode.KEYS -> check is KeysCheck.Valid
        SnapshotMode.PASSPHRASE -> passphraseError == null
        SnapshotMode.NONE -> true
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.etcd_snapshot_protect_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.etcd_snapshot_protect_body, hostname), style = MaterialTheme.typography.bodySmall)
                Column(Modifier.selectableGroup()) {
                    ModeRow(SnapshotMode.KEYS, mode, R.string.etcd_snapshot_mode_keys) { mode = it }
                    ModeRow(SnapshotMode.PASSPHRASE, mode, R.string.etcd_snapshot_mode_passphrase) { mode = it }
                    ModeRow(SnapshotMode.NONE, mode, R.string.etcd_snapshot_mode_none) { mode = it }
                }
                when (mode) {
                    SnapshotMode.KEYS -> KeysInput(keys, check) { keys = it }
                    SnapshotMode.PASSPHRASE -> {
                        PassphraseField(passphrase, { passphrase = it }, stringResource(R.string.backup_passphrase))
                        PassphraseField(again, { again = it }, stringResource(R.string.backup_passphrase_again))
                        if (passphrase.isNotEmpty()) {
                            passphraseError?.let {
                                Text(it.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                            }
                        }
                        MutedText(stringResource(R.string.etcd_snapshot_passphrase_hint))
                    }
                    SnapshotMode.NONE -> Text(
                        stringResource(R.string.etcd_snapshot_warning_body, hostname),
                        color = LocalStatusColors.current.bad,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = ready,
                onClick = {
                    onConfirm(
                        when (mode) {
                            SnapshotMode.KEYS -> SnapshotEncryption.Keys(keys.trim())
                            SnapshotMode.PASSPHRASE -> SnapshotEncryption.Passphrase(passphrase)
                            SnapshotMode.NONE -> SnapshotEncryption.None
                        },
                    )
                },
            ) { Text(stringResource(R.string.etcd_snapshot_continue)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun ModeRow(value: SnapshotMode, selected: SnapshotMode, label: Int, onSelect: (SnapshotMode) -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier.fillMaxWidth().selectable(selected = value == selected, role = Role.RadioButton) { onSelect(value) },
    ) {
        RadioButton(selected = value == selected, onClick = null)
        Text(stringResource(label))
    }
}

@Composable
private fun KeysInput(keys: String, check: KeysCheck, onKeys: (String) -> Unit) {
    OutlinedTextField(
        value = keys,
        onValueChange = onKeys,
        label = { Text(stringResource(R.string.etcd_snapshot_keys_label)) },
        placeholder = { Text("age1…\nssh-ed25519 AAAA… you@laptop\nage1tag1… (YubiKey)", fontFamily = FontFamily.Monospace) },
        textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
        minLines = 3,
        maxLines = 6,
        modifier = Modifier.fillMaxWidth(),
    )
    when (check) {
        KeysCheck.Empty, KeysCheck.Checking -> MutedText(stringResource(R.string.etcd_snapshot_keys_help))
        is KeysCheck.Valid -> Text(
            stringResource(R.string.etcd_snapshot_keys_for, check.recipients.joinToString { it.describe() }),
            color = LocalStatusColors.current.ok,
            style = MaterialTheme.typography.bodySmall,
        )
        is KeysCheck.Invalid -> Text(check.message, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
    }
}

private fun SnapshotRecipient.describe() = if (comment.isEmpty()) type else "$type ($comment)"

/** The commands restoring a saved snapshot on a Unix machine, with a copy button. */
@Composable
fun SnapshotRestoreDialog(commands: String, onDismiss: () -> Unit) {
    val context = LocalContext.current
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.etcd_snapshot_restore_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.etcd_snapshot_restore_body), style = MaterialTheme.typography.bodySmall)
                SelectionContainer {
                    Text(commands, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                }
            }
        },
        confirmButton = {
            TextButton(onClick = { copyToClipboard(context, "talosctl", commands) }) { Text(stringResource(R.string.etcd_snapshot_restore_copy)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) } },
    )
}

private const val KEYS_DEBOUNCE_MS = 300L
