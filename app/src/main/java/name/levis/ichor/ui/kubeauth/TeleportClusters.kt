package name.levis.ichor.ui.kubeauth

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.TeleportCluster
import name.levis.ichor.model.TeleportClusters
import name.levis.ichor.signInChanged
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage
import name.levis.ichor.util.formatDateTime

/**
 * The Kubernetes clusters of the Teleport proxy [context] signs in at (its stored contexts):
 * a tap opens one's sign-in ([onOpen]), one can be added by name ([onAdded] gets its new
 * context), and the proxy can be signed out of entirely ([onSignedOut]). Nothing for a cluster
 * that lists none (the demo).
 */
@Composable
internal fun TeleportClusters(context: String, onOpen: (String) -> Unit, onAdded: (String) -> Unit, onSignedOut: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    var reload by remember { mutableIntStateOf(0) }
    val list by produceState<TeleportClusters?>(null, context, reload) {
        value = runCatching { app.kubeAuthRepository.teleportClusters(context) }.getOrNull()
    }
    val clusters = list?.clusters.orEmpty()
    if (clusters.isEmpty()) return

    var confirm by remember { mutableStateOf(false) }

    Text(stringResource(R.string.kube_signin_teleport_clusters), style = MaterialTheme.typography.titleSmall)
    Column(Modifier.selectableGroup()) {
        clusters.forEach { cluster -> TeleportClusterRow(cluster, shown = cluster.context == context) { onOpen(cluster.context) } }
    }
    AddTeleportCluster(onAdd = { name -> app.kubeAuthRepository.teleportAddCluster(context, name) }, onAdded = onAdded)

    if (clusters.any { it.signedIn } || list?.user.orEmpty().isNotEmpty()) {
        IchorTextButton(onClick = { confirm = true }) {
            Text(stringResource(R.string.kube_signin_teleport_sign_out_all), color = LocalStatusColors.current.bad)
        }
    }
    if (confirm) {
        IchorAlertDialog(
            onDismissRequest = { confirm = false },
            title = { Text(stringResource(R.string.kube_signin_teleport_sign_out_all)) },
            text = { Text(stringResource(R.string.kube_signin_teleport_sign_out_all_body)) },
            confirmButton = {
                IchorTextButton(onClick = {
                    confirm = false
                    scope.launch {
                        runCatching { app.kubeAuthRepository.teleportSignOutAll(context) }
                        app.signInChanged()
                        reload++
                        onSignedOut()
                    }
                }) { Text(stringResource(R.string.kube_signin_sign_out), color = LocalStatusColors.current.bad) }
            },
            dismissButton = { IchorTextButton(onClick = { confirm = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

@Composable
private fun TeleportClusterRow(cluster: TeleportCluster, shown: Boolean, onClick: () -> Unit) {
    val status = when {
        cluster.signedIn && cluster.expires > 0 -> stringResource(R.string.kube_signin_teleport_cert_until, formatDateTime(cluster.expires * 1000))
        cluster.signedIn -> stringResource(R.string.kube_signin_signed_in)
        else -> stringResource(R.string.kube_signin_teleport_no_cert)
    }
    Row(
        Modifier.fillMaxWidth().selectable(selected = shown, role = Role.RadioButton, onClick = onClick).padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(selected = shown, onClick = null)
        Column(Modifier.padding(start = 8.dp)) {
            Text(cluster.name)
            MutedText(status)
        }
    }
}

/** "Add a cluster by name": the core refuses a name that is not a DNS label, in a sentence shown here. */
@Composable
private fun AddTeleportCluster(onAdd: suspend (String) -> String, onAdded: (String) -> Unit) {
    val scope = rememberCoroutineScope()
    var name by rememberSaveable { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }

    MutedText(stringResource(R.string.kube_signin_teleport_add_hint))
    OutlinedTextField(
        value = name,
        onValueChange = {
            name = it
            error = null
        },
        label = { Text(stringResource(R.string.kube_signin_teleport_add_label)) },
        singleLine = true,
        enabled = !busy,
        modifier = Modifier.fillMaxWidth(),
    )
    IchorOutlinedButton(
        onClick = {
            busy = true
            scope.launch {
                runCatching { onAdd(name.trim()) }
                    .onSuccess {
                        name = ""
                        onAdded(it)
                    }
                    .onFailure { error = it.userMessage() }
                busy = false
            }
        },
        enabled = name.isNotBlank() && !busy,
    ) { Text(stringResource(R.string.kube_signin_teleport_add)) }
    error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium) }
}
