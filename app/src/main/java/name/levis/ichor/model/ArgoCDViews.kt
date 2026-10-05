package name.levis.ichor.model

// What the Argo CD screens derive from ArgoStatus: filters, groups, sync-wave steps, the likely
// cause of a problem and the apps an inventory tile runs. Pure, so it is unit-tested.

/** The chips above the apps list; [ALL] has none. */
enum class ArgoFilter {
    ALL,
    /** Critical: a failed sync, Degraded or Missing. */
    ATTENTION,
    OUT_OF_SYNC,
    PROGRESSING,
    SYNCING,
    AUTO_SYNC_OFF,
    /** An active deny sync window stops its automated syncs. */
    FROZEN,
    ;

    fun matches(app: ArgoApp): Boolean = when (this) {
        ALL -> true
        ATTENTION -> app.serviceHealth == ServiceHealth.CRITICAL
        OUT_OF_SYNC -> app.outOfSync
        PROGRESSING -> app.healthState == ArgoHealth.PROGRESSING
        SYNCING -> app.isRunning
        AUTO_SYNC_OFF -> !app.autoSync.enabled
        FROZEN -> app.freeze != null
    }
}

enum class ArgoGroupBy { NONE, PROJECT, APP_SET, NAMESPACE }

/** Worst level first, then by name. */
val ArgoStatus.sortedApps: List<ArgoApp>
    get() = apps.sortedWith(compareBy<ArgoApp> { it.serviceHealth.ordinal }.thenBy { it.name })

/** How many apps each chip would show. */
fun List<ArgoApp>.filterCounts(): Map<ArgoFilter, Int> = ArgoFilter.entries.associateWith { f -> count(f::matches) }

/** Apps of [filter] whose name, project, namespaces or chart contain [query] (case-insensitive). */
fun List<ArgoApp>.filtered(filter: ArgoFilter, query: String): List<ArgoApp> {
    val q = query.trim()
    return filter { a ->
        filter.matches(a) && (q.isEmpty() || listOf(a.name, a.project, a.namespace, a.destination.namespace, a.versionLabel).any { it.contains(q, ignoreCase = true) })
    }
}

/**
 * Apps in groups named by project, owning ApplicationSet or destination namespace, sorted by
 * name with the unnamed group ("": no ApplicationSet) last; one unnamed group for [ArgoGroupBy.NONE].
 */
fun List<ArgoApp>.grouped(by: ArgoGroupBy): List<Pair<String, List<ArgoApp>>> {
    if (by == ArgoGroupBy.NONE) return listOf("" to this)
    return groupBy { a ->
        when (by) {
            ArgoGroupBy.PROJECT -> a.project
            ArgoGroupBy.APP_SET -> a.owner?.takeIf { it.isAppSet }?.name.orEmpty()
            ArgoGroupBy.NAMESPACE -> a.destination.namespace
            ArgoGroupBy.NONE -> ""
        }
    }.toList().sortedWith(compareBy<Pair<String, List<ArgoApp>>> { it.first.isEmpty() }.thenBy { it.first })
}

/** What "Sync all" syncs: OutOfSync apps without a sync running already. */
fun List<ArgoApp>.syncAllCandidates(): List<ArgoApp> = filter { it.outOfSync && !it.isRunning }

/** How many apps are at each level, for the health bar. */
fun List<ArgoApp>.levelCounts(): Map<ServiceHealth, Int> = groupingBy { it.serviceHealth }.eachCount()

/** Apps with a sync running, for the overview card. */
val ArgoStatus.runningSyncs: List<ArgoApp> get() = sortedApps.filter { it.isRunning }

/** Apps that need a look and are not just syncing: critical first. */
val ArgoStatus.problemApps: List<ArgoApp>
    get() = sortedApps.filter { it.serviceHealth.needsAttention && !it.isRunning }

/** Nothing needs a look, nothing is syncing. */
val ArgoStatus.allFine: Boolean get() = apps.none { it.serviceHealth.needsAttention || it.isRunning }

enum class WaveState { DONE, CURRENT, PENDING, FAILED }

/** One step of the sync-waves timeline: a wave, its resources (hooks included) and where it stands. */
data class WaveStep(val wave: Int, val resources: List<ArgoResource>, val state: WaveState)

private val SYNC_DONE = setOf("Synced", "Pruned", "PruneSkipped")

/**
 * The app's resources grouped by sync wave, lowest first. While a sync runs, waves before its
 * current one are done, that one is current and later ones pending; otherwise a wave is done
 * when every resource is Synced. A wave with a resource whose sync failed is failed.
 */
fun ArgoApp.waveSteps(): List<WaveStep> {
    val op = operation
    val failedKeys = op?.failed.orEmpty().map { "${it.kind}/${it.namespace}/${it.name}" }.toSet()
    return resources.groupBy { it.wave }.toSortedMap().map { (wave, list) ->
        val failed = list.any { it.syncResult == "SyncFailed" || "${it.kind}/${it.namespace}/${it.name}" in failedKeys }
        val state = when {
            failed -> WaveState.FAILED
            op != null && op.isRunning -> when {
                wave < op.wave -> WaveState.DONE
                wave == op.wave -> WaveState.CURRENT
                else -> WaveState.PENDING
            }
            list.filter { !it.hook }.all { it.sync == "Synced" || it.syncResult in SYNC_DONE && it.sync != "OutOfSync" } -> WaveState.DONE
            else -> WaveState.PENDING
        }
        WaveStep(wave, list, state)
    }
}

/** Why an app is unhealthy, the most telling first. */
sealed interface ArgoCause {
    /** A pod of the app sits on a node Talos reports not ready. */
    data class NodeDown(val node: String, val pod: String) : ArgoCause
    /** A pod's own status, such as CrashLoopBackOff or Pending. */
    data class PodStatus(val pod: String, val status: String) : ArgoCause
    /** Argo CD's health message, or the first failed resource's or condition's message. */
    data class Message(val text: String) : ArgoCause
}

/**
 * The likely cause of the app's problem: an unhealthy pod on one of [downNodes] (the D8 join
 * with Talos), else an unhealthy pod's status, else the health message, a failed resource's
 * message or a condition. Null when nothing explains it.
 */
fun ArgoApp.likelyCause(downNodes: Set<String>): ArgoCause? {
    // The pods are those of its namespace: they only explain an app whose own health is bad,
    // not a healthy one whose last sync failed.
    val pods = if (health != "Healthy" && health != "Suspended") unhealthyPods else emptyList()
    pods.firstOrNull { it.node.isNotEmpty() && it.node in downNodes }?.let { return ArgoCause.NodeDown(it.node, it.name) }
    pods.firstOrNull { it.status.isNotEmpty() }?.let { return ArgoCause.PodStatus(it.name, it.status) }
    val text = healthMessage.ifBlank { operation?.failed?.firstOrNull()?.message.orEmpty() }
        .ifBlank { conditions.firstOrNull()?.message.orEmpty() }
    return text.takeIf { it.isNotBlank() }?.let(ArgoCause::Message)
}

/**
 * The Argo CD Applications that deploy the inventory app [inv], worst first. The first rule
 * that finds any wins: the same catalog icon, then the same name, then (for an app that is
 * not Kubernetes or Talos plumbing) a destination namespace the app runs in.
 */
fun argoAppsFor(inv: InventoryApp, status: ArgoStatus): List<ArgoApp> {
    val apps = status.sortedApps
    val byIcon = apps.filter { it.icon.isNotEmpty() && (it.icon == inv.id || it.icon == inv.icon) }
    if (byIcon.isNotEmpty()) return byIcon
    val byName = apps.filter { it.name == inv.id || it.name == inv.name.lowercase() }
    if (byName.isNotEmpty()) return byName
    if (inv.system) return emptyList()
    return apps.filter { it.destination.namespace.isNotEmpty() && it.destination.namespace in inv.namespaces }
}

/**
 * The level to badge an inventory tile with: the worst of its Argo CD apps when one is critical
 * or OutOfSync; none (absent) otherwise. Keyed by inventory app id.
 */
fun ArgoStatus.inventoryBadges(inventory: List<InventoryApp>): Map<String, ServiceHealth> =
    inventory.mapNotNull { inv ->
        val matched = argoAppsFor(inv, this)
        val worst = matched.firstOrNull() ?: return@mapNotNull null
        when {
            worst.serviceHealth == ServiceHealth.CRITICAL -> inv.id to ServiceHealth.CRITICAL
            matched.any { it.outOfSync } -> inv.id to ServiceHealth.WARNING
            else -> null
        }
    }.toMap()
