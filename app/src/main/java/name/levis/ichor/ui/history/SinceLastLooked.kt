package name.levis.ichor.ui.history

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.HISTORY_NOT_READY
import name.levis.ichor.model.HistoryAlertSpan
import name.levis.ichor.model.HistorySince
import name.levis.ichor.model.hasNews
import name.levis.ichor.model.historyNodeName
import name.levis.ichor.model.newAlerts
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.localizedDuration

/** How long the cluster's home must stay on screen for the user to have looked at it. */
private const val LOOKED_AFTER_MILLIS = 5_000L

/** At most this many lines; the rest is counted. */
private const val SINCE_MAX_LINES = 6

/**
 * "Since you last looked" on a cluster's home: the nodes that went down and came back or are
 * still down, the alerts resolved and still open, the upgrades, since the user last looked at
 * this cluster. Nothing when nothing happened. Looking for a few seconds, or dismissing it,
 * moves "last looked" to now (the card stays until dismissed or left).
 */
@Composable
fun SinceLastLookedCard(fingerprint: String) {
    val app = LocalContext.current.applicationContext as TalosApp
    var since by remember(fingerprint) { mutableStateOf<HistorySince?>(null) }
    LaunchedEffect(fingerprint) {
        val start = app.lastLooked.start(fingerprint, System.currentTimeMillis())
        since = app.historyRepository.since(fingerprint, start)?.takeIf { it.hasNews }
        delay(LOOKED_AFTER_MILLIS)
        app.lastLooked.mark(fingerprint, System.currentTimeMillis())
    }
    val shown = since ?: return
    val lines = sinceLines(shown)
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(stringResource(R.string.history_since_title), style = MaterialTheme.typography.titleSmall)
            lines.take(SINCE_MAX_LINES).forEach { Text(it, style = MaterialTheme.typography.bodyMedium) }
            if (lines.size > SINCE_MAX_LINES) MutedText(stringResource(R.string.history_since_more, lines.size - SINCE_MAX_LINES))
            Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                TextButton(onClick = {
                    app.lastLooked.mark(fingerprint, System.currentTimeMillis())
                    since = null
                }) { Text(stringResource(R.string.history_since_dismiss)) }
            }
        }
    }
}

/** One line per event, most pressing first: still down, new alerts, recovered, resolved, upgrades. */
@Composable
private fun sinceLines(since: HistorySince): List<String> = buildList {
    since.nodesDown.forEach { add(stringResource(R.string.history_since_down, historyNodeName(it.node, it.hostname), stateLabel(it.state))) }
    since.newAlerts.forEach { add(stringResource(R.string.history_since_alert_open, alertLabel(it))) }
    since.nodesRecovered.forEach {
        val seconds = ((it.upAt ?: it.downAt) - it.downAt) / 1000
        add(stringResource(R.string.history_since_recovered, historyNodeName(it.node, it.hostname), stateLabel(it.state), localizedDuration(seconds)))
    }
    since.alertsResolved.forEach { add(stringResource(R.string.history_since_alert_resolved, alertLabel(it))) }
    since.upgrades.forEach { add(stringResource(R.string.history_since_upgrade, historyNodeName(it.node, it.hostname), it.from, it.to)) }
    val olderOpen = since.alertsOpen.size - since.newAlerts.size
    if (olderOpen > 0) add(pluralStringResource(R.plurals.history_since_still_open, olderOpen, olderOpen))
}

@Composable
private fun stateLabel(state: String): String =
    stringResource(if (state == HISTORY_NOT_READY) R.string.common_status_not_ready else R.string.common_status_unreachable)

/** The alert's short title (an alertname, a volume), the certificate's own text, else its key. */
@Composable
private fun alertLabel(alert: HistoryAlertSpan): String = when {
    alert.track == "cert" -> stringResource(R.string.history_alert_cert)
    alert.title.isNotBlank() -> alert.title
    else -> alert.key.substringAfter(':')
}
