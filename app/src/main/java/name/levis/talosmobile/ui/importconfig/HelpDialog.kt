package name.levis.talosmobile.ui.importconfig

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

/** What each Talos role unlocks in this app (rules from Talos v1.14). */
private enum class PhoneRole(val label: String, val role: String, val unlocks: String) {
    READER("Read-only", "os:reader", "Monitoring only: overview, services, resources, logs, etcd, KubeSpan, live graphs, alerts. Recommended for a phone."),
    OPERATOR("Operator", "os:operator", "Everything read-only, plus reboot and shutdown."),
    ADMIN("Admin", "os:admin", "Everything, including the cluster health check, kubeconfig export and debug shells. Treat the phone like a laptop."),
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TalosconfigHelpDialog(onDismiss: () -> Unit) {
    var role by rememberSaveable { mutableStateOf(PhoneRole.READER) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Create a talosconfig for this phone") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(
                    "Generate a dedicated identity instead of copying your admin ~/.talos/config: if the phone " +
                        "is lost, its certificate only grants what you chose, and it expires on its own.",
                    style = MaterialTheme.typography.bodyMedium,
                )
                SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                    PhoneRole.entries.forEachIndexed { i, r ->
                        SegmentedButton(
                            selected = r == role,
                            onClick = { role = r },
                            shape = SegmentedButtonDefaults.itemShape(i, PhoneRole.entries.size),
                        ) { Text(r.label) }
                    }
                }
                Text(role.unlocks, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)

                Step("1. On your workstation, sign a certificate on one control-plane node:")
                Command("talosctl -n <control-plane-ip> config new talosconfig-phone --roles ${role.role} --crt-ttl 8760h")
                Step("2. List every node the app should show:")
                Command("talosctl --talosconfig talosconfig-phone config node <node-1> <node-2> …")
                Step("3. Bring it to the phone, then import it here:")
                Command("adb push talosconfig-phone /sdcard/Download/")
                Step("or show it as a QR code and use the QR tab:")
                Command("qrencode -t ansiutf8 -r talosconfig-phone")
                Text(
                    "Delete the copy in Download (and any QR image) afterwards: it contains the private key. " +
                        "The app stores it encrypted in hardware-backed storage.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Got it") } },
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
                Icon(Icons.Outlined.ContentCopy, contentDescription = "Copy command")
            }
        }
    }
}
