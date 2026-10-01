package name.levis.talosmobile.ui.components

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import name.levis.talosmobile.model.Feature

/** Explains why a feature is unavailable with the imported talosconfig's roles. */
@Composable
fun RoleNotice(feature: Feature, roles: List<String>, modifier: Modifier = Modifier) {
    Card(modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text("${feature.label} needs ${feature.minimumRole}", style = MaterialTheme.typography.titleSmall)
            Text(
                "This talosconfig has ${roles.joinToString().ifEmpty { "no roles" }}. Import a talosconfig " +
                    "created with --roles ${feature.minimumRole} to use it (see README).",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
