package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
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
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.uiText
import name.levis.talosmobile.Talosmobile

/** Longest address one can type: a host name, a port and a short path. */
private const val KUBE_SERVER_MAX = 300

/**
 * The Kubernetes API address to use for the cluster instead of the one in the kubeconfig
 * Talos issues, [saved] so far. Checked by the Go core; [onSave] gets it as an https URL,
 * or "" (left empty) to use the kubeconfig's again.
 */
@Composable
internal fun KubeServerDialog(saved: String, onSave: (String) -> Unit, onDismiss: () -> Unit) {
    var input by remember { mutableStateOf(saved) }
    var error by remember { mutableStateOf<UiText?>(null) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.kube_server_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(
                    value = input,
                    onValueChange = {
                        input = it.take(KUBE_SERVER_MAX)
                        error = null
                    },
                    label = { Text(stringResource(R.string.kube_server_label)) },
                    placeholder = { Text("k8s.example.com:6443") },
                    isError = error != null,
                    supportingText = error?.let { e -> { Text(e.asString()) } },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                Text(stringResource(R.string.kube_server_hint), style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = {
            TextButton(onClick = {
                runCatching { Talosmobile.normalizeKubeServer(input) }
                    .onSuccess(onSave)
                    .onFailure { error = it.uiText() }
            }) { Text(stringResource(R.string.common_ok)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
