package name.levis.ichor.ui.importconfig

import androidx.annotation.StringRes
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.DialogProperties

/** What each Talos role unlocks in this app (rules from Talos v1.14). */
private enum class PhoneRole(@StringRes val label: Int, val role: String, @StringRes val unlocks: Int) {
    READER(R.string.help_role_reader, "os:reader", R.string.help_role_reader_desc),
    OPERATOR(R.string.help_role_operator, "os:operator", R.string.help_role_operator_desc),
    ADMIN(R.string.help_role_admin, "os:admin", R.string.help_role_admin_desc),
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TalosconfigHelpDialog(onDismiss: () -> Unit) {
    var role by rememberSaveable { mutableStateOf(PhoneRole.READER) }

    // The platform default dialog width is too narrow for the role tabs on phones.
    AlertDialog(
        onDismissRequest = onDismiss,
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
        properties = DialogProperties(usePlatformDefaultWidth = false),
        title = { Text(stringResource(R.string.help_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(
                    stringResource(R.string.help_intro),
                    style = MaterialTheme.typography.bodyMedium,
                )
                SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                    PhoneRole.entries.forEachIndexed { i, r ->
                        SegmentedButton(
                            selected = r == role,
                            onClick = { role = r },
                            shape = SegmentedButtonDefaults.itemShape(i, PhoneRole.entries.size),
                            icon = {},
                        ) { Text(stringResource(r.label)) }
                    }
                }
                Text(stringResource(role.unlocks), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)

                Step(stringResource(R.string.help_step1))
                Command("talosctl -n <control-plane-ip> config new talosconfig-phone --roles ${role.role} --crt-ttl 8760h")
                Step(stringResource(R.string.help_step2))
                Command("talosctl --talosconfig talosconfig-phone config node <node-1> <node-2> …")
                Step(stringResource(R.string.help_step3))
                Command("adb push talosconfig-phone /sdcard/Download/")
                Step(stringResource(R.string.help_step_qr))
                Command("qrencode -t ansiutf8 -r talosconfig-phone")
                Text(
                    stringResource(R.string.help_cleanup),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.help_got_it)) } },
    )
}

@Composable
private fun Step(text: String) {
    Text(text, style = MaterialTheme.typography.labelLarge)
}

@Composable
private fun Command(command: String) {
    val clipboard = LocalClipboardManager.current
    Surface(color = MaterialTheme.colorScheme.surfaceVariant, shape = MaterialTheme.shapes.small) {
        Row(Modifier.padding(start = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(
                command,
                fontFamily = FontFamily.Monospace,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.weight(1f).padding(vertical = 8.dp),
            )
            IconButton(onClick = { clipboard.setText(AnnotatedString(command)) }) {
                Icon(Icons.Outlined.ContentCopy, contentDescription = stringResource(R.string.help_copy_command))
            }
        }
    }
}
