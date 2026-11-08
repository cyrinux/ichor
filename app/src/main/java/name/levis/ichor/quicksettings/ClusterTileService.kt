package name.levis.ichor.quicksettings

import android.annotation.SuppressLint
import android.app.PendingIntent
import android.app.StatusBarManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.graphics.drawable.Icon
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import androidx.annotation.RequiresApi
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.i18n.AppLocale

/**
 * The Quick Settings tile: the cluster on screen and its health from the last background check,
 * the snapshot the home widget shows (no network call, nothing unsealed: the system binds this
 * service without the app running). A tap opens the app; a long press opens the checkup
 * (MainActivity's QS_TILE_PREFERENCES filter). Counts only, as on the lock screen.
 */
class ClusterTileService : TileService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var watch: Job? = null

    override fun onStartListening() {
        super.onStartListening()
        val app = application as TalosApp
        // Followed while the shade is open: the config may finish loading, a check may land.
        watch?.cancel()
        watch = scope.launch {
            combine(
                app.monitorStore.state,
                app.configRepository.config,
                app.clusterNames.names,
                app.uiPreferences.privacyMask,
            ) { state, stored, names, mask -> tileView(state, stored?.summary, stored?.activeContext, names, mask.enabled) }
                .collect(::show)
        }
    }

    override fun onStopListening() {
        watch?.cancel()
        watch = null
        super.onStopListening()
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun show(view: TileView) {
        val tile = qsTile ?: return
        // The service's context is the application's: apply the in-app language to it.
        val res = AppLocale.wrap(this).resources
        tile.state = when (view.health) {
            TileHealth.READY, TileHealth.DEGRADED -> Tile.STATE_ACTIVE
            TileHealth.NO_CHECK -> Tile.STATE_INACTIVE
            TileHealth.NO_CLUSTER -> Tile.STATE_UNAVAILABLE
        }
        tile.label = view.label ?: res.getString(R.string.tile_cluster_label)
        val subtitle = when (view.health) {
            TileHealth.READY -> res.getString(R.string.tile_cluster_subtitle_ready)
            TileHealth.DEGRADED -> res.getString(R.string.tile_cluster_subtitle_degraded, view.ready, view.total)
            TileHealth.NO_CHECK -> res.getString(R.string.tile_cluster_no_check)
            TileHealth.NO_CLUSTER -> res.getString(R.string.tile_cluster_no_cluster)
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) tile.subtitle = subtitle
        // Read aloud whole: the subtitle alone shows from Android 10 on.
        tile.contentDescription = "${tile.label}, $subtitle"
        tile.updateTile()
    }

    override fun onClick() {
        super.onClick()
        // The app lock asks for itself once open; the device's own lock first.
        if (isLocked) unlockAndRun(::openApp) else openApp()
    }

    @SuppressLint("StartActivityAndCollapseDeprecated")
    private fun openApp() {
        val intent = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startActivityAndCollapse(PendingIntent.getActivity(this, 0, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
        } else {
            // Android 14 made this form throw; below it, it is the only one.
            @Suppress("DEPRECATION")
            startActivityAndCollapse(intent)
        }
    }

    companion object {
        fun component(context: Context) = ComponentName(context, ClusterTileService::class.java)

        /**
         * Offers to add the tile to Quick Settings (Android 13+): the system asks the user, once;
         * nothing happens when it is already there.
         */
        @RequiresApi(Build.VERSION_CODES.TIRAMISU)
        fun requestAdd(context: Context) {
            val manager = context.getSystemService(StatusBarManager::class.java) ?: return
            val label = AppLocale.wrap(context).getString(R.string.tile_cluster_label)
            runCatching {
                manager.requestAddTileService(component(context), label, Icon.createWithResource(context, R.drawable.ic_stat_ichor), context.mainExecutor) {}
            }
        }

        /** Asks the system to redraw the tile (an open shade, or the next time it opens). */
        fun refresh(context: Context) {
            runCatching { requestListeningState(context, component(context)) }
        }
    }
}
