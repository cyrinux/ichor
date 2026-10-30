package name.levis.ichor.ui.backup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.BackupFileFormat
import name.levis.ichor.model.BackupSeal
import name.levis.ichor.model.ageRecipientLines
import name.levis.ichor.model.firstInvalidAgeRecipientLine
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichorgo.Ichorgo

/** What an age backup opens with: a passphrase (the app restores it) or age public keys (a computer does). */
internal enum class AgeSealMode { PASSPHRASE, RECIPIENTS }

/** What the new-backup dialog holds; never saved (the passphrase must not land in saved instance state). */
internal data class NewBackupInput(
    val format: BackupFileFormat = BackupFileFormat.ICHOR,
    val ageMode: AgeSealMode = AgeSealMode.PASSPHRASE,
    val passphrase: String = "",
    val again: String = "",
    val recipients: String = "",
) {
    val usesRecipients get() = format == BackupFileFormat.AGE && ageMode == AgeSealMode.RECIPIENTS

    override fun toString() = "NewBackupInput($format, $ageMode)"
}

/** Why [input] cannot seal a backup yet, null when it can. */
internal fun newBackupError(input: NewBackupInput): UiText? {
    if (!input.usesRecipients) return newPassphraseError(input.passphrase, input.again)
    val invalid = firstInvalidAgeRecipientLine(input.recipients)
    return when {
        invalid != null -> UiText.Res(R.string.backup_err_age_recipient, invalid)
        ageRecipientLines(input.recipients).isEmpty() -> UiText.Res(R.string.backup_err_age_recipients_empty)
        else -> null
    }
}

/** How [input] seals the backup; only for an input without [newBackupError]. */
internal fun newBackupSeal(input: NewBackupInput): BackupSeal = when {
    input.format == BackupFileFormat.ICHOR -> BackupSeal.Ichor(input.passphrase)
    input.usesRecipients -> BackupSeal.AgeRecipients(ageRecipientLines(input.recipients).map { it.lowercase() })
    else -> BackupSeal.AgePassphrase(input.passphrase)
}

/** Asks the file format, the passphrase or age keys, and whether the secrets go in, for a new backup. */
@Composable
internal fun NewBackupDialog(onConfirm: (BackupSeal, Boolean) -> Unit, onDismiss: () -> Unit) {
    var input by remember { mutableStateOf(NewBackupInput()) }
    // Off by default, asked again for every backup.
    var includeSecrets by remember { mutableStateOf(false) }
    val error = newBackupError(input)
    val showError = if (input.usesRecipients) input.recipients.isNotBlank() else input.again.isNotEmpty()
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.backup_create_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.backup_format), style = MaterialTheme.typography.labelLarge)
                Column(Modifier.selectableGroup()) {
                    ChoiceRow(input.format == BackupFileFormat.ICHOR, stringResource(R.string.backup_format_ichor)) {
                        input = input.copy(format = BackupFileFormat.ICHOR)
                    }
                    ChoiceRow(input.format == BackupFileFormat.AGE, stringResource(R.string.backup_format_age)) {
                        input = input.copy(format = BackupFileFormat.AGE)
                    }
                }
                if (input.format == BackupFileFormat.AGE) AgeModeChoice(input) { input = it }
                if (input.usesRecipients) {
                    RecipientsField(input.recipients) { input = input.copy(recipients = it) }
                } else {
                    Text(stringResource(R.string.backup_create_desc, Ichorgo.BackupMinPassphrase.toInt()), style = MaterialTheme.typography.bodySmall)
                    PassphraseField(input.passphrase, { input = input.copy(passphrase = it) }, stringResource(R.string.backup_passphrase))
                    PassphraseField(input.again, { input = input.copy(again = it) }, stringResource(R.string.backup_passphrase_again))
                }
                ToggleRow(
                    title = stringResource(R.string.backup_include_secrets),
                    checked = includeSecrets,
                    onChange = { includeSecrets = it },
                    description = stringResource(R.string.backup_include_secrets_warning),
                )
                if (error != null && showError) {
                    Text(error.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            IchorTextButton(onClick = { onConfirm(newBackupSeal(input), includeSecrets) }, enabled = error == null) {
                Text(stringResource(R.string.backup_create_confirm))
            }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun AgeModeChoice(input: NewBackupInput, onInput: (NewBackupInput) -> Unit) {
    MutedText(stringResource(R.string.backup_format_age_desc))
    Column(Modifier.selectableGroup()) {
        ChoiceRow(input.ageMode == AgeSealMode.PASSPHRASE, stringResource(R.string.backup_age_with_passphrase)) {
            onInput(input.copy(ageMode = AgeSealMode.PASSPHRASE))
        }
        ChoiceRow(input.ageMode == AgeSealMode.RECIPIENTS, stringResource(R.string.backup_age_with_recipients)) {
            onInput(input.copy(ageMode = AgeSealMode.RECIPIENTS))
        }
    }
}

@Composable
private fun RecipientsField(recipients: String, onRecipients: (String) -> Unit) {
    OutlinedTextField(
        value = recipients,
        onValueChange = onRecipients,
        label = { Text(stringResource(R.string.backup_age_recipients)) },
        placeholder = { Text("age1…", fontFamily = FontFamily.Monospace) },
        textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
        keyboardOptions = KeyboardOptions(autoCorrectEnabled = false),
        minLines = 3,
        maxLines = 6,
        modifier = Modifier.fillMaxWidth(),
    )
    MutedText(stringResource(R.string.backup_age_recipients_help))
}

@Composable
private fun ChoiceRow(selected: Boolean, label: String, onSelect: () -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier.fillMaxWidth().selectable(selected = selected, role = Role.RadioButton, onClick = onSelect),
    ) {
        RadioButton(selected = selected, onClick = null)
        Text(label)
    }
}
