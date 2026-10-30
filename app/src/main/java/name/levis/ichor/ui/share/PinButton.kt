package name.levis.ichor.ui.share

import android.widget.Toast
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material.icons.outlined.PushPin
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.flow.MutableStateFlow
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.PinStore
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.pinScope
import name.levis.ichor.model.Pin
import name.levis.ichor.model.Pins
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.components.RowAction
import name.levis.ichor.ui.components.TooltipIconButton

/** The pins of the cluster on screen ([scope], see [pinScope]), and how to change them. */
class ClusterPins(val pins: List<Pin>, private val store: PinStore, private val scope: String, private val clusterId: String) {
    fun isPinned(target: ShareTarget): Boolean = Pins.contains(pins, target)

    /** Pins or unpins [target] under [label]; false when the cluster has no room for one more. */
    fun toggle(target: ShareTarget, label: String): Boolean = store.toggle(scope, Pin(target.copy(cluster = clusterId, tab = ""), label))
}

/** The pins of the cluster on screen; null while no cluster is (nothing to pin to). */
@Composable
fun rememberClusterPins(): ClusterPins? {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val summary = config?.activeSummary
    val clusterId = summary?.clusterId.orEmpty()
    val scope = summary?.fingerprint?.takeIf { it.isNotBlank() }?.let { pinScope(it, mask) }
    val flow = remember(scope) { scope?.let(app.pinStore::pins) ?: MutableStateFlow(emptyList()) }
    val pins by flow.collectAsState()
    if (scope == null) return null
    return remember(pins, scope, clusterId) { ClusterPins(pins, app.pinStore, scope, clusterId) }
}

/** A top-bar button pinning [target] (shown as [label]) to the overview's Pinned card, or unpinning it. */
@Composable
fun PinButton(target: ShareTarget, label: String) {
    val action = rememberPinAction(target, label) ?: return
    TooltipIconButton(action.icon, action.label, onClick = action.onClick)
}

/**
 * Pin or Unpin [target] (shown as [label]) as a row action, for a long-press menu; the same
 * store as [PinButton]. Null while no cluster is on screen (nothing to pin to).
 */
@Composable
fun rememberPinAction(target: ShareTarget, label: String): RowAction? {
    val pins = rememberClusterPins() ?: return null
    val context = LocalContext.current
    val pinned = pins.isPinned(target)
    return RowAction(
        if (pinned) Icons.Filled.PushPin else Icons.Outlined.PushPin,
        stringResource(if (pinned) R.string.pin_remove else R.string.pin_add),
        onClick = {
            if (!pins.toggle(target, label)) {
                Toast.makeText(context, context.getString(R.string.pin_limit, Pins.MAX), Toast.LENGTH_LONG).show()
            }
        },
    )
}
