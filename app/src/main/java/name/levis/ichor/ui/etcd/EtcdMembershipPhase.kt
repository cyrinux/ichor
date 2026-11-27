package name.levis.ichor.ui.etcd

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R

/** The chip label of a Talos etcd membership phase; an unknown phase is shown as Talos names it. */
@Composable
fun membershipPhaseLabel(phase: String): String = when (phase) {
    "preparing" -> stringResource(R.string.etcd_phase_preparing)
    "joining" -> stringResource(R.string.etcd_phase_joining)
    "learner" -> stringResource(R.string.etcd_status_learner)
    "leaving" -> stringResource(R.string.etcd_phase_leaving)
    "left" -> stringResource(R.string.etcd_phase_left)
    else -> phase
}
