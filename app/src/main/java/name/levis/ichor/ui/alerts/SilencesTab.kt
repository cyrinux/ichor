package name.levis.ichor.ui.alerts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.AmSilence
import name.levis.ichor.model.AmSilenceState
import name.levis.ichor.model.AmSilences
import name.levis.ichor.model.KubePermission
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatDateTime
import name.levis.ichor.util.timeAgo

/**
 * The silences of the Alertmanager: active ones (ending soonest first), then pending ones and,
 * when asked, the latest expired ones. An active or pending silence can be expired, once
 * confirmed. [busy]: the IDs being expired; [denial]: why the credentials cannot expire.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SilencesTab(
    state: UiState<AmSilences>,
    withExpired: Boolean,
    onWithExpired: (Boolean) -> Unit,
    busy: Set<String>,
    denial: KubePermission?,
    onExpire: (AmSilence) -> Unit,
    onRefresh: () -> Unit,
) {
    var confirming by remember { mutableStateOf<AmSilence?>(null) }
    confirming?.let { silence ->
        ConfirmDialog(
            title = stringResource(R.string.alerts_expire_title),
            text = stringResource(R.string.alerts_expire_text, silence.matchers.joinToString(", ") { it.text }),
            confirm = stringResource(R.string.alerts_expire),
            destructive = true,
            onConfirm = { confirming = null; onExpire(silence) },
            onDismiss = { confirming = null },
        )
    }
    Column(Modifier.fillMaxSize()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) {
            ToggleRow(stringResource(R.string.alerts_silences_show_expired), withExpired, onWithExpired)
            KubeDenialNote(denial)
        }
        when (state) {
            UiState.Loading -> LoadingBox()
            is UiState.Failed -> ErrorBox(state.message, onRefresh)
            is UiState.Loaded -> PullToRefreshBox(isRefreshing = state.refreshing, onRefresh = onRefresh, modifier = Modifier.fillMaxSize()) {
                LazyColumn(
                    Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    if (state.data.silences.isEmpty()) item { MutedText(stringResource(R.string.alerts_silences_none), Modifier.padding(vertical = 16.dp)) }
                    items(state.data.silences, key = { it.id }) { silence ->
                        SilenceCard(silence, expiring = silence.id in busy, canExpire = denial == null, onExpire = { confirming = silence })
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun SilenceCard(silence: AmSilence, expiring: Boolean, canExpire: Boolean, onExpire: () -> Unit) {
    val colors = LocalStatusColors.current
    val color = when (silence.state) {
        AmSilenceState.ACTIVE -> colors.ok
        AmSilenceState.PENDING -> colors.warn
        else -> colors.muted
    }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                StatusPill(silenceStateLabel(silence.state), color)
                MutedText(silenceWhen(silence), Modifier.padding(start = 8.dp).weight(1f))
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                silence.matchers.forEach { m ->
                    Text(m.text, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall)
                }
            }
            if (silence.comment.isNotEmpty()) Text(silence.comment, style = MaterialTheme.typography.bodyMedium)
            if (silence.createdBy.isNotEmpty()) MutedText(stringResource(R.string.alerts_silence_by, silence.createdBy))
            if (silence.state != AmSilenceState.EXPIRED) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End, verticalAlignment = Alignment.CenterVertically) {
                    if (expiring) CircularProgressIndicator(Modifier.padding(end = 8.dp), strokeWidth = 2.dp)
                    TextButton(onClick = onExpire, enabled = !expiring && canExpire) {
                        Text(stringResource(R.string.alerts_expire), color = if (canExpire) colors.bad else colors.muted)
                    }
                }
            }
        }
    }
}

/** "Ends in 4 hours", "Starts in 2 hours" or "Ended yesterday", with the date. */
@Composable
private fun silenceWhen(silence: AmSilence): String = when (silence.state) {
    AmSilenceState.PENDING -> stringResource(R.string.alerts_silence_starts, timeAgo(silence.startsAt), formatDateTime(silence.startsAt))
    AmSilenceState.ACTIVE -> stringResource(R.string.alerts_silence_ends, timeAgo(silence.endsAt), formatDateTime(silence.endsAt))
    else -> stringResource(R.string.alerts_silence_ended, timeAgo(silence.endsAt), formatDateTime(silence.endsAt))
}
