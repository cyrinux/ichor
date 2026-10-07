package name.levis.ichor.ui.importconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DiscoveryProvider
import name.levis.ichor.model.credentialsComplete
import name.levis.ichor.model.credentialsFor
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.kubeauth.CredentialFields
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Adding clusters from a cloud account (K7): pick the provider, enter the account's
 * credentials, and its clusters show in the kubeconfig preview. Nothing is stored before
 * the user adds them there; the clusters then sign in with the same credentials.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun DiscoverCard(
    state: ImportState.Discover,
    onDiscover: (DiscoveryProvider, Map<String, String>) -> Unit,
    onCancel: () -> Unit,
) {
    val providers = state.fields.keys.toList()
    var provider by remember { mutableStateOf(providers.firstOrNull()) }
    // Not saveable: secrets never go into saved instance state.
    val values = remember { mutableStateMapOf<String, String>() }
    val fields = provider?.let { state.fields[it] }.orEmpty()
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.kube_discover_title), style = MaterialTheme.typography.titleLarge)
        MutedText(stringResource(R.string.kube_discover_body))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            providers.forEach { p ->
                FilterChip(
                    selected = provider == p,
                    onClick = { provider = p },
                    label = { Text(stringResource(p.label)) },
                    enabled = !state.running,
                )
            }
        }
        CredentialFields(fields, values, onValue = { name, value -> values[name] = value }, enabled = !state.running)
        MutedText(stringResource(R.string.kube_discover_least_privilege))
        state.error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium) }
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton(onClick = onCancel, enabled = !state.running, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.common_cancel)) }
            Button(
                onClick = { provider?.let { onDiscover(it, credentialsFor(fields, values)) } },
                enabled = provider != null && !state.running && credentialsComplete(fields, values),
                modifier = Modifier.weight(1f),
            ) {
                if (state.running) {
                    CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                } else {
                    Text(stringResource(R.string.kube_discover_find))
                }
            }
        }
        Box(Modifier.height(8.dp))
    }
}
