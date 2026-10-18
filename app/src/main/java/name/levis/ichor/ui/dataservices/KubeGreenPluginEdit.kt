package name.levis.ichor.ui.dataservices

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.TalosApp
import name.levis.ichor.model.KubeGreenSleepInfo
import name.levis.ichor.model.PluginRef
import name.levis.ichor.model.PluginRow
import name.levis.ichor.ui.cancellableCatching

/** The built-in kube-green plugin's id (go/plugin/builtin/kube-green.yaml). */
const val KUBE_GREEN_PLUGIN = "kube-green"

private const val SLEEPINFO_GROUP = "kube-green.com"
private const val SLEEPINFO_RESOURCE = "sleepinfos"

/**
 * The native "Edit schedule" under a kube-green plugin row of a SleepInfo, until plugin actions
 * can carry a form: the schedule editor needs the SleepInfo as written, read the way the
 * workload sheets read it (KubeGreenNamespace, kept a minute). Null for any other row.
 */
fun kubeGreenRowExtra(row: PluginRow, actions: KubeGreenActions): (@Composable () -> Unit)? {
    val ref = row.ref?.takeIf { it.group == SLEEPINFO_GROUP && it.resource == SLEEPINFO_RESOURCE } ?: return null
    return { KubeGreenPluginEdit(ref, actions) }
}

/** The button, once the SleepInfo is read; nothing when it cannot be (refused, or not there). */
@Composable
private fun KubeGreenPluginEdit(ref: PluginRef, actions: KubeGreenActions) {
    val app = LocalContext.current.applicationContext as TalosApp
    val busy by actions.busy.collectAsStateWithLifecycle()
    val saved by actions.saved.collectAsStateWithLifecycle()
    val info by produceState<KubeGreenSleepInfo?>(null, ref, saved) {
        value = cancellableCatching { app.dataServicesRepository.kubeGreenNamespace(ref.namespace) }
            .getOrNull()?.sleepInfos?.firstOrNull { it.name == ref.name }
    }
    var editing by rememberSaveable(ref.key) { mutableStateOf(false) }
    val current = info ?: return
    if (editing) {
        KubeGreenScheduleSheet(current, onDismiss = { editing = false }, onSave = { schedule ->
            editing = false
            actions.setSchedule(current, schedule)
        })
    }
    KubeGreenEditButton(current, busy = current.label in busy, onEdit = { editing = true })
}
