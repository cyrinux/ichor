package name.levis.ichor.ui.settings

import android.content.Intent
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
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
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.security.AppLock
import name.levis.ichor.security.PanicActions
import name.levis.ichor.security.PanicConfigDialog
import name.levis.ichor.security.PanicResponse
import name.levis.ichor.security.panicAppLabel
import name.levis.ichor.ui.components.MutedText

/**
 * The panic app connected to Ichor, if any (see security/PanicResponder.kt). Connecting starts
 * from the panic app; shown without a lock too, so the feature can be found.
 */
@Composable
fun PanicSection(appLock: AppLock) {
    val context = LocalContext.current
    val trigger by appLock.panicTriggerPackage.collectAsStateWithLifecycle()
    val response by appLock.panicResponse.collectAsStateWithLifecycle()
    var changing by remember { mutableStateOf(false) }
    // For a result, so the panic app can tell who disconnects (getCallingActivity).
    val tellTrigger = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) {}

    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.panic_section_title), style = MaterialTheme.typography.titleMedium)
            val connected = trigger
            if (connected == null) {
                MutedText(stringResource(R.string.panic_not_connected))
            } else {
                Text(stringResource(R.string.panic_connected_app, panicAppLabel(context, connected)), style = MaterialTheme.typography.bodyMedium)
                MutedText(stringResource(if (response == PanicResponse.DELETE_CONFIG) R.string.panic_response_delete else R.string.panic_response_lock))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    IchorOutlinedButton(onClick = { changing = true }) { Text(stringResource(R.string.panic_change)) }
                    IchorOutlinedButton(onClick = {
                        val disconnect = Intent(PanicActions.DISCONNECT).setPackage(connected)
                        if (context.packageManager.resolveActivity(disconnect, 0) != null) tellTrigger.launch(disconnect)
                        appLock.setPanicConnection(null, PanicResponse.LOCK)
                    }) { Text(stringResource(R.string.panic_disconnect)) }
                }
                if (changing) {
                    PanicConfigDialog(
                        appLock = appLock,
                        triggerPackage = connected,
                        onConnected = {
                            appLock.setPanicConnection(connected, it)
                            changing = false
                        },
                        onDismiss = { changing = false },
                    )
                }
            }
        }
    }
}
