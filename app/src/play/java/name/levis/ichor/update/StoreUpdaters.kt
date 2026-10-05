package name.levis.ichor.update

import android.content.Context
import android.content.SharedPreferences
import androidx.activity.ComponentActivity
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.IntentSenderRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import com.google.android.play.core.appupdate.AppUpdateInfo
import com.google.android.play.core.appupdate.AppUpdateManagerFactory
import com.google.android.play.core.appupdate.AppUpdateOptions
import com.google.android.play.core.install.model.AppUpdateType
import com.google.android.play.core.install.model.InstallStatus
import com.google.android.play.core.install.model.UpdateAvailability
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** Google Play in-app updates (the flexible flow: the app stays usable while it downloads). */
fun createStoreUpdater(context: Context): StoreUpdater =
    PlayStoreUpdater(context.applicationContext, context.getSharedPreferences(PREFS, Context.MODE_PRIVATE))

private const val PREFS = "ichor-store-update"

/** The last version code offered on its own: each new version is offered once, not on every launch. */
private const val KEY_OFFERED = "offered_version_code"

private class PlayStoreUpdater(context: Context, private val prefs: SharedPreferences) : StoreUpdater {
    private val manager = AppUpdateManagerFactory.create(context)

    private val _state = MutableStateFlow<StoreUpdateState>(StoreUpdateState.None)
    override val state: StateFlow<StoreUpdateState> = _state.asStateFlow()

    /** The current activity's, to launch Play's dialog; null while no activity is alive. */
    private var launcher: ActivityResultLauncher<IntentSenderRequest>? = null

    init {
        // Lives as long as the app: the download goes on while the user moves between screens.
        manager.registerListener { install ->
            _state.value = when (install.installStatus()) {
                InstallStatus.PENDING, InstallStatus.DOWNLOADING -> StoreUpdateState.Downloading
                InstallStatus.DOWNLOADED -> StoreUpdateState.Downloaded
                InstallStatus.FAILED, InstallStatus.CANCELED -> StoreUpdateState.Available
                else -> _state.value
            }
        }
    }

    override fun attach(activity: ComponentActivity) {
        // Declined or failed, the update stays Available (the overview card offers it again);
        // accepted, the listener above follows the download.
        val registered = activity.registerForActivityResult(ActivityResultContracts.StartIntentSenderForResult()) {}
        launcher = registered
        activity.lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onResume(owner: LifecycleOwner) = refresh(offer = false)

            override fun onDestroy(owner: LifecycleOwner) {
                if (launcher === registered) launcher = null
            }
        })
    }

    override fun start() = refresh(offer = true)

    override fun install() {
        manager.completeUpdate()
    }

    /**
     * Asks Play how the app stands. [offer]: show Play's dialog for an available update even if
     * this version was offered before. An [AppUpdateInfo] starts one flow only, hence a fresh one.
     */
    private fun refresh(offer: Boolean) {
        manager.appUpdateInfo
            .addOnSuccessListener { info ->
                _state.value = info.toState()
                if (_state.value != StoreUpdateState.Available) return@addOnSuccessListener
                val version = info.availableVersionCode()
                if (offer || prefs.getInt(KEY_OFFERED, 0) < version) launch(info, version)
            }
            // No Play Store, or a build Play did not install (ERROR_APP_NOT_OWNED): nothing to offer.
            .addOnFailureListener { _state.value = StoreUpdateState.None }
    }

    private fun launch(info: AppUpdateInfo, version: Int) {
        val launcher = launcher ?: return
        prefs.edit().putInt(KEY_OFFERED, version).apply()
        manager.startUpdateFlowForResult(info, launcher, AppUpdateOptions.newBuilder(AppUpdateType.FLEXIBLE).build())
    }

    private fun AppUpdateInfo.toState(): StoreUpdateState = when {
        installStatus() == InstallStatus.DOWNLOADED -> StoreUpdateState.Downloaded
        installStatus() == InstallStatus.PENDING || installStatus() == InstallStatus.DOWNLOADING -> StoreUpdateState.Downloading
        updateAvailability() == UpdateAvailability.UPDATE_AVAILABLE && isUpdateTypeAllowed(AppUpdateType.FLEXIBLE) ->
            StoreUpdateState.Available
        else -> StoreUpdateState.None
    }
}
