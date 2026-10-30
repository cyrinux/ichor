package name.levis.ichor.ui.gitops

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.EDIT_CHECK_PLAIN
import name.levis.ichor.model.EditCheckError
import name.levis.ichor.model.GitOpsEditCheck
import name.levis.ichor.model.SopsWarning
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.diff.DiffResourceCard
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The edit's build check under the review's text diff: building, then "Builds" with the
 * rendered objects that change (each unfolds its diff), "Build fails" with the errors and the
 * file and line they name, or why the file is not checked (a Helm chart). Opening the pull
 * request stays possible either way: the person decides.
 */
@Composable
fun EditCheckSection(check: UiState<GitOpsEditCheck>, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(
            stringResource(R.string.gitops_check_title),
            style = MaterialTheme.typography.titleSmall,
            modifier = Modifier.semantics { heading() },
        )
        when (check) {
            UiState.Loading -> Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                MutedText(stringResource(R.string.gitops_check_running))
            }
            is UiState.Failed -> Row(verticalAlignment = Alignment.CenterVertically) {
                MutedText(stringResource(R.string.gitops_check_failed, check.message.asString()), Modifier.weight(1f))
                IchorTextButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
            }
            is UiState.Loaded -> CheckResult(check.data)
        }
    }
}

@Composable
private fun CheckResult(c: GitOpsEditCheck) {
    val colors = LocalStatusColors.current
    val live = Modifier.semantics { liveRegion = LiveRegionMode.Polite }
    when {
        c.broken -> {
            Status(Icons.Outlined.Info, stringResource(R.string.gitops_check_already_broken), colors.warn, live)
            c.errors.forEach { ErrorRow(it) }
        }
        !c.ok -> {
            Status(Icons.Outlined.ErrorOutline, stringResource(R.string.gitops_check_fails), colors.bad, live)
            c.errors.forEach { ErrorRow(it) }
        }
        c.built -> {
            val label = if (c.kind == EDIT_CHECK_PLAIN) R.string.gitops_check_valid_yaml else R.string.gitops_check_builds
            Status(Icons.Outlined.CheckCircle, stringResource(label), colors.ok, live)
            if (c.kind != EDIT_CHECK_PLAIN) MutedText(stringResource(R.string.gitops_check_dir, c.dir.ifEmpty { "/" }))
            val changed = c.diff.resources
            MutedText(
                if (changed.isEmpty()) stringResource(R.string.gitops_check_no_objects) else stringResource(R.string.gitops_check_objects, changed.size, c.unchanged),
            )
            changed.forEach { r ->
                var open by rememberSaveable(r.key) { mutableStateOf(false) }
                DiffResourceCard(r, expanded = open, onToggle = { open = !open })
            }
        }
        else -> Status(Icons.Outlined.Info, stringResource(R.string.gitops_check_not_checked), MaterialTheme.colorScheme.onSurfaceVariant, live)
    }
    if (c.note.isNotEmpty()) MutedText(c.note)
}

/** The edit commits a clear-text secret where the repository's .sops.yaml encrypts. */
@Composable
fun SopsWarningText(w: SopsWarning, modifier: Modifier = Modifier) {
    val recipients = w.rule.recipients.joinToString { "${it.type} ${it.short}" }
    Text(
        stringResource(R.string.sops_clear_warning, sopsWhat(w), w.rule.pathRegex, recipients.ifEmpty { "-" }),
        color = LocalStatusColors.current.bad,
        style = MaterialTheme.typography.bodyMedium,
        modifier = modifier.semantics { liveRegion = LiveRegionMode.Polite },
    )
}

/** What a warning names, worded: a Kubernetes Secret, Talos secrets, or the Talos key as is. */
@Composable
fun sopsWhat(w: SopsWarning): String = when {
    w.isSecret -> stringResource(R.string.sops_what_secret)
    w.isTalosBundle -> stringResource(R.string.sops_what_talos)
    else -> w.what
}

@Composable
private fun Status(icon: ImageVector, text: String, color: Color, modifier: Modifier = Modifier) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(icon, contentDescription = null, tint = color)
        Text(text, color = color, style = MaterialTheme.typography.titleSmall)
    }
}

@Composable
private fun ErrorRow(e: EditCheckError) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        if (e.path.isNotEmpty()) {
            Text(
                if (e.line > 0) stringResource(R.string.gitops_check_at, e.path, e.line) else e.path,
                style = MaterialTheme.typography.labelMedium,
                fontFamily = FontFamily.Monospace,
            )
        }
        Text(e.message, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
    }
}
