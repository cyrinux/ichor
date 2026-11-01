package name.levis.ichor.ui.join

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ChevronRight
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.design.IchorCard

/** Where the new machine comes from: what the rest of the screen then shows. */
enum class AddNodePath(@StringRes val title: Int, @StringRes val body: Int) {
    CLOUD(R.string.add_node_cloud, R.string.add_node_cloud_body),
    MACHINE(R.string.add_node_machine, R.string.add_node_machine_body),
    ;

    companion object {
        /** The path a route names; null (ask) for none or an unknown one. */
        fun fromRoute(name: String?): AddNodePath? = entries.firstOrNull { it.name == name }
    }
}

/** A cloud provider, in alphabetical order; [platform] names its Talos image, null lists them all. */
enum class CloudProvider(val label: String?, val platform: String?) {
    HETZNER("Hetzner", "hcloud"),
    OVHCLOUD("OVHcloud", "openstack"),
    SCALEWAY("Scaleway", "scaleway"),
    OTHER(null, null),
}

/** "Where is the new machine?": one row per path. */
@Composable
fun AddNodeChooser(onPick: (AddNodePath) -> Unit) {
    SectionTitle(stringResource(R.string.add_node_where))
    AddNodePath.entries.forEach { path ->
        IchorCard(onClick = { onPick(path) }, modifier = Modifier.fillMaxWidth()) {
            Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(path.title), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
                    MutedText(stringResource(path.body))
                }
                Icon(Icons.Outlined.ChevronRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}
