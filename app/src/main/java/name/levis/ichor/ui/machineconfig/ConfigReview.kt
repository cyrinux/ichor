package name.levis.ichor.ui.machineconfig

import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Restore
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.model.CONFIG_TRY_DEFAULT_TIMEOUT
import name.levis.ichor.model.CONFIG_TRY_TIMEOUTS
import name.levis.ichor.model.ConfigApplyMode
import name.levis.ichor.model.ConfigApplyState
import name.levis.ichor.model.applyModes
import name.levis.ichor.model.ConfigDiffLine
import name.levis.ichor.model.ConfigPreview
import name.levis.ichor.model.ConfigTryState
import name.levis.ichor.model.countdownText
import name.levis.ichor.model.secondsLeft
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * What the draft would change on the node, and the choice to try it ([onTry] gets the timeout
 * in seconds) or to apply it for good ([onApply]). [onAlsoApply], when the draft is only field
 * edits, takes the same change to other nodes.
 */
@Composable
fun ConfigReviewContent(
    review: UiState<ConfigPreview>,
    onRetry: () -> Unit,
    onTry: (Int) -> Unit,
    onApply: (ConfigApplyMode) -> Unit,
    modifier: Modifier = Modifier,
    onAlsoApply: (() -> Unit)? = null,
) {
    when (review) {
        UiState.Loading -> Waiting(stringResource(R.string.machine_config_review_loading), modifier)
        is UiState.Failed -> ErrorBox(review.message, onRetry, modifier)
        is UiState.Loaded -> {
            val preview = review.data
            if (!preview.changed) {
                InfoBox(stringResource(R.string.machine_config_no_changes), modifier)
                return
            }
            Column(modifier.fillMaxSize()) {
                DiffView(preview.lines, Modifier.weight(1f))
                Column(
                    Modifier.verticalScroll(rememberScrollState()).padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    if (preview.needsReboot) {
                        Notice(stringResource(R.string.machine_config_needs_reboot), LocalStatusColors.current.warn)
                    } else {
                        TryChoice(onTry)
                    }
                    ApplyChoice(preview.applyModes, onApply)
                    onAlsoApply?.let {
                        Text(stringResource(R.string.machine_config_multi_title), style = MaterialTheme.typography.labelLarge)
                        OutlinedButton(onClick = it, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.machine_config_multi_also)) }
                        MutedText(stringResource(R.string.machine_config_multi_also_desc))
                    }
                }
            }
        }
    }
}

@Composable
private fun TryChoice(onTry: (Int) -> Unit) {
    var timeout by rememberSaveable { mutableIntStateOf(CONFIG_TRY_DEFAULT_TIMEOUT) }
    MutedText(stringResource(R.string.machine_config_try_explain))
    Text(stringResource(R.string.machine_config_try_timeout), style = MaterialTheme.typography.labelLarge)
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        CONFIG_TRY_TIMEOUTS.forEachIndexed { i, seconds ->
            SegmentedButton(
                selected = seconds == timeout,
                onClick = { timeout = seconds },
                shape = SegmentedButtonDefaults.itemShape(i, CONFIG_TRY_TIMEOUTS.size),
                icon = {},
            ) { Text(minutesText(seconds)) }
        }
    }
    Button(onClick = { onTry(timeout) }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.machine_config_try)) }
}

/** The ways to apply the change for good, each with what it does. */
@Composable
internal fun ApplyChoice(modes: List<ConfigApplyMode>, onApply: (ConfigApplyMode) -> Unit) {
    Text(stringResource(R.string.machine_config_apply_other), style = MaterialTheme.typography.labelLarge)
    modes.forEach { mode ->
        OutlinedButton(onClick = { onApply(mode) }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(mode.label)) }
        MutedText(stringResource(mode.description))
    }
}

@get:StringRes
val ConfigApplyMode.label: Int
    get() = when (this) {
        ConfigApplyMode.AUTO -> R.string.machine_config_apply_auto
        ConfigApplyMode.STAGED -> R.string.machine_config_apply_staged
        ConfigApplyMode.REBOOT -> R.string.machine_config_apply_reboot
    }

@get:StringRes
private val ConfigApplyMode.description: Int
    get() = when (this) {
        ConfigApplyMode.AUTO -> R.string.machine_config_apply_auto_desc
        ConfigApplyMode.STAGED -> R.string.machine_config_apply_staged_desc
        ConfigApplyMode.REBOOT -> R.string.machine_config_apply_reboot_desc
    }

/** A change being applied for good: its phase, then how it ended. */
@Composable
fun ConfigApplyContent(run: ConfigApplyState, onDone: () -> Unit, modifier: Modifier = Modifier) {
    val status = LocalStatusColors.current
    when (run) {
        is ConfigApplyState.Running -> Waiting(
            stringResource(
                when (run.phase) {
                    ConfigApplyState.REBOOTING -> R.string.machine_config_apply_rebooting
                    ConfigApplyState.WAITING -> R.string.machine_config_apply_waiting
                    else -> R.string.machine_config_apply_applying
                },
            ) + run.message.takeIf { it.isNotEmpty() }?.let { "\n$it" }.orEmpty(),
            modifier,
        )
        is ConfigApplyState.Done -> Ended(
            Icons.Outlined.CheckCircle,
            status.ok,
            stringResource(
                when (run.mode) {
                    ConfigApplyMode.AUTO -> R.string.machine_config_applied
                    ConfigApplyMode.STAGED -> R.string.machine_config_staged
                    ConfigApplyMode.REBOOT -> R.string.machine_config_rebooted
                },
            ),
            onDone,
            modifier,
        )
        is ConfigApplyState.Failed -> Ended(
            Icons.Outlined.ErrorOutline,
            status.bad,
            run.message.ifEmpty { stringResource(R.string.machine_config_try_no_answer) },
            onDone,
            modifier,
            done = R.string.machine_config_back_to_draft,
        )
    }
}

@Composable
fun minutesText(seconds: Int): String = stringResource(R.string.machine_config_minutes, seconds / 60)

@Composable
internal fun DiffView(lines: List<ConfigDiffLine>, modifier: Modifier = Modifier) {
    val status = LocalStatusColors.current
    SelectionContainer(modifier) {
        Box(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).horizontalScroll(rememberScrollState())) {
            // As wide as the longest line, so every coloured line reaches the same edge.
            Column(Modifier.width(IntrinsicSize.Max).padding(vertical = 8.dp)) {
                lines.forEach { line ->
                    val (prefix, background, color) = when (line.kind) {
                        ConfigDiffLine.ADDED -> Triple("+ ", status.ok.copy(alpha = 0.18f), Color.Unspecified)
                        ConfigDiffLine.REMOVED -> Triple("- ", status.bad.copy(alpha = 0.18f), Color.Unspecified)
                        ConfigDiffLine.HUNK -> Triple("", Color.Transparent, MaterialTheme.colorScheme.onSurfaceVariant)
                        else -> Triple("  ", Color.Transparent, Color.Unspecified)
                    }
                    Text(
                        prefix + line.text,
                        color = color,
                        style = MaterialTheme.typography.bodySmall,
                        fontFamily = FontFamily.Monospace,
                        softWrap = false,
                        modifier = Modifier.fillMaxWidth().background(background).padding(horizontal = 16.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun Notice(text: String, color: Color) {
    Card(colors = CardDefaults.cardColors(containerColor = color.copy(alpha = 0.15f)), modifier = Modifier.fillMaxWidth()) {
        Text(text, color = color, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(12.dp))
    }
}

@Composable
internal fun Waiting(text: String, modifier: Modifier = Modifier) {
    Column(
        modifier.fillMaxSize().padding(32.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        CircularProgressIndicator()
        Text(text, textAlign = TextAlign.Center)
    }
}

/** A change being tried on the node: the countdown to its automatic revert, then how it ended. */
@Composable
fun ConfigTryContent(run: ConfigTryState, onKeep: () -> Unit, onRevert: () -> Unit, onDone: () -> Unit, modifier: Modifier = Modifier) {
    val status = LocalStatusColors.current
    when (run) {
        is ConfigTryState.Running -> when (run.phase) {
            ConfigTryState.TRYING -> Trying(run, onKeep, onRevert, modifier)
            ConfigTryState.KEEPING -> Waiting(stringResource(R.string.machine_config_try_keeping), modifier)
            ConfigTryState.REVERTING -> Waiting(stringResource(R.string.machine_config_try_reverting), modifier)
            else -> Waiting(stringResource(R.string.machine_config_try_applying), modifier)
        }
        ConfigTryState.Kept -> Ended(Icons.Outlined.CheckCircle, status.ok, stringResource(R.string.machine_config_kept), onDone, modifier)
        ConfigTryState.Reverted -> Ended(Icons.Outlined.Restore, status.muted, stringResource(R.string.machine_config_reverted), onDone, modifier)
        is ConfigTryState.Failed -> Ended(
            Icons.Outlined.ErrorOutline,
            status.bad,
            run.message.ifEmpty { stringResource(R.string.machine_config_try_no_answer) },
            onDone,
            modifier,
            done = R.string.machine_config_back_to_draft,
        )
    }
}

@Composable
private fun Trying(run: ConfigTryState.Running, onKeep: () -> Unit, onRevert: () -> Unit, modifier: Modifier = Modifier) {
    val now by produceState(System.currentTimeMillis(), run.deadline) {
        while (true) {
            value = System.currentTimeMillis()
            delay(250)
        }
    }
    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(stringResource(R.string.machine_config_try_countdown), style = MaterialTheme.typography.titleMedium)
        Text(countdownText(secondsLeft(run.deadline, now)), style = MaterialTheme.typography.displayLarge, fontFamily = FontFamily.Monospace)
        if (run.message.isNotEmpty()) Notice(run.message, LocalStatusColors.current.bad)
        Button(onClick = onKeep, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.machine_config_keep)) }
        OutlinedButton(onClick = onRevert, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.machine_config_revert_now)) }
        MutedText(stringResource(R.string.machine_config_try_leave_note))
    }
}

@Composable
private fun Ended(
    icon: ImageVector,
    tint: Color,
    text: String,
    onDone: () -> Unit,
    modifier: Modifier = Modifier,
    @StringRes done: Int = R.string.machine_config_done,
) {
    Column(
        modifier.fillMaxSize().padding(32.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(56.dp))
        Text(text, textAlign = TextAlign.Center)
        Button(onClick = onDone) { Text(stringResource(done)) }
    }
}
