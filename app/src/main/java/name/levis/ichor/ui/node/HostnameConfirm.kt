package name.levis.ichor.ui.node

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.confirmationCode
import name.levis.ichor.model.confirmationMatches
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * A confirmation that requires typing [hostname], GitHub-style, to avoid accidental taps on
 * risky node actions. [content] goes above the hostname field (options, warnings).
 */
@Composable
fun HostnameConfirmDialog(
    title: String,
    hostname: String,
    confirmLabel: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    emphasized: Boolean = false,
    enabled: Boolean = true,
    content: @Composable ColumnScope.() -> Unit = {},
) {
    // Per hostname: a dialog reused for another object starts empty.
    var typed by remember(hostname) { mutableStateOf("") }
    // [enabled] false: something forbids the action whatever is typed (e.g. a blocker).
    val matches = enabled && confirmationMatches(typed, hostname)
    val colors = LocalStatusColors.current

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                content()
                TypedConfirmPrompt(hostname)
                OutlinedTextField(
                    value = typed,
                    onValueChange = { typed = it },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm, enabled = matches) {
                Text(
                    confirmLabel,
                    color = if (matches) colors.bad else MaterialTheme.colorScheme.onSurfaceVariant,
                    fontWeight = if (emphasized) FontWeight.Bold else null,
                )
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** "Type … to confirm": the whole [name], or only its end when it is long (see [confirmationCode]). */
@Composable
fun TypedConfirmPrompt(name: String) {
    val code = confirmationCode(name)
    val prompt = if (code == name.trim()) stringResource(R.string.power_type_to_confirm, code) else stringResource(R.string.power_type_end_to_confirm, code, name.trim())
    Text(prompt, style = MaterialTheme.typography.bodyMedium)
}
