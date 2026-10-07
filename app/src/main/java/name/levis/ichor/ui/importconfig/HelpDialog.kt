package name.levis.ichor.ui.importconfig

import android.content.ClipData
import androidx.annotation.StringRes
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.TextAutoSize
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
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.ClipEntry
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.DialogProperties
import kotlinx.coroutines.launch
import name.levis.ichor.ui.components.MutedText

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

    AlertDialog(
        onDismissRequest = onDismiss,
        // The platform default leaves wide side margins; the role selector
        // and the commands need the room.
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
                            // The fill already marks the selection; a checkmark
                            // would squeeze the label onto two lines.
                            icon = {},
                        ) {
                            Text(
                                stringResource(r.label),
                                maxLines = 1,
                                softWrap = false,
                                autoSize = TextAutoSize.StepBased(minFontSize = 10.sp, maxFontSize = MaterialTheme.typography.labelLarge.fontSize),
                            )
                        }
                    }
                }
                MutedText(stringResource(role.unlocks))

                Step(stringResource(R.string.help_step1))
                Command("talosctl -n <control-plane-ip> config new talosconfig-phone --roles ${role.role} --crt-ttl 8760h")
                Step(stringResource(R.string.help_step2))
                Command("talosctl --talosconfig talosconfig-phone config node <node-1> <node-2> …")
                Step(stringResource(R.string.help_step3))
                Command("adb push talosconfig-phone /sdcard/Download/")
                Step(stringResource(R.string.help_step_qr))
                Command("qrencode -t ansiutf8 -r talosconfig-phone")
                Step(stringResource(R.string.help_qr_compressed))
                Command(COMPRESSED_QR_COMMAND)
                MutedText(stringResource(R.string.help_cleanup))
                KubeconfigHelp()
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.help_got_it)) } },
    )
}

/**
 * A config too large for one QR code (a kubeconfig with an embedded client certificate)
 * fits once compressed: the scanner reads raw gzip in a binary code (see
 * go/ichorgo/qr_payload.go), and the importer expands an "ichor-config:" text payload too
 * (go/ichorgo/import_text.go). Works for a kubeconfig as well.
 */
private const val COMPRESSED_QR_COMMAND = "gzip -9 < talosconfig-phone | qrencode -8 -t ansiutf8"

/** Clusters added from a kubeconfig: what works today, and how to make one the app can read. */
@Composable
private fun KubeconfigHelp() {
    Text(stringResource(R.string.help_kube_title), style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 8.dp))
    Text(stringResource(R.string.help_kube_body), style = MaterialTheme.typography.bodyMedium)
    Step(stringResource(R.string.help_kube_flatten))
    Command("kubectl config view --flatten --minify > kubeconfig-phone")
    MutedText(stringResource(R.string.help_kube_later))
}

@Composable
private fun Step(text: String) {
    Text(text, style = MaterialTheme.typography.labelLarge)
}

@Composable
private fun Command(command: String) {
    val clipboard = LocalClipboard.current
    val scope = rememberCoroutineScope()
    Surface(color = MaterialTheme.colorScheme.surfaceVariant, shape = MaterialTheme.shapes.small) {
        Row(Modifier.padding(start = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(
                command,
                fontFamily = FontFamily.Monospace,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.weight(1f).padding(vertical = 8.dp),
            )
            IconButton(onClick = { scope.launch { clipboard.setClipEntry(ClipEntry(ClipData.newPlainText(command, command))) } }) {
                Icon(Icons.Outlined.ContentCopy, contentDescription = stringResource(R.string.help_copy_command))
            }
        }
    }
}
