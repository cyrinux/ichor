package name.levis.ichor.ui.checkup

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Anchor
import androidx.compose.material.icons.outlined.DeleteSweep
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.NotificationImportant
import androidx.compose.material.icons.outlined.Speed
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Upgrade
import androidx.compose.material.icons.outlined.VerifiedUser
import androidx.compose.material.icons.outlined.Webhook
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.CheckupFinding
import name.levis.ichor.model.CheckupKind
import name.levis.ichor.model.CheckupSectionId
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.formatPercent
import kotlin.math.roundToInt

/** How a section introduces itself: its icon, its name and what it looks for. */
data class SectionLook(val icon: ImageVector, @StringRes val title: Int, @StringRes val hint: Int)

fun sectionLook(id: String): SectionLook? = when (id) {
    CheckupSectionId.WORKLOADS -> SectionLook(Icons.Outlined.Widgets, R.string.checkup_section_workloads, R.string.checkup_section_workloads_hint)
    CheckupSectionId.EVENTS -> SectionLook(Icons.Outlined.NotificationImportant, R.string.checkup_section_events, R.string.checkup_section_events_hint)
    CheckupSectionId.STORAGE -> SectionLook(Icons.Outlined.Storage, R.string.checkup_section_storage, R.string.checkup_section_storage_hint)
    CheckupSectionId.UPGRADE -> SectionLook(Icons.Outlined.Upgrade, R.string.checkup_section_upgrade, R.string.checkup_section_upgrade_hint)
    CheckupSectionId.WEBHOOKS -> SectionLook(Icons.Outlined.Webhook, R.string.checkup_section_webhooks, R.string.checkup_section_webhooks_hint)
    CheckupSectionId.CAPACITY -> SectionLook(Icons.Outlined.Speed, R.string.checkup_section_capacity, R.string.checkup_section_capacity_hint)
    CheckupSectionId.NODES -> SectionLook(Icons.Outlined.Dns, R.string.checkup_section_nodes, R.string.checkup_section_nodes_hint)
    CheckupSectionId.LOAD_BALANCERS -> SectionLook(Icons.Outlined.Lan, R.string.checkup_section_loadbalancers, R.string.checkup_section_loadbalancers_hint)
    CheckupSectionId.TERMINATING -> SectionLook(Icons.Outlined.DeleteSweep, R.string.checkup_section_terminating, R.string.checkup_section_terminating_hint)
    CheckupSectionId.CERTIFICATES -> SectionLook(Icons.Outlined.VerifiedUser, R.string.checkup_section_certificates, R.string.checkup_section_certificates_hint)
    CheckupSectionId.SECRETS -> SectionLook(Icons.Outlined.Key, R.string.checkup_section_secrets, R.string.checkup_section_secrets_hint)
    CheckupSectionId.HELM -> SectionLook(Icons.Outlined.Anchor, R.string.checkup_section_helm, R.string.checkup_section_helm_hint)
    CheckupSectionId.MONITORING -> SectionLook(Icons.Outlined.MonitorHeart, R.string.checkup_section_monitoring, R.string.checkup_section_monitoring_hint)
    else -> null
}

/** How long ago [since] was at [now], "" when unknown. */
@Composable
fun ageSince(since: Long, now: Long): String =
    if (since > 0) localizedDuration(((now - since) / 1000).coerceAtLeast(0)) else ""

@Composable
private fun resourceName(resource: String): String =
    if (resource == "memory") stringResource(R.string.checkup_memory) else stringResource(R.string.checkup_cpu)

private fun percent(value: Double): String = formatPercent(value, decimals = if (value >= 99.95 || value % 1.0 == 0.0) 0 else 1)

/** What the finding says happens, in the user's words; the kind's name for one this version does not know. */
@Composable
fun checkupTitle(f: CheckupFinding, now: Long): String {
    val age = ageSince(f.since, now)
    return when (f.kind) {
        CheckupKind.POD_CRASH_LOOP -> stringResource(R.string.checkup_podCrashLoop, f.count.toString())
        CheckupKind.POD_IMAGE_PULL -> stringResource(R.string.checkup_podImagePull, f.extra)
        CheckupKind.POD_UNSCHEDULABLE -> stringResource(R.string.checkup_podUnschedulable, age)
        CheckupKind.POD_STUCK_STARTING -> stringResource(R.string.checkup_podStuckStarting, age)
        CheckupKind.POD_NOT_READY -> stringResource(R.string.checkup_podNotReady, f.count.toString(), f.limit.roundToInt().toString())
        CheckupKind.POD_FAILED -> stringResource(R.string.checkup_podFailed, f.reason)
        CheckupKind.POD_OOM_KILLED -> stringResource(R.string.checkup_podOOMKilled, f.count.toString())
        CheckupKind.JOB_FAILED -> stringResource(R.string.checkup_jobFailed, age)
        CheckupKind.EVENT -> stringResource(R.string.checkup_event, f.reason, f.count.toString(), age)
        CheckupKind.VOLUME_FULL -> stringResource(R.string.checkup_volumeFull, percent(f.value), formatBytes(f.limit.toLong()))
        CheckupKind.VOLUME_INODES -> stringResource(R.string.checkup_volumeInodes, percent(f.value))
        CheckupKind.PVC_PENDING -> stringResource(R.string.checkup_pvcPending, age, f.extra.ifEmpty { "-" })
        CheckupKind.PVC_LOST -> stringResource(R.string.checkup_pvcLost)
        CheckupKind.PV_FAILED -> stringResource(R.string.checkup_pvFailed)
        CheckupKind.PV_RELEASED -> stringResource(R.string.checkup_pvReleased, f.extra)
        CheckupKind.DEPRECATED_API ->
            if (f.extra.isEmpty()) stringResource(R.string.checkup_deprecatedAPI_unplanned) else stringResource(R.string.checkup_deprecatedAPI, f.extra)
        CheckupKind.WEBHOOK_DOWN, CheckupKind.WEBHOOK_SKIPPED -> stringResource(R.string.checkup_webhook, f.reason, f.extra)
        CheckupKind.NODE_REQUESTS_HIGH -> stringResource(R.string.checkup_nodeRequestsHigh, percent(f.value), resourceName(f.extra))
        CheckupKind.NODE_PODS_FULL -> stringResource(R.string.checkup_nodePodsFull, f.count.toString(), f.limit.roundToInt().toString())
        CheckupKind.NO_ROOM_TO_DRAIN -> stringResource(R.string.checkup_noRoomToDrain, resourceName(f.extra))
        CheckupKind.QUOTA_NEAR_LIMIT -> stringResource(R.string.checkup_quotaNearLimit, percent(f.value), f.extra)
        CheckupKind.NODE_NOT_READY -> stringResource(R.string.checkup_nodeNotReady)
        CheckupKind.NODE_PRESSURE -> stringResource(R.string.checkup_nodePressure, f.extra)
        CheckupKind.NODE_CORDONED ->
            if (age.isEmpty()) stringResource(R.string.checkup_nodeCordoned_plain) else stringResource(R.string.checkup_nodeCordoned, age)
        CheckupKind.NODE_VERSION_SKEW -> stringResource(R.string.checkup_nodeVersionSkew, f.extra, f.reason)
        CheckupKind.LB_PENDING -> stringResource(R.string.checkup_lbPending, age)
        CheckupKind.LB_POOL_EXHAUSTED -> stringResource(R.string.checkup_lbPoolExhausted, f.value.roundToInt().toString())
        CheckupKind.LB_POOL_CONFLICT -> stringResource(R.string.checkup_lbPoolConflict)
        CheckupKind.NAMESPACE_TERMINATING, CheckupKind.POD_TERMINATING, CheckupKind.PVC_TERMINATING ->
            stringResource(R.string.checkup_terminating, age)
        CheckupKind.NETPERF_LEFTOVER -> stringResource(R.string.checkup_netperfLeftover, age)
        CheckupKind.CSR_PENDING -> stringResource(R.string.checkup_csrPending, f.count.toString(), f.extra)
        CheckupKind.CSR_DENIED -> stringResource(R.string.checkup_csrDenied, f.count.toString(), f.extra)
        CheckupKind.EXTERNAL_SECRET_FAILED -> stringResource(R.string.checkup_externalSecretFailed, f.extra)
        CheckupKind.SECRET_STORE_NOT_READY -> stringResource(R.string.checkup_secretStoreNotReady, f.extra)
        CheckupKind.HELM_FAILED -> stringResource(R.string.checkup_helmFailed, f.count.toString())
        CheckupKind.HELM_PENDING -> stringResource(R.string.checkup_helmPending, f.reason, age)
        CheckupKind.SCRAPE_TARGETS_DOWN ->
            if (f.extra.isEmpty()) {
                stringResource(R.string.checkup_scrapeTargetsDown_plain, f.count.toString(), f.limit.roundToInt().toString(), percent(f.value))
            } else {
                stringResource(R.string.checkup_scrapeTargetsDown, f.count.toString(), f.limit.roundToInt().toString(), percent(f.value), f.extra)
            }
        CheckupKind.PROMETHEUS_RULE_ERRORS, CheckupKind.RULE_GROUP_ERRORS -> stringResource(R.string.checkup_ruleErrors, f.count.toString(), f.reason)
        else -> f.kind
    }
}

/** What to do about it; 0 for a kind without advice (an event) or one this version does not know. */
@StringRes
fun checkupFix(kind: String): Int = when (kind) {
    CheckupKind.POD_CRASH_LOOP -> R.string.checkup_podCrashLoop_fix
    CheckupKind.POD_IMAGE_PULL -> R.string.checkup_podImagePull_fix
    CheckupKind.POD_UNSCHEDULABLE -> R.string.checkup_podUnschedulable_fix
    CheckupKind.POD_STUCK_STARTING -> R.string.checkup_podStuckStarting_fix
    CheckupKind.POD_NOT_READY -> R.string.checkup_podNotReady_fix
    CheckupKind.POD_FAILED -> R.string.checkup_podFailed_fix
    CheckupKind.POD_OOM_KILLED -> R.string.checkup_podOOMKilled_fix
    CheckupKind.JOB_FAILED -> R.string.checkup_jobFailed_fix
    CheckupKind.VOLUME_FULL -> R.string.checkup_volumeFull_fix
    CheckupKind.VOLUME_INODES -> R.string.checkup_volumeInodes_fix
    CheckupKind.PVC_PENDING -> R.string.checkup_pvcPending_fix
    CheckupKind.PVC_LOST -> R.string.checkup_pvcLost_fix
    CheckupKind.PV_FAILED -> R.string.checkup_pvFailed_fix
    CheckupKind.PV_RELEASED -> R.string.checkup_pvReleased_fix
    CheckupKind.DEPRECATED_API -> R.string.checkup_deprecatedAPI_fix
    CheckupKind.WEBHOOK_DOWN -> R.string.checkup_webhookDown_fix
    CheckupKind.WEBHOOK_SKIPPED -> R.string.checkup_webhookSkipped_fix
    CheckupKind.NODE_REQUESTS_HIGH -> R.string.checkup_nodeRequestsHigh_fix
    CheckupKind.NODE_PODS_FULL -> R.string.checkup_nodePodsFull_fix
    CheckupKind.NO_ROOM_TO_DRAIN -> R.string.checkup_noRoomToDrain_fix
    CheckupKind.QUOTA_NEAR_LIMIT -> R.string.checkup_quotaNearLimit_fix
    CheckupKind.NODE_NOT_READY -> R.string.checkup_nodeNotReady_fix
    CheckupKind.NODE_PRESSURE -> R.string.checkup_nodePressure_fix
    CheckupKind.NODE_CORDONED -> R.string.checkup_nodeCordoned_fix
    CheckupKind.NODE_VERSION_SKEW -> R.string.checkup_nodeVersionSkew_fix
    CheckupKind.LB_PENDING -> R.string.checkup_lbPending_fix
    CheckupKind.LB_POOL_EXHAUSTED -> R.string.checkup_lbPoolExhausted_fix
    CheckupKind.LB_POOL_CONFLICT -> R.string.checkup_lbPoolConflict_fix
    CheckupKind.NAMESPACE_TERMINATING -> R.string.checkup_namespaceTerminating_fix
    CheckupKind.POD_TERMINATING -> R.string.checkup_podTerminating_fix
    CheckupKind.PVC_TERMINATING -> R.string.checkup_pvcTerminating_fix
    CheckupKind.NETPERF_LEFTOVER -> R.string.checkup_netperfLeftover_fix
    CheckupKind.CSR_PENDING -> R.string.checkup_csrPending_fix
    CheckupKind.CSR_DENIED -> R.string.checkup_csrDenied_fix
    CheckupKind.EXTERNAL_SECRET_FAILED -> R.string.checkup_externalSecretFailed_fix
    CheckupKind.SECRET_STORE_NOT_READY -> R.string.checkup_secretStoreNotReady_fix
    CheckupKind.HELM_FAILED -> R.string.checkup_helmFailed_fix
    CheckupKind.HELM_PENDING -> R.string.checkup_helmPending_fix
    CheckupKind.SCRAPE_TARGETS_DOWN -> R.string.checkup_scrapeTargetsDown_fix
    CheckupKind.PROMETHEUS_RULE_ERRORS -> R.string.checkup_prometheusRuleErrors_fix
    CheckupKind.RULE_GROUP_ERRORS -> R.string.checkup_ruleGroupErrors_fix
    else -> 0
}

/** The kinds whose [CheckupFinding.reason] says more than their title does. */
private val REASON_KINDS = setOf(
    CheckupKind.POD_CRASH_LOOP, CheckupKind.POD_OOM_KILLED, CheckupKind.POD_STUCK_STARTING, CheckupKind.JOB_FAILED,
    CheckupKind.NODE_NOT_READY, CheckupKind.NODE_PRESSURE, CheckupKind.EXTERNAL_SECRET_FAILED, CheckupKind.SECRET_STORE_NOT_READY,
)

/** Kubernetes' own words about the finding, "" when it has none. */
val CheckupFinding.detail: String get() = listOf(reason.takeIf { kind in REASON_KINDS }.orEmpty(), message)
    .filter { it.isNotEmpty() }
    .joinToString(": ")
