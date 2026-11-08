package name.levis.ichor.security

import android.app.Activity
import android.content.Context
import android.os.Bundle
import androidx.activity.compose.setContent
import androidx.compose.runtime.getValue
import androidx.fragment.app.FragmentActivity
import androidx.glance.appwidget.updateAll
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import name.levis.ichor.TalosApp
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.monitor.cancelPostedAlerts
import name.levis.ichor.ui.theme.TalosTheme
import name.levis.ichor.widget.ClusterWidget
import name.levis.ichor.widget.FleetWidget

/** Outlives the activity: a delete that started must finish, whatever happens to the screen. */
private val deletions = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

/**
 * Answers a panic app (Ripple or any PanicKit trigger, see PanicResponder.kt). An activity, not a
 * receiver: only an activity can tell who sent the intent. No launchMode in the manifest:
 * getCallingActivity() is null under singleTask and singleInstance, and a trigger would only lock.
 */
class PanicResponderActivity : FragmentActivity() {
    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(AppLocale.wrap(newBase))
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val app = application as TalosApp
        val lock = app.appLock
        val caller = callingActivity?.packageName
        when (val request = panicRequest(intent?.action, caller, lock.panicTriggerPackage.value, lock.panicResponse.value)) {
            is PanicRequest.Trigger -> {
                // Shows nothing: the panic app goes on to its next responder.
                cancelPostedAlerts(app)
                if (request.response == PanicResponse.DELETE_CONFIG) deleteConfig(app) else lock.lock()
                finish()
            }
            is PanicRequest.Connect -> showConfig(app, request.callingPackage)
            PanicRequest.Disconnect -> {
                lock.setPanicConnection(null, PanicResponse.LOCK)
                finish()
            }
            PanicRequest.Ignore -> finish()
        }
    }

    private fun showConfig(app: TalosApp, caller: String) {
        setContent {
            val themeMode by app.uiPreferences.themeMode.collectAsStateWithLifecycle()
            TalosTheme(themeMode) {
                PanicConfigDialog(
                    appLock = app.appLock,
                    triggerPackage = caller,
                    onConnected = { response ->
                        app.appLock.setPanicConnection(caller, response)
                        setResult(Activity.RESULT_OK)
                        finish()
                    },
                    // Whatever was connected before stays as it was.
                    onDismiss = {
                        setResult(Activity.RESULT_CANCELED)
                        finish()
                    },
                )
            }
        }
    }
}

/**
 * What the lock screen's "delete config" does: only Ichor's copy of the configs and sign-ins on
 * this phone goes, nothing is sent to any cluster. Each step runs even if the one before failed.
 */
private fun deleteConfig(app: TalosApp) {
    deletions.launch {
        runCatching { app.configRepository.clear() }
        runCatching { app.kubeAuthStore.clear() }
        // A running app drops its screens before the lock comes off, as after the lock screen's delete.
        app.appLock.notifyConfigDeleted()
        app.appLock.setEnabled(false)
        runCatching { ClusterWidget().updateAll(app) }
        runCatching { FleetWidget().updateAll(app) }
    }
}
