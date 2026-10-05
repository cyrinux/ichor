package name.levis.ichor.update

import android.content.Context
import androidx.activity.ComponentActivity
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** The open-source builds update themselves from GitHub releases (UpdateManager). */
@Suppress("UNUSED_PARAMETER")
fun createStoreUpdater(context: Context): StoreUpdater = NoStoreUpdater

private object NoStoreUpdater : StoreUpdater {
    override val state: StateFlow<StoreUpdateState> = MutableStateFlow(StoreUpdateState.None).asStateFlow()

    override fun attach(activity: ComponentActivity) = Unit

    override fun start() = Unit

    override fun install() = Unit
}
