package name.levis.ichor.ui.components

import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.isKube

/**
 * Explains why a feature is unavailable for [cluster]: the talosconfig's roles, or for a
 * cluster added from a kubeconfig, that it has no Talos API (no role would help).
 */
@Composable
fun RoleNotice(feature: Feature, cluster: ContextSummary, modifier: Modifier = Modifier) {
    if (cluster.isKube) TalosOnlyNotice(modifier) else RoleNotice(feature, cluster.roles, modifier)
}

/** A Talos screen reached on a cluster added from a kubeconfig (a link, a notification). */
@Composable
fun TalosOnlyNotice(modifier: Modifier = Modifier) {
    Card(modifier.fillMaxWidth()) {
        MutedText(stringResource(R.string.common_talos_only), modifier = Modifier.padding(16.dp))
    }
}

/** Explains why a feature is unavailable with the imported talosconfig's roles. */
@Composable
fun RoleNotice(feature: Feature, roles: List<String>, modifier: Modifier = Modifier) {
    Card(modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text(stringResource(R.string.common_role_notice_title, stringResource(feature.label), feature.minimumRole), style = MaterialTheme.typography.titleSmall)
            MutedText(
                stringResource(
                    R.string.common_role_notice_body,
                    roles.joinToString().ifEmpty { stringResource(R.string.common_role_notice_no_roles) },
                    feature.minimumRole,
                ),
            )
        }
    }
}
