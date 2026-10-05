package name.levis.ichor.update

import androidx.activity.ComponentActivity
import kotlinx.coroutines.flow.StateFlow

sealed interface StoreUpdateState {
    data object None : StoreUpdateState

    /** Newer on the store, not downloaded: declined, or the offer failed. */
    data object Available : StoreUpdateState
    data object Downloading : StoreUpdateState

    /** Downloaded in the background: restarting the app installs it. */
    data object Downloaded : StoreUpdateState
}

/**
 * Updates delivered by the store the app came from: Google Play in-app updates in the Play
 * build, nothing in the open-source builds, which update themselves (see [UpdateManager] and
 * `createStoreUpdater`).
 */
interface StoreUpdater {
    val state: StateFlow<StoreUpdateState>

    /**
     * From the activity's onCreate. Asks the store on every resume, and offers each new version
     * once on its own (the store's dialog); accepted, it downloads while the app is used.
     */
    fun attach(activity: ComponentActivity)

    /** Offers the available update again (the overview card, after the user declined it). */
    fun start()

    /** Restarts the app into the downloaded update. */
    fun install()
}
