package name.levis.ichor.ui.argocd

import androidx.compose.foundation.layout.Column
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AcUnit
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.data.ARGO_CD
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoFreezeAction
import name.levis.ichor.model.ArgoProject
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.FREEZE_EXTEND_MINUTES
import name.levis.ichor.model.FreezeScope
import name.levis.ichor.model.freezeOptions
import name.levis.ichor.model.projectOf
import name.levis.ichor.model.selfHealingOwner

// A change made by hand (scale, suspend a CronJob) to a resource an Argo CD app self-heals is
// undone within minutes: ask first, offering to freeze the app for an hour. See
// the Linear plan document "D9. Argo CD freeze: hotfix live without being reverted" (the D2 hook).

/** An Argo CD app that self-heals a resource, with its project. */
data class ArgoSelfHealer(val app: ArgoApp, val project: ArgoProject)

/**
 * The Argo CD app that would undo a change to [kind] [namespace]/[name] (self-heal on, not
 * frozen), from the Argo CD status already loaded; null when none or not loaded.
 */
fun GitOpsRepository.argoSelfHealer(kind: String, namespace: String, name: String): ArgoSelfHealer? {
    val status = cached<ArgoStatus>(ARGO_CD)?.value ?: return null
    val app = status.selfHealingOwner(kind, namespace, name) ?: return null
    return status.projectOf(app)?.let { ArgoSelfHealer(app, it) }
}

/** Freezes the healer's app for an hour with [reason] (stored on the cluster: no masked name). */
suspend fun GitOpsRepository.freezeForHandChange(healer: ArgoSelfHealer, reason: String) {
    val options = freezeOptions(healer.app, FreezeScope.APP, FREEZE_EXTEND_MINUTES, manualSync = true, reason = reason)
    argoFreeze(healer.project.namespace, healer.project.name, ArgoFreezeAction.FREEZE, options)
}

/**
 * [title] and [text] (what the change does) plus why Argo CD would undo it, with "Freeze 1 h
 * first", the change anyway ([confirm]), and Cancel.
 */
@Composable
fun ArgoRevertDialog(
    healer: ArgoSelfHealer,
    title: String,
    text: String,
    confirm: String,
    onFreezeFirst: () -> Unit,
    onAnyway: () -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Icon(Icons.Outlined.AcUnit, contentDescription = null) },
        title = { Text(title) },
        text = { Text(listOf(text, stringResource(R.string.argo_revert_text_change, healer.app.name)).filter { it.isNotEmpty() }.joinToString("\n\n")) },
        confirmButton = {
            Column(horizontalAlignment = Alignment.End) {
                TextButton(onClick = onFreezeFirst) { Text(stringResource(R.string.argo_revert_freeze_first, healer.app.name)) }
                TextButton(onClick = onAnyway) { Text(confirm) }
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
            }
        },
    )
}
