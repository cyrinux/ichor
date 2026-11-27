package name.levis.ichor.ui.alerts

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.AmAlert
import name.levis.ichor.model.AmAlerts
import name.levis.ichor.ui.components.ListWithDetailPane
import name.levis.ichor.ui.components.pickedById

/**
 * What picks [this] alert in the detail pane, saved across rotation and process death: its
 * Alertmanager fingerprint, or, without one, its alertname and labels (an alert's identity).
 */
internal val AmAlert.paneId: String
    get() = fingerprint.ifEmpty {
        alertname + labels.toSortedMap().entries.joinToString(",", prefix = "{", postfix = "}") { "${it.key}=${it.value}" }
    }

/**
 * The alert of [alerts] picked by its [paneId]: the one last read, so the pane follows a refresh
 * (now silenced, now an incident behind it). Null when nothing is picked, the alerts are not
 * read yet, or it stopped firing.
 */
internal fun pickedAlert(alerts: AmAlerts?, id: String?): AmAlert? =
    pickedById(alerts?.groups?.flatMap { it.alerts }, id) { it.paneId }

/**
 * The Alerts tab on a tablet or an unfolded foldable: the list ([list]) with the picked alert
 * beside it ([detail], its sheet's content), or "Pick an alert" before a pick. Back clears the
 * pick ([onClear]). On a phone the tab draws [list] alone and a tap opens the sheet.
 */
@Composable
internal fun AlertsListDetail(
    alert: AmAlert?,
    onClear: () -> Unit,
    list: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    detail: @Composable (AmAlert) -> Unit,
) {
    ListWithDetailPane(
        selected = alert,
        onClear = onClear,
        emptyIcon = Icons.Outlined.NotificationsActive,
        emptyText = stringResource(R.string.panes_pick_alert),
        list = list,
        modifier = modifier,
    ) { picked ->
        // A fresh detail per alert: its scroll position and AI card do not carry over.
        key(picked.paneId) { detail(picked) }
    }
}
