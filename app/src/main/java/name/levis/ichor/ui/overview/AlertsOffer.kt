package name.levis.ichor.ui.overview

import android.Manifest
import android.content.Intent
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material3.Button
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.monitor.canPostNotifications

/**
 * Whether the home offers background alerts (the card below): once, after the first real cluster
 * is added, until turned on or dismissed; never on the demo nor over the demo tour.
 */
fun shouldOfferAlerts(pending: Boolean, alertsEnabled: Boolean, isDemo: Boolean, tourActive: Boolean): Boolean =
    pending && !alertsEnabled && !isDemo && !tourActive

/** [shouldOfferAlerts] for the cluster on screen ([isDemo]), from the app's settings. */
@Composable
fun rememberAlertsOffer(isDemo: Boolean): Boolean {
    val app = LocalContext.current.applicationContext as TalosApp
    val pending by app.uiPreferences.alertsOfferPending.collectAsStateWithLifecycle()
    val enabled by app.monitorStore.alertsEnabled.collectAsStateWithLifecycle()
    val tour by app.uiPreferences.tourPending.collectAsStateWithLifecycle()
    return shouldOfferAlerts(pending, enabled, isDemo, tour)
}

/**
 * Background alerts, offered in context: the notification permission is asked here, when the
 * person chose to be paged, not at install. Refused (Android 13+), it points at the app's
 * notification settings instead of asking again. "Not now" leaves the toggle in Settings.
 */
@Composable
fun AlertsOfferCard() {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    var blocked by remember { mutableStateOf(false) }

    fun turnOn() {
        app.monitorStore.setAlertsEnabled(true)
        app.launchSync(runNow = true)
        app.uiPreferences.setAlertsOfferPending(false)
    }

    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) turnOn() else blocked = true
    }

    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Icon(Icons.Outlined.NotificationsActive, contentDescription = null)
                Text(stringResource(R.string.alerts_offer_title), style = MaterialTheme.typography.titleMedium)
            }
            Text(stringResource(R.string.alerts_offer_body), style = MaterialTheme.typography.bodyMedium)
            if (blocked) {
                Text(stringResource(R.string.monitor_notifications_blocked), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                TextButton(onClick = { app.uiPreferences.setAlertsOfferPending(false) }) {
                    Text(stringResource(R.string.alerts_offer_not_now))
                }
                if (blocked) {
                    Button(onClick = {
                        context.startActivity(
                            Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
                                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                        )
                    }) { Text(stringResource(R.string.alerts_offer_open_settings)) }
                } else {
                    Button(onClick = {
                        if (!canPostNotifications(context) && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                            permission.launch(Manifest.permission.POST_NOTIFICATIONS)
                        } else {
                            turnOn()
                        }
                    }) { Text(stringResource(R.string.alerts_offer_turn_on)) }
                }
            }
        }
    }
}
