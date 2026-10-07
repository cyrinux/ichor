package name.levis.ichor.ui.importconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.kubeauth.OmniKeyField

/**
 * An Omni context in the import preview: the instance and cluster in place of roles and a
 * certificate, and its service account key to enter now ([key]) or a sign-in later.
 */
@Composable
internal fun OmniContextRows(ctx: ContextSummary, key: String, onKey: (String) -> Unit) {
    InfoRow(stringResource(R.string.import_form_omni_url), ctx.endpoints.firstOrNull().orEmpty(), mono = true)
    InfoRow(stringResource(R.string.import_form_omni_cluster), ctx.omniCluster, mono = true)
    if (ctx.identity.isNotEmpty()) InfoRow(stringResource(R.string.import_form_omni_identity), ctx.identity)
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        MutedText(stringResource(R.string.omni_nodes_discovered))
        OmniKeyField(key, onKey, optional = true)
        MutedText(stringResource(R.string.import_form_omni_key_hint))
    }
}
