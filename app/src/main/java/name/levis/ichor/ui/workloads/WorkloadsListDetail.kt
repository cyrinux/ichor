package name.levis.ichor.ui.workloads

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Layers
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.ui.components.ListWithDetailPane
import name.levis.ichor.ui.components.pickedById

/**
 * The workload of [workloads] picked by its [KubeWorkload.key] ("kind/namespace/name"): the live
 * row, so the detail follows a refresh (new replica count, new revision). Null when nothing is
 * picked or the workload left the list.
 */
internal fun pickedWorkload(workloads: List<KubeWorkload>?, key: String?): KubeWorkload? =
    pickedById(workloads, key) { it.key }

/**
 * The Workloads tab on a tablet or an unfolded foldable: the list ([list]) with the picked
 * workload beside it ([detail], its sheet's content), or "Pick a workload" before a pick. Back
 * clears the pick ([onClear]). On a phone the tab draws [list] alone and a tap opens the sheet.
 */
@Composable
internal fun WorkloadsListDetail(
    workload: KubeWorkload?,
    onClear: () -> Unit,
    list: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    detail: @Composable (KubeWorkload) -> Unit,
) {
    ListWithDetailPane(
        selected = workload,
        onClear = onClear,
        emptyIcon = Icons.Outlined.Layers,
        emptyText = stringResource(R.string.panes_pick_workload),
        list = list,
        modifier = modifier,
    ) { picked ->
        // A fresh detail per workload: its confirmations and logs sheet do not carry over.
        key(picked.key) { detail(picked) }
    }
}
