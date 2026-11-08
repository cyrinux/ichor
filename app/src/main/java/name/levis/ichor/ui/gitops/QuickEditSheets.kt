package name.levis.ichor.ui.gitops

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Remove
import name.levis.ichor.ui.design.IchorAssistChip
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import name.levis.ichor.ui.design.IchorFilterChip
import androidx.compose.material3.Icon
import name.levis.ichor.ui.design.IchorIconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SheetValue
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.MAX_SCALE_REPLICAS
import name.levis.ichor.model.QuickEdit
import name.levis.ichor.model.QuickEditTargets
import name.levis.ichor.model.imageTag
import name.levis.ichor.model.isValidImageTag
import name.levis.ichor.model.quickImageChoices
import name.levis.ichor.model.quickPrefillIndex
import name.levis.ichor.model.quickPrefillTag
import name.levis.ichor.model.quickReplicaChoices
import name.levis.ichor.model.quickTagChips
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText

/**
 * Change image tag: the container (or kustomization entry) to change, its tag typed or picked
 * from the chips (the tag the workload ran before, the one it runs now), then Review change.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun QuickEditImageSheet(sheet: QuickSheet, targets: QuickEditTargets, onApply: (QuickEdit) -> Unit, onDismiss: () -> Unit) {
    val choices = remember(targets) { quickImageChoices(targets) }
    if (choices.isEmpty()) return
    // An update opens on its container (or kustomization entry) with the version picked.
    val prefill = sheet.prefill
    val prefilled = remember(choices, prefill) { prefill?.let { quickPrefillIndex(choices, it) } ?: -1 }
    var selected by rememberSaveable { mutableIntStateOf(prefilled.coerceAtLeast(0)) }
    val choice = choices[selected.coerceIn(0, choices.lastIndex)]
    var tag by rememberSaveable(choice.label) {
        mutableStateOf(if (selected == prefilled && prefill != null) quickPrefillTag(prefill) else imageTag(choice.image))
    }
    val revisions = sheet.suggestions.revisions[choice.documentIndex].takeUnless { choice.isKustomize }.orEmpty()
    val chips = remember(choice, sheet.suggestions) { quickTagChips(choice.image, sheet.suggestions.liveImages, revisions) }
    val valid = isValidImageTag(tag)
    QuickSheetFrame(stringResource(R.string.gitops_quick_image), sheet, enabled = valid, onApply = { onApply(choice.edit(tag)) }, onDismiss = onDismiss) {
        Text(stringResource(R.string.gitops_quick_container), style = MaterialTheme.typography.labelLarge)
        if (choices.size > 1) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                choices.forEachIndexed { i, c ->
                    IchorFilterChip(
                        selected = i == selected,
                        onClick = { if (!sheet.applying) selected = i },
                        label = { Text(c.label, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    )
                }
            }
        } else {
            Text(choice.label, style = MaterialTheme.typography.bodyLarge)
        }
        Text(choice.image, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        if (choice.isKustomize) MutedText(stringResource(R.string.gitops_quick_kustomize_note))
        OutlinedTextField(
            value = tag,
            onValueChange = { tag = it.trim() },
            label = { Text(stringResource(R.string.gitops_quick_tag)) },
            singleLine = true,
            enabled = !sheet.applying,
            isError = tag.isNotEmpty() && !valid,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
            modifier = Modifier.fillMaxWidth(),
        )
        if (chips.previous != null || chips.running != null) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                chips.previous?.let { previous -> TagChip(stringResource(R.string.gitops_quick_previous), previous, !sheet.applying) { tag = previous } }
                chips.running?.let { running -> TagChip(stringResource(R.string.gitops_quick_running), running, !sheet.applying) { tag = running } }
            }
        }
        MutedText(stringResource(R.string.gitops_quick_automation_hint))
    }
}

/** Set replicas: the workload (when the file has several), a stepper from the file's count, then Review change. */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun QuickEditReplicasSheet(sheet: QuickSheet, targets: QuickEditTargets, onApply: (QuickEdit) -> Unit, onDismiss: () -> Unit) {
    val choices = remember(targets) { quickReplicaChoices(targets) }
    if (choices.isEmpty()) return
    var selected by rememberSaveable { mutableIntStateOf(0) }
    val choice = choices[selected.coerceIn(0, choices.lastIndex)]
    val current = choice.current
    var target by rememberSaveable(choice.label) { mutableIntStateOf(current) }
    QuickSheetFrame(
        stringResource(R.string.gitops_quick_replicas),
        sheet,
        enabled = target != current,
        onApply = { onApply(choice.edit(target)) },
        onDismiss = onDismiss,
    ) {
        if (choices.size > 1) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                choices.forEachIndexed { i, c ->
                    IchorFilterChip(
                        selected = i == selected,
                        onClick = { if (!sheet.applying) selected = i },
                        label = { Text(c.label, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    )
                }
            }
        } else {
            MutedText(choice.label)
        }
        Text(stringResource(R.string.gitops_quick_replica_count), style = MaterialTheme.typography.labelLarge)
        Row(verticalAlignment = Alignment.CenterVertically) {
            IchorIconButton(onClick = { target -= 1 }, enabled = target > 0 && !sheet.applying) {
                Icon(Icons.Outlined.Remove, stringResource(R.string.workloads_scale_less))
            }
            Text(
                stringResource(R.string.workloads_scale_change, current, target),
                style = MaterialTheme.typography.titleMedium,
                fontFamily = FontFamily.Monospace,
            )
            IchorIconButton(onClick = { target += 1 }, enabled = target < MAX_SCALE_REPLICAS && !sheet.applying) {
                Icon(Icons.Outlined.Add, stringResource(R.string.workloads_scale_more))
            }
        }
    }
}

/** A suggested tag: what it is ([label]) and the [tag] it sets. */
@Composable
private fun TagChip(label: String, tag: String, enabled: Boolean, onClick: () -> Unit) {
    IchorAssistChip(
        onClick = onClick,
        enabled = enabled,
        label = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(label)
                Text(tag, Modifier.padding(start = 6.dp), fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        },
    )
}

/** The frame of a quick edit sheet: [title], [content], the error, Cancel and Review change. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun QuickSheetFrame(
    title: String,
    sheet: QuickSheet,
    enabled: Boolean,
    onApply: () -> Unit,
    onDismiss: () -> Unit,
    content: @Composable ColumnScope.() -> Unit,
) {
    val busy = sheet.applying
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true, confirmValueChange = { it != SheetValue.Hidden || !busy })
    ModalBottomSheet(onDismissRequest = { if (!busy) onDismiss() }, sheetState = sheetState) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(title, style = MaterialTheme.typography.titleLarge)
            content()
            sheet.error?.let { InlineError(it.asString(), Modifier.semantics { liveRegion = LiveRegionMode.Polite }) }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End), verticalAlignment = Alignment.CenterVertically) {
                if (busy) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                IchorTextButton(onClick = onDismiss, enabled = !busy) { Text(stringResource(R.string.common_cancel)) }
                IchorButton(onClick = onApply, enabled = enabled && !busy) { Text(stringResource(R.string.gitops_quick_review)) }
            }
        }
    }
}
