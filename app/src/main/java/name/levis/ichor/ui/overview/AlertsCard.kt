package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.AlertmanagerRepository
import name.levis.ichor.data.AlertmanagerStore
import name.levis.ichor.data.sourceFor
import name.levis.ichor.model.AmAlerts
import name.levis.ichor.model.AmSeverity
import name.levis.ichor.model.PromSource
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.alerts.severityColor
import name.levis.ichor.ui.alerts.severityLabel
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors

/** The Alertmanager the card reads, and what it holds. */
data class AlertsOverview(val source: PromSource, val alerts: AmAlerts)

/**
 * The overview's Alerts card: the cluster's Alertmanager (chosen, else found), and its alerts.
 * Loaded(null) when there is none: the card stays hidden. Loaded once per key, never polled.
 */
class AlertsCardViewModel(
    private val alertmanager: AlertmanagerRepository,
    private val store: AlertmanagerStore,
    private val fingerprint: () -> String?,
) : LoadingViewModel<AlertsOverview?>() {
    private var key: Any? = null

    override suspend fun fetch(): AlertsOverview? {
        val cluster = fingerprint() ?: return null
        val found = alertmanager.sourceFor(store, cluster) ?: return null
        return AlertsOverview(found, alertmanager.alerts(found))
    }

    /** Loads once per [key] (context, config generation, invalidations); [refresh] forces it. */
    fun load(key: Any) {
        if (key == this.key) return
        this.key = key
        refresh(reset = true)
    }

    /** Nothing to load (a role without the Kubernetes API): the next [load] loads again. */
    fun forget() {
        key = null
    }
}

fun alertsCardViewModel(app: TalosApp) =
    AlertsCardViewModel(app.alertmanagerRepository, app.alertmanagerStore) { app.configRepository.config.value?.activeSummary?.fingerprint }

/**
 * Whether the card has something to show: an Alertmanager was found (or chosen), read or not.
 * Not while loading: a cluster without one would show a card that then disappears.
 */
val UiState<AlertsOverview?>.alertmanagerFound: Boolean
    get() = this is UiState.Failed || this is UiState.Loaded && data != null

/**
 * The Alertmanager alerts at a glance, opening the Alerts screen: the firing alerts by
 * severity and how many are silenced or inhibited. One calm line when nothing fires; one
 * muted line when the alerts cannot be read. Only composed once an Alertmanager was found.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun AlertsCard(state: UiState<AlertsOverview?>, onOpen: () -> Unit) {
    val colors = LocalStatusColors.current
    Card(onClick = onOpen, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            val counts = ((state as? UiState.Loaded)?.data)?.alerts?.counts
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.alerts_title), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                if (counts != null && counts.critical > 0) StatusPill(severityLabel(AmSeverity.CRITICAL), colors.bad)
                Icon(
                    Icons.AutoMirrored.Outlined.KeyboardArrowRight,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 4.dp),
                )
            }
            when {
                state is UiState.Failed -> MutedText(
                    stringResource(R.string.alerts_unreadable, state.message.asString()),
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                counts == null -> MutedText(stringResource(R.string.alerts_loading))
                counts.firing == 0 -> MutedText(stringResource(R.string.alerts_none_firing))
                else -> FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    listOf(
                        AmSeverity.CRITICAL to counts.critical,
                        AmSeverity.WARNING to counts.warning,
                        AmSeverity.INFO to counts.info,
                        AmSeverity.OTHER to counts.other,
                    ).filter { it.second > 0 }.forEach { (severity, n) ->
                        StatusPill("$n ${severityLabel(severity)}", severityColor(severity))
                    }
                }
            }
            if (counts != null && counts.suppressed > 0) {
                MutedText(pluralStringResource(R.plurals.alerts_suppressed, counts.suppressed, counts.suppressed))
            }
        }
    }
}
