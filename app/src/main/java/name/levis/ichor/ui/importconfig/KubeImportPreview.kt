package name.levis.ichor.ui.importconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeImportRow
import name.levis.ichor.model.kubeAuthLabel
import name.levis.ichor.model.kubeImportRows
import name.levis.ichor.model.kubeProblemText
import name.levis.ichor.model.parseCloudContext
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.cloudDetail
import name.levis.ichor.ui.kubeauth.signInMethodText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The contexts of an imported kubeconfig: a typical ~/.kube/config holds several, so each is
 * a row to check or not. Those the app cannot add yet say why and stay unchecked.
 */
@Composable
internal fun KubePreviewCard(
    preview: ImportState.KubePreview,
    adding: Boolean,
    onInclude: (Int, Boolean) -> Unit,
    onRename: (Int, String) -> Unit,
    onReplace: (Int, Boolean) -> Unit,
    onConfirm: () -> Unit,
    onCancel: () -> Unit,
) {
    val rows = remember(preview) { kubeImportRows(preview.summary, preview.conflicts, preview.choices) }
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.import_kube_title), style = MaterialTheme.typography.titleLarge)
        rows.forEach { row ->
            KubeContextRow(
                row = row,
                current = row.context.name == preview.summary.current,
                taken = row.index in preview.takenNames,
                onInclude = { onInclude(row.index, it) },
                onRename = { onRename(row.index, it) },
                onReplace = { onReplace(row.index, it) },
            )
        }
        if (rows.none { it.importable }) {
            Text(stringResource(R.string.import_kube_none_importable), color = LocalStatusColors.current.warn, style = MaterialTheme.typography.bodyMedium)
        }
        if (adding) Text(stringResource(R.string.import_adds_cluster), style = MaterialTheme.typography.bodyMedium)
        preview.error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium) }
        MutedText(stringResource(R.string.import_stored_encrypted))
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton(onClick = onCancel, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.common_cancel)) }
            Button(onClick = onConfirm, enabled = preview.canImport, modifier = Modifier.weight(1f)) {
                Text(stringResource(R.string.import_import))
            }
        }
        Box(Modifier.height(8.dp))
    }
}

@Composable
private fun KubeContextRow(
    row: KubeImportRow,
    current: Boolean,
    taken: Boolean,
    onInclude: (Boolean) -> Unit,
    onRename: (String) -> Unit,
    onReplace: (Boolean) -> Unit,
) {
    val ctx = row.context
    // EKS and GKE name contexts by ARN or gke_project_location_name: show the cluster's name.
    val cloud = parseCloudContext(ctx.name)
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(start = 4.dp, end = 16.dp, top = 4.dp, bottom = 16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = row.included, onCheckedChange = onInclude, enabled = row.importable)
                val shown = cloud?.cluster ?: ctx.name
                val name = if (current) stringResource(R.string.import_context_current, shown) else shown
                Text(name, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
            }
            Column(Modifier.padding(start = 12.dp)) {
                if (cloud != null) MutedText(cloudDetail(cloud))
                InfoRow(stringResource(R.string.import_kube_server), ctx.endpoints.joinToString("\n"), mono = true)
                InfoRow(stringResource(R.string.import_kube_auth), authText(ctx.auth, ctx.authDetail))
                if (ctx.user.isNotEmpty()) InfoRow(stringResource(R.string.import_kube_user), ctx.user)
                if (ctx.namespace.isNotEmpty()) InfoRow(stringResource(R.string.import_kube_namespace), ctx.namespace)
                if (ctx.certNotAfter > 0) InfoRow(stringResource(R.string.import_kube_expires), certExpiry(ctx.certNotAfter))
                if (ctx.insecure) MutedText(stringResource(R.string.import_kube_insecure))
                // Added without credentials: the cluster's home asks to sign in.
                if (row.importable && ctx.signIn.isNotEmpty()) {
                    MutedText(stringResource(R.string.import_kube_signin_follows, signInMethodText(ctx.signIn)))
                }
                if (!row.importable) Problem(ctx.problem, ctx.problemDetail)
                row.conflict?.let { conflict ->
                    NameConflict(
                        name = ctx.name,
                        conflict = conflict,
                        choice = row.choice,
                        taken = taken,
                        onRename = onRename,
                        onReplace = onReplace,
                    )
                }
            }
        }
    }
}

/** The sign-in method, and what it signs in to when the kubeconfig says. */
@Composable
private fun authText(auth: String, detail: String): String {
    val label = kubeAuthLabel(auth)?.let { stringResource(it) } ?: auth
    return if (detail.isEmpty()) label else "$label · $detail"
}

/** Why the context cannot be added, with the raw detail (a file path, a plugin) under it. */
@Composable
private fun Problem(problem: String, detail: String) {
    Column(Modifier.padding(top = 8.dp)) {
        Text(stringResource(kubeProblemText(problem)), color = LocalStatusColors.current.warn, style = MaterialTheme.typography.bodyMedium)
        if (detail.isNotEmpty()) {
            Text(
                detail,
                fontFamily = FontFamily.Monospace,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
