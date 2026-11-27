package name.levis.ichor.ui.settings

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.os.Build
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.core.app.NotificationManagerCompat
import androidx.lifecycle.compose.LifecycleResumeEffect
import name.levis.ichor.R
import name.levis.ichor.monitor.canPostNotifications
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorCard
import name.levis.ichor.ui.design.IchorOutlinedButton

/**
 * Android 16 and later, when the user turned Live Updates off for the app (notifications still
 * allowed): says what they bring to a running operation and opens the system page to allow them
 * again. Nothing otherwise, read again each time the screen comes back.
 */
@Composable
fun LiveUpdatesRow() {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.BAKLAVA) return
    val context = LocalContext.current
    var off by remember { mutableStateOf(false) }
    LifecycleResumeEffect(Unit) {
        off = canPostNotifications(context) && !NotificationManagerCompat.from(context).canPostPromotedNotifications()
        onPauseOrDispose {}
    }
    if (!off) return
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.live_updates_off_title), style = MaterialTheme.typography.titleSmall)
            MutedText(stringResource(R.string.live_updates_off_desc))
            IchorOutlinedButton(onClick = { openPromotionSettings(context) }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.live_updates_allow))
            }
        }
    }
}

/** The app's Live Updates page; its notification settings where a device has no such page. */
private fun openPromotionSettings(context: Context) {
    val notifications = Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
    val page = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.BAKLAVA) {
        Intent(Settings.ACTION_APP_NOTIFICATION_PROMOTION_SETTINGS)
    } else {
        notifications
    }
    try {
        context.startActivity(forApp(context, page))
    } catch (_: ActivityNotFoundException) {
        context.startActivity(forApp(context, notifications))
    }
}

private fun forApp(context: Context, intent: Intent): Intent =
    Intent(intent).putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
