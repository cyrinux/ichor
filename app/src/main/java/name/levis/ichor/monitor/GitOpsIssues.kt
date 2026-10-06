package name.levis.ichor.monitor

import name.levis.ichor.model.ArgoHealth
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.ServiceHealth

/** Why a GitOps app alerts; stored after the severity ("critical|failed"), so the text can say it. */
const val GITOPS_ARGO_FAILED = "failed"
const val GITOPS_ARGO_DEGRADED = "degraded"
const val GITOPS_ARGO_MISSING = "missing"
const val GITOPS_ARGO_OUT_OF_SYNC = "outOfSync"
const val GITOPS_ARGO_ERROR = "error"
const val GITOPS_FLUX_NOT_READY = "notReady"

/** Key prefixes ("argocd|namespace/name", "flux|Kind namespace/name"). */
const val GITOPS_ARGO = "argocd"
const val GITOPS_FLUX = "flux"

/** Argo CD conditions that mean it cannot render or apply the app. */
private val ARGO_ERROR_CONDITIONS = setOf("ComparisonError", "InvalidSpecError", "SyncError")

/** A GitOps issue's stored value: "severity|reason". */
fun gitopsValue(severity: String, reason: String): String = "$severity|$reason"

fun gitopsSeverity(value: String): String = value.substringBefore('|')

/** A GitOps alert's "tool|severity|reason" detail, split; a missing part is "". */
data class GitOpsDetail(val tool: String, val severity: String, val reason: String) {
    companion object {
        fun parse(detail: String): GitOpsDetail {
            val parts = detail.split('|')
            return GitOpsDetail(parts.getOrElse(0) { "" }, parts.getOrElse(1) { "" }, parts.getOrElse(2) { "" })
        }
    }
}

/**
 * The Argo CD and Flux apps worth a notification, keyed "argocd|namespace/name" or
 * "flux|Kind namespace/name", valued "severity|reason" (see [gitopsValue]).
 *
 * Argo CD: critical when its last sync failed (Failed or Error), or it is Degraded or Missing;
 * a warning when OutOfSync with auto-sync on (it should have converged), or on a
 * ComparisonError, InvalidSpecError or SyncError condition. Progressing, Suspended, or OutOfSync
 * with auto-sync off (the user's choice) are not issues.
 *
 * Flux: critical when the app is (Ready=False, Stalled, failing); a suspended app never alerts,
 * nor does Ready=Unknown while it reconciles.
 */
fun gitopsIssuesOf(argo: ArgoStatus?, flux: FluxStatus?): Map<String, String> {
    val out = sortedMapOf<String, String>()
    argo?.takeIf { it.installed }?.apps.orEmpty().forEach { app ->
        val reason = when {
            app.operation?.isFailed == true -> GITOPS_ARGO_FAILED to DATA_CRITICAL
            app.healthState == ArgoHealth.DEGRADED -> GITOPS_ARGO_DEGRADED to DATA_CRITICAL
            app.healthState == ArgoHealth.MISSING -> GITOPS_ARGO_MISSING to DATA_CRITICAL
            app.conditions.any { it.type in ARGO_ERROR_CONDITIONS } -> GITOPS_ARGO_ERROR to DATA_WARNING
            app.outOfSync && app.autoSync.enabled -> GITOPS_ARGO_OUT_OF_SYNC to DATA_WARNING
            else -> null
        } ?: return@forEach
        out["$GITOPS_ARGO|${app.key}"] = gitopsValue(reason.second, reason.first)
    }
    flux?.takeIf { it.installed }?.apps.orEmpty().forEach { app ->
        val reconcilingUnknown = app.ready == "Unknown" && app.isBusy
        if (app.suspended || reconcilingUnknown || app.serviceHealth != ServiceHealth.CRITICAL) return@forEach
        out["$GITOPS_FLUX|${app.kind} ${app.namespace}/${app.name}"] = gitopsValue(DATA_CRITICAL, GITOPS_FLUX_NOT_READY)
    }
    return out
}

/**
 * [gitopsIssuesOf] for a check where a part could not be read ([argo] or [flux] null, or Flux's
 * HelmReleases unlisted): that part keeps the issues [known] had for it, so it neither clears
 * them falsely nor stops the other part from alerting. Null when nothing could be read.
 */
fun gitopsIssuesWithGaps(argo: ArgoStatus?, flux: FluxStatus?, known: Map<String, String>): Map<String, String>? {
    if (argo == null && flux == null) return null
    val unread = buildList {
        if (argo == null) add("$GITOPS_ARGO|")
        if (flux == null) add("$GITOPS_FLUX|")
        if (flux != null && flux.helmError.isNotEmpty()) add("$GITOPS_FLUX|HelmRelease ")
    }
    return gitopsIssuesOf(argo, flux) + known.filterKeys { key -> unread.any { key.startsWith(it) } }
}

/**
 * The GitOps issues a partial read may carry over: [previous]'s, only when it is the same cluster
 * ([context]) and that check watched and read them. Another cluster's issues must never leak in.
 */
fun knownGitOpsIssues(previous: ClusterSnapshot?, context: String): Map<String, String> =
    previous?.takeIf { it.context == context && it.gitopsWatched && it.gitopsChecked }?.gitopsIssues.orEmpty()
