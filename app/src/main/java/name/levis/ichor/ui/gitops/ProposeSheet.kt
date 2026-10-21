package name.levis.ichor.ui.gitops

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SheetValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.GitProposalResult
import name.levis.ichor.model.gitOpsToolLabel
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.kubebrowser.ErrorCard
import name.levis.ichor.ui.settings.openUrl
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The pull request of an edit: title, commit message and description (prefilled), the new
 * branch (folded), the target branch (asked only when the app follows a tag or a commit:
 * [tracked]), draft; then "Open pull request". A stale file offers [onReload].
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProposeSheet(
    form: ProposeForm,
    tracked: String,
    reloading: Boolean,
    onChange: ((ProposeForm) -> ProposeForm) -> Unit,
    onPropose: () -> Unit,
    onReload: () -> Unit,
    onDismiss: () -> Unit,
) {
    var branchOpen by rememberSaveable { mutableStateOf(false) }
    val busy = form.sending || reloading
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true, confirmValueChange = { it != SheetValue.Hidden || !busy })
    ModalBottomSheet(onDismissRequest = { if (!busy) onDismiss() }, sheetState = sheetState) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(stringResource(R.string.gitops_propose_title), style = MaterialTheme.typography.titleLarge)
            FormField(form.title, { v -> onChange { it.copy(title = v) } }, stringResource(R.string.gitops_propose_field_title), enabled = !busy)
            FormField(form.message, { v -> onChange { it.copy(message = v) } }, stringResource(R.string.gitops_propose_field_message), enabled = !busy, singleLine = false)
            FormField(form.body, { v -> onChange { it.copy(body = v) } }, stringResource(R.string.gitops_propose_field_body), enabled = !busy, singleLine = false)
            if (!form.baseFixed) MutedText(stringResource(R.string.gitops_propose_base_not_branch, tracked))
            // Read only when the app follows a branch: the request goes there.
            FormField(
                form.base,
                { v -> onChange { it.copy(base = v) } },
                stringResource(R.string.gitops_propose_field_base),
                enabled = !busy,
                branch = true,
                readOnly = form.baseFixed,
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                TextButton(onClick = { branchOpen = !branchOpen }) {
                    Icon(if (branchOpen) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.gitops_propose_field_branch), Modifier.padding(start = 6.dp))
                }
                if (!branchOpen) MutedText(form.branch, Modifier.weight(1f), maxLines = 1)
            }
            if (branchOpen) {
                FormField(form.branch, { v -> onChange { it.copy(branch = v) } }, stringResource(R.string.gitops_propose_field_branch), enabled = !busy, branch = true)
            }
            ToggleRow(stringResource(R.string.gitops_propose_draft), form.draft, { v -> if (!busy) onChange { it.copy(draft = v) } })
            form.error?.let { error ->
                Column(Modifier.semantics { liveRegion = LiveRegionMode.Polite }, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    ErrorCard(error.asString())
                    if (form.stale) OutlinedButton(onClick = onReload, enabled = !reloading) { Text(stringResource(R.string.gitops_edit_reload)) }
                }
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End), verticalAlignment = Alignment.CenterVertically) {
                if (busy) {
                    CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                    if (form.sending) MutedText(stringResource(R.string.gitops_propose_opening))
                }
                TextButton(onClick = onDismiss, enabled = !busy) { Text(stringResource(R.string.common_cancel)) }
                Button(
                    onClick = onPropose,
                    enabled = !busy && form.title.isNotBlank() && form.message.isNotBlank() && form.base.isNotBlank(),
                ) { Text(stringResource(R.string.gitops_propose_open)) }
            }
        }
    }
}

@Composable
private fun FormField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    enabled: Boolean,
    singleLine: Boolean = true,
    branch: Boolean = false,
    readOnly: Boolean = false,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        label = { Text(label) },
        singleLine = singleLine,
        minLines = if (singleLine) 1 else 2,
        enabled = enabled,
        readOnly = readOnly,
        textStyle = if (branch) MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace) else MaterialTheme.typography.bodyLarge,
        keyboardOptions = if (branch) KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false) else KeyboardOptions.Default,
        modifier = Modifier.fillMaxWidth(),
    )
}

/**
 * The pull request opened: its link (Open, Copy link) and when it takes effect: [tool]'s
 * controller applies it once merged ("" for the cluster repository, which no controller is named for).
 */
@Composable
fun ProposeResult(result: GitProposalResult, tool: String, onDone: () -> Unit, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val copied = stringResource(R.string.gitops_source_copied)
    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(24.dp).semantics { liveRegion = LiveRegionMode.Polite },
        verticalArrangement = Arrangement.spacedBy(12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = LocalStatusColors.current.ok, modifier = Modifier.size(40.dp))
        Text(stringResource(R.string.gitops_propose_done), style = MaterialTheme.typography.titleLarge)
        if (result.url.isNotEmpty()) Text(result.url, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
        MutedText(if (tool.isEmpty()) stringResource(R.string.repo_propose_applies) else stringResource(R.string.gitops_propose_applies, gitOpsToolLabel(tool)))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (result.url.isNotEmpty()) {
                Button(onClick = { openUrl(context, result.url) }) {
                    Icon(Icons.AutoMirrored.Outlined.OpenInNew, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.gitops_propose_open_link), Modifier.padding(start = 8.dp))
                }
                OutlinedButton(onClick = {
                    copyToClipboard(context, result.branch, result.url)
                    Toast.makeText(context, copied, Toast.LENGTH_SHORT).show()
                }) {
                    Icon(Icons.Outlined.ContentCopy, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.gitops_propose_copy_link), Modifier.padding(start = 8.dp))
                }
            }
        }
        TextButton(onClick = onDone) { Text(stringResource(R.string.common_close)) }
    }
}
