package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Bedtime
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.KubeGreenNamespace
import name.levis.ichor.model.KubeGreenWarn
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatDateTime

// kube-green puts a namespace's workloads to sleep and restores the replicas it saved at the
// next wake-up: a workload or CronJob row opened there says so, and a scale or a resume warns
// that kube-green would undo it. The read never blocks the sheet and never fails it.

/**
 * What kube-green has done to [namespace], read once when the sheet opens and reused for a
 * minute (see [name.levis.ichor.data.DataServicesRepository.kubeGreenNamespace]). Never blocks:
 * a refusal (403) or an unreachable API just leaves null, so the sheet shows nothing new.
 */
@Composable
fun rememberKubeGreenNamespace(namespace: String): KubeGreenNamespace? {
    val app = LocalContext.current.applicationContext as TalosApp
    val status by produceState<KubeGreenNamespace?>(null, namespace) {
        if (namespace.isNotBlank()) {
            value = runCatching { app.dataServicesRepository.kubeGreenNamespace(namespace) }.getOrNull()
        }
    }
    return status
}

/**
 * The asleep line for a [kind] ("Deployment", "StatefulSet", "CronJob") row: shown when
 * kube-green has the namespace asleep and its SleepInfos suspend that kind. Nothing otherwise.
 */
@Composable
fun KubeGreenAsleepNote(status: KubeGreenNamespace?, kind: String) {
    if (status == null || !status.asleep || !status.sleeps(kind)) return
    val colors = LocalStatusColors.current
    Row(
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Outlined.Bedtime, contentDescription = null, tint = colors.warn, modifier = Modifier.size(16.dp))
        Text(kubeGreenAsleepLine(status), style = MaterialTheme.typography.bodySmall, color = colors.warn)
    }
}

/** "Asleep: kube-green has this namespace asleep (SleepInfo staging/nights), wakes up 15 Oct, 07:00". */
@Composable
private fun kubeGreenAsleepLine(status: KubeGreenNamespace): String {
    val when0 = if (status.nextWakeUpAt > 0) {
        stringResource(R.string.kubegreen_wakes, formatDateTime(status.nextWakeUpAt))
    } else {
        stringResource(R.string.kubegreen_until_changed)
    }
    val sleepInfo = (status.asleepInfo ?: status.sleepInfos.firstOrNull())?.label.orEmpty()
    return stringResource(
        if (status.filtered) R.string.kubegreen_asleep_note_maybe else R.string.kubegreen_asleep_note,
        sleepInfo,
        when0,
    )
}

/**
 * The warning to show before a change kube-green would undo: while the namespace sleeps, or
 * when it sleeps within a day. Null when kube-green will not touch it, or the state is unknown.
 */
@Composable
fun kubeGreenChangeWarning(status: KubeGreenNamespace?): String? {
    val warn = status?.warnBeforeChange() ?: return null
    return when (warn) {
        KubeGreenWarn.ASLEEP ->
            if (status.nextWakeUpAt > 0) {
                stringResource(R.string.kubegreen_warn_asleep, formatDateTime(status.nextWakeUpAt))
            } else {
                stringResource(R.string.kubegreen_warn_asleep_until)
            }
        KubeGreenWarn.SLEEPS_SOON -> stringResource(R.string.kubegreen_warn_soon, formatDateTime(status.nextSleepAt))
    }
}

/**
 * The line under a suspended CronJob kube-green would have suspended anyway: "Suspended by
 * kube-green, resumes 15 Oct, 07:00". Nothing when the CronJob is running or the namespace is awake.
 */
@Composable
fun KubeGreenCronJobNote(status: KubeGreenNamespace?, suspended: Boolean) {
    if (status == null || !suspended || !status.asleep || !status.sleepsCronJobs) return
    val colors = LocalStatusColors.current
    val when0 = if (status.nextWakeUpAt > 0) {
        stringResource(R.string.kubegreen_resumes, formatDateTime(status.nextWakeUpAt))
    } else {
        stringResource(R.string.kubegreen_until_changed)
    }
    Row(
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Outlined.Bedtime, contentDescription = null, tint = colors.warn, modifier = Modifier.size(16.dp))
        Text(stringResource(R.string.kubegreen_cronjob_suspended, when0), style = MaterialTheme.typography.bodySmall, color = colors.warn)
    }
}
