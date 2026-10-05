package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_argocd.go, kube_argocd_actions.go and kube_argocd_syncwindows.go
// (the design is in plans/argocd/README.md and plans/roadmap/devops/09-argocd-freeze.md). Argo CD terms (Synced, OutOfSync, Healthy...) are its own names.

/** The Argo CD Applications of every namespace, with their ApplicationSets and projects. */
@Serializable
data class ArgoStatus(
    /** False when the cluster serves no argoproj.io Applications. */
    val installed: Boolean = false,
    /** The application controller's image tag, "" when unknown. */
    val version: String = "",
    /** Listing the ApplicationSets failed; the apps still show. */
    val appSetsError: String = "",
    val apps: List<ArgoApp> = emptyList(),
    val appSets: List<ArgoAppSet> = emptyList(),
    val projects: List<ArgoProject> = emptyList(),
)

@Serializable
data class ArgoApp(
    val namespace: String = "",
    val name: String,
    val project: String = "",
    /** The ApplicationSet or parent app that writes its spec; null when none. */
    val owner: ArgoOwner? = null,
    /** critical, warning, ok or idle (suspended): health, sync and the last operation summed up. */
    val level: String = "",
    /** Bundled catalog icon id, "" when none. */
    val icon: String = "",
    /** Dashboard Icons slug, "" when none. */
    val remoteIcon: String = "",
    /** Healthy, Progressing, Degraded, Suspended, Missing or Unknown. */
    val health: String = "",
    val healthMessage: String = "",
    /** Synced, OutOfSync or Unknown. */
    val sync: String = "",
    val revision: String = "",
    /** The pending refresh: normal, hard or "". */
    val refreshing: String = "",
    val sources: List<ArgoSource> = emptyList(),
    val destination: ArgoDestination = ArgoDestination(),
    val autoSync: ArgoAutoSync = ArgoAutoSync(),
    val syncOptions: List<String> = emptyList(),
    /** The running or last sync; null when none ran since the app was created. */
    val operation: ArgoOperation? = null,
    val conditions: List<ArgoCondition> = emptyList(),
    /** By sync wave. */
    val resources: List<ArgoResource> = emptyList(),
    /** Newest first. */
    val history: List<ArgoHistory> = emptyList(),
    val images: List<String> = emptyList(),
    val externalURLs: List<String> = emptyList(),
    /** Pods of its destination namespace that are not ready, only next to an unhealthy app. */
    val unhealthyPods: List<KubePod> = emptyList(),
    /** Unix millis. */
    val reconciledAt: Long = 0,
    /** Set while an active deny sync window of its project stops its automated syncs. */
    val freeze: ArgoFreeze? = null,
) {
    val key: String get() = "$namespace/$name"
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(level)
    val healthState: ArgoHealth get() = ArgoHealth.from(health)
    val syncState: ArgoSync get() = ArgoSync.from(sync)
    val outOfSync: Boolean get() = syncState == ArgoSync.OUT_OF_SYNC

    /** A sync is running or being terminated: another one would be refused. */
    val isRunning: Boolean get() = operation?.isRunning == true

    /** Spec changes (auto-sync, rollback) stick only on an app no ApplicationSet or parent rewrites. */
    val canChangeSpec: Boolean get() = owner == null

    /** Rolling back needs auto-sync paused, or Argo CD syncs straight back to the latest revision. */
    val canRollback: Boolean get() = canChangeSpec && !autoSync.enabled && !isRunning

    /** Defaults the sync sheet's server-side apply to the app's own option. */
    val serverSideApply: Boolean get() = syncOptions.any { it.equals("ServerSideApply=true", ignoreCase = true) }

    /** What a sync with prune would delete. */
    val pruneCandidates: List<ArgoResource> get() = resources.filter { it.prune }

    /** "chart@8.6.0" for a Helm chart, else the target revision or the short synced revision. */
    val versionLabel: String
        get() {
            val src = sources.firstOrNull()
            return when {
                src != null && src.chart.isNotEmpty() -> "${src.chart}@${src.targetRevision.ifEmpty { revision }}"
                revision.isNotEmpty() -> shortRevision(revision)
                else -> src?.targetRevision.orEmpty()
            }
        }

    /** When the current revision was deployed (unix millis), 0 when unknown. */
    val deployedAt: Long get() = history.firstOrNull()?.deployedAt ?: 0
}

@Serializable
data class ArgoOwner(
    /** ApplicationSet or Application. */
    val kind: String = "",
    val name: String = "",
) {
    val isAppSet: Boolean get() = kind == "ApplicationSet"
}

@Serializable
data class ArgoSource(
    val repo: String = "",
    val path: String = "",
    val chart: String = "",
    val targetRevision: String = "",
)

@Serializable
data class ArgoDestination(
    val server: String = "",
    val name: String = "",
    val namespace: String = "",
)

@Serializable
data class ArgoAutoSync(
    val enabled: Boolean = false,
    val prune: Boolean = false,
    val selfHeal: Boolean = false,
)

@Serializable
data class ArgoOperation(
    /** Running, Terminating, Succeeded, Failed or Error. */
    val phase: String = "",
    val message: String = "",
    /** Unix millis, 0 when unknown. */
    val startedAt: Long = 0,
    val finishedAt: Long = 0,
    /** A user name, or "automated". */
    val initiatedBy: String = "",
    val revision: String = "",
    val retryCount: Int = 0,
    val dryRun: Boolean = false,
    /** Resources synced so far, out of [total] (hooks included once run). */
    val done: Int = 0,
    val total: Int = 0,
    /** The lowest wave with resources not synced yet; [waves] every wave of the app. */
    val wave: Int = 0,
    val waves: List<Int> = emptyList(),
    val failed: List<ArgoResult> = emptyList(),
) {
    val isRunning: Boolean get() = phase == "Running" || phase == "Terminating"
    val isFailed: Boolean get() = phase == "Failed" || phase == "Error"
    val progress: Float get() = if (total > 0) (done.toFloat() / total).coerceIn(0f, 1f) else 0f

    /** 1-based position of the current wave among [waves], 0 when there is none. */
    val waveIndex: Int get() = waves.indexOf(wave) + 1
}

@Serializable
data class ArgoResult(
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val message: String = "",
)

@Serializable
data class ArgoCondition(val type: String = "", val message: String = "")

@Serializable
data class ArgoResource(
    val group: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val sync: String = "",
    /** "" for kinds without a health check, and by default on Argo CD 3. */
    val health: String = "",
    val healthMessage: String = "",
    val wave: Int = 0,
    val hook: Boolean = false,
    /** Would be deleted by a sync with prune. */
    val prune: Boolean = false,
    /** The last operation's word on it: Synced, SyncFailed, Pruned, PruneSkipped or "". */
    val syncResult: String = "",
) {
    val key: String get() = "$group/$kind/$namespace/$name"
    val ref: ArgoResourceRef get() = ArgoResourceRef(group, kind, namespace, name)

    /** A Deployment, StatefulSet or DaemonSet: one a rollout restart applies to. */
    val restartable: Boolean get() = group == "apps" && kind in RESTARTABLE_KINDS
}

private val RESTARTABLE_KINDS = setOf("Deployment", "StatefulSet", "DaemonSet")

@Serializable
data class ArgoHistory(
    val id: Long = 0,
    val revision: String = "",
    val targetRevision: String = "",
    val chart: String = "",
    /** Unix millis. */
    val deployedAt: Long = 0,
    val initiatedBy: String = "",
)

@Serializable
data class ArgoAppSet(
    val namespace: String = "",
    val name: String,
    /** Worst level of its apps, critical on an error condition. */
    val level: String = "",
    val apps: Int = 0,
    val conditions: List<ArgoAppSetCondition> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(level)
}

@Serializable
data class ArgoAppSetCondition(val type: String = "", val status: String = "", val message: String = "")

@Serializable
data class ArgoProject(
    val namespace: String = "",
    val name: String,
    val description: String = "",
    val syncWindows: Int = 0,
    val windows: List<ArgoWindow> = emptyList(),
    /** The Argo CD app that applies the project from Git, "" when none. */
    val managedBy: String = "",
    /** [managedBy] applies it server-side: a freeze was not checked to survive its syncs. */
    val managedServerSide: Boolean = false,
) {
    val key: String get() = "$namespace/$name"
}

/** One sync window of a project, evaluated at read time. */
@Serializable
data class ArgoWindow(
    /** A hash of its content: windows have no name. */
    val id: String = "",
    /** allow or deny. */
    val kind: String = "",
    val schedule: String = "",
    val duration: String = "",
    val timeZone: String = "",
    val applications: List<String> = emptyList(),
    val namespaces: List<String> = emptyList(),
    val clusters: List<String> = emptyList(),
    val manualSync: Boolean = false,
    val active: Boolean = false,
    /** The current occurrence when active, else the next one (unix millis); 0 when none. */
    val start: Long = 0,
    val end: Long = 0,
    /** Why the window cannot be read, "" when it can. */
    val error: String = "",
    /** Apps of the project it matches. */
    val apps: Int = 0,
    /** Set on a freeze Ichor created. */
    val ichor: ArgoFreezeInfo? = null,
) {
    val isDeny: Boolean get() = kind == "deny"

    /** When it ends for good: an Ichor freeze's expiry, else the current occurrence's end. */
    val endsAt: Long get() = ichor?.expiresAt ?: end
}

@Serializable
data class ArgoFreezeInfo(
    val reason: String = "",
    /** Unix millis. */
    val createdAt: Long = 0,
    val expiresAt: Long = 0,
    /** Over: Argo CD would only fire it again a year later. */
    val expired: Boolean = false,
)

/** An app's active deny windows summed up. */
@Serializable
data class ArgoFreeze(
    val project: String = "",
    /** The latest end among them, unix millis. */
    val until: Long = 0,
    /** Every one of them allows manual syncs. */
    val manualSync: Boolean = false,
    /** Every one of them is an Ichor freeze. */
    val byIchor: Boolean = false,
    /** Their [ArgoWindow.id]s. */
    val windows: List<String> = emptyList(),
)

/** The freeze sheet's choices, as KubeArgoFreeze reads them. */
@Serializable
data class ArgoFreezeOptions(
    val applications: List<String> = emptyList(),
    val namespaces: List<String> = emptyList(),
    val minutes: Int = 0,
    val manualSync: Boolean = false,
    val reason: String = "",
    /** The [ArgoWindow.id] to extend or unfreeze. */
    val window: String = "",
    /** Confirms removing a window Ichor did not create. */
    val fromGit: Boolean = false,
)

/** What KubeArgoFreeze runs, by its wire name. */
enum class ArgoFreezeAction(val wire: String) {
    FREEZE("freeze"),
    EXTEND("extend"),
    UNFREEZE("unfreeze"),
    CLEAR_EXPIRED("clearExpired"),
}

/** The sync sheet's choices (and a rollback's target), as KubeArgoAction reads them. */
@Serializable
data class ArgoSyncOptions(
    val prune: Boolean = false,
    val dryRun: Boolean = false,
    val force: Boolean = false,
    val applyOutOfSyncOnly: Boolean = false,
    val serverSideApply: Boolean = false,
    val replace: Boolean = false,
    /** Empty: every resource. */
    val resources: List<ArgoResourceRef> = emptyList(),
    /** Rollback only. */
    val historyId: Long = 0,
)

@Serializable
data class ArgoResourceRef(val group: String = "", val kind: String = "", val namespace: String = "", val name: String = "")

/** What KubeArgoAction runs, by its wire name. */
enum class ArgoAction(val wire: String) {
    REFRESH("refresh"),
    HARD_REFRESH("hardRefresh"),
    SYNC("sync"),
    TERMINATE("terminate"),
    AUTO_SYNC_ON("autoSyncOn"),
    AUTO_SYNC_OFF("autoSyncOff"),
    ROLLBACK("rollback"),
}

enum class ArgoHealth(val wire: String) {
    HEALTHY("Healthy"),
    PROGRESSING("Progressing"),
    DEGRADED("Degraded"),
    SUSPENDED("Suspended"),
    MISSING("Missing"),
    UNKNOWN("Unknown"),
    ;

    companion object {
        fun from(wire: String): ArgoHealth = entries.firstOrNull { it.wire == wire } ?: UNKNOWN
    }
}

enum class ArgoSync(val wire: String) {
    SYNCED("Synced"),
    OUT_OF_SYNC("OutOfSync"),
    UNKNOWN("Unknown"),
    ;

    companion object {
        fun from(wire: String): ArgoSync = entries.firstOrNull { it.wire == wire } ?: UNKNOWN
    }
}

/** A Git SHA cut to 7 characters; versions and tags stay whole. */
fun shortRevision(revision: String): String =
    if (revision.length >= 40 && revision.all { it.isDigit() || it in 'a'..'f' }) revision.take(7) else revision

/** The inventory's catalog id for Argo CD. */
const val ARGO_CD_CATALOG_ID = "argo-cd"

/** Whether the inventory saw Argo CD running: only then is the cluster asked for Applications. */
val Inventory.hasArgoCD: Boolean get() = apps.any { it.id == ARGO_CD_CATALOG_ID }
