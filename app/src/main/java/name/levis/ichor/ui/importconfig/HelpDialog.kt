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
import name.levis.ichor.ui.design.IchorAlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import name.levis.ichor.ui.design.IchorIconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
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
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
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

/**
 * What each provider chip of the add screen asks for, from the fields DiscoverClusters takes
 * (go/ichorgo/kube_discover.go KubeDiscoverFields / KubeDiscoverOptions). Omni has its own block.
 */
private val CLOUD_LINES = listOf(
    R.string.help_cloud_eks,
    R.string.help_cloud_gke,
    R.string.help_cloud_aks,
    R.string.help_cloud_digitalocean,
    R.string.help_cloud_rancher,
)

/** The three ways to add a cluster, most common first: a cloud account, a kubeconfig, a Talos identity. */
@Composable
fun AddClusterHelpDialog(onDismiss: () -> Unit) {
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        // The platform default leaves wide side margins; the role selector
        // and the commands need the room.
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
        properties = DialogProperties(usePlatformDefaultWidth = false),
        title = { Text(stringResource(R.string.help_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                CloudHelp()
                KubeconfigHelp()
                TalosHelp()
                BringToPhoneHelp()
            }
        },
        confirmButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.help_got_it)) } },
    )
}

/**
 * A config too large for one QR code (a kubeconfig with an embedded client certificate)
 * fits once compressed: the scanner reads raw gzip in a binary code (see QrPayload.kt and
 * go/ichorgo/qr_payload.go), and the importer expands an "ichor-config:" text payload too
 * (go/ichorgo/import_text.go).
 */
private const val COMPRESSED_QR_COMMAND = "gzip -9c <file> | qrencode -8 -t ansiutf8"

/**
 * Larger still (several clusters, long certificates): scripts/qr-parts.py splits the gzip
 * into `ichor:<i>/<n>:<chunk>` codes that the scanner collects in any order (QrPartCollector).
 */
private const val QR_PARTS_COMMAND = "just qr-parts <file>"

@Composable
private fun BlockTitle(@StringRes title: Int, top: Boolean = true) {
    Text(
        stringResource(title),
        style = MaterialTheme.typography.titleSmall,
        modifier = Modifier.semantics { heading() }.then(if (top) Modifier.padding(top = 8.dp) else Modifier),
    )
}

/** Cloud accounts: one line per provider chip, then Sidero Omni's omniconfig. */
@Composable
private fun CloudHelp() {
    BlockTitle(R.string.help_cloud_title, top = false)
    Text(stringResource(R.string.help_cloud_body), style = MaterialTheme.typography.bodyMedium)
    CLOUD_LINES.forEach { Text(stringResource(it), style = MaterialTheme.typography.bodyMedium) }
    Step(stringResource(R.string.help_omni_title))
    Text(stringResource(R.string.help_omni_body), style = MaterialTheme.typography.bodyMedium)
    Command("qrencode -t ansiutf8 -r ~/.config/omni/config")
}

/** Clusters added from a kubeconfig, and how to make one the app can read. */
@Composable
private fun KubeconfigHelp() {
    var serviceAccount by rememberSaveable { mutableStateOf(false) }
    if (serviceAccount) ServiceAccountHelpDialog(onDismiss = { serviceAccount = false })
    BlockTitle(R.string.help_kube_title)
    Text(stringResource(R.string.help_kube_body), style = MaterialTheme.typography.bodyMedium)
    MutedText(stringResource(R.string.help_kube_signin))
    Step(stringResource(R.string.help_kube_flatten))
    Command("kubectl config view --flatten --minify > kubeconfig-phone")
    IchorTextButton(onClick = { serviceAccount = true }) { Text(stringResource(R.string.sa_help_title)) }
}

/** A dedicated Talos identity for the phone, with the role it gets. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun TalosHelp() {
    var role by rememberSaveable { mutableStateOf(PhoneRole.READER) }
    BlockTitle(R.string.help_talos_title)
    Text(stringResource(R.string.help_intro), style = MaterialTheme.typography.bodyMedium)
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
}

/** Getting a kubeconfig or a talosconfig onto the phone: a file, or one or more QR codes. */
@Composable
private fun BringToPhoneHelp() {
    BlockTitle(R.string.help_bring_title)
    Step(stringResource(R.string.help_bring_file))
    Command("adb push <file> /sdcard/Download/")
    Step(stringResource(R.string.help_step_qr))
    Command("qrencode -t ansiutf8 -r <file>")
    Step(stringResource(R.string.help_qr_compressed))
    Command(COMPRESSED_QR_COMMAND)
    Step(stringResource(R.string.help_qr_parts))
    Command(QR_PARTS_COMMAND)
    MutedText(stringResource(R.string.help_cleanup))
}

@Composable
internal fun Step(text: String) {
    Text(text, style = MaterialTheme.typography.labelLarge)
}

/** A monospace block of [command] (a command, or YAML) with a button that copies it. */
@Composable
internal fun Command(command: String, @StringRes copyLabel: Int = R.string.help_copy_command) {
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
            IchorIconButton(onClick = { scope.launch { clipboard.setClipEntry(ClipEntry(ClipData.newPlainText(command, command))) } }) {
                Icon(Icons.Outlined.ContentCopy, contentDescription = stringResource(copyLabel))
            }
        }
    }
}
