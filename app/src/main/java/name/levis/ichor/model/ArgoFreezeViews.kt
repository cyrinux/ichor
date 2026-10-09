package name.levis.ichor.model

import java.time.Instant
import java.time.ZoneId

// What the freeze screens derive from ArgoStatus: who a freeze would stop, the windows screen's
// sections, the reminders to schedule. Pure, so it is unit-tested. The design is in
// the Linear plan document "D9. Argo CD freeze: hotfix live without being reverted".

/** How much of its project a freeze around an app covers. */
enum class FreezeScope { APP, NAMESPACE, PROJECT }

/** The durations the freeze sheet offers, in minutes (plus "until 09:00"). */
val FREEZE_DURATIONS = listOf(15, 60, 240)
const val DEFAULT_FREEZE_MINUTES = 60
const val FREEZE_MIN_MINUTES = 5
const val FREEZE_MAX_MINUTES = 7 * 24 * 60

/** What "+1 h" adds to a running freeze. */
const val FREEZE_EXTEND_MINUTES = 60

/** The project [app] belongs to: the one with its name, in the app's namespace first. */
fun ArgoStatus.projectOf(app: ArgoApp): ArgoProject? =
    projects.filter { it.name == app.project }.let { same -> same.firstOrNull { it.namespace == app.namespace } ?: same.firstOrNull() }

/** A namespace freeze needs the app to deploy to one. */
fun FreezeScope.availableFor(app: ArgoApp): Boolean = this != FreezeScope.NAMESPACE || app.destination.namespace.isNotEmpty()

/** The apps a freeze of [scope] around [app] stops, by name: its project's, matched as Argo CD would. */
fun ArgoStatus.freezeTargets(app: ArgoApp, scope: FreezeScope): List<ArgoApp> = apps.filter {
    it.project == app.project && when (scope) {
        FreezeScope.APP -> it.name == app.name
        FreezeScope.NAMESPACE -> it.destination.namespace == app.destination.namespace
        FreezeScope.PROJECT -> true
    }
}.sortedBy { it.name }

/** The options KubeArgoFreeze takes for a freeze of [scope] around [app]. */
fun freezeOptions(app: ArgoApp, scope: FreezeScope, minutes: Int, manualSync: Boolean, reason: String) = ArgoFreezeOptions(
    applications = when (scope) {
        FreezeScope.APP -> listOf(app.name)
        FreezeScope.PROJECT -> listOf("*")
        FreezeScope.NAMESPACE -> emptyList()
    },
    namespaces = if (scope == FreezeScope.NAMESPACE) listOf(app.destination.namespace) else emptyList(),
    minutes = minutes,
    manualSync = manualSync,
    reason = reason.trim(),
)

/** Minutes from [now] to the next [hour]:00 in [zone], within what a freeze may last. */
fun minutesUntil(hour: Int, now: Long, zone: ZoneId): Int {
    val start = Instant.ofEpochMilli(now).atZone(zone)
    var target = start.toLocalDate().atTime(hour, 0).atZone(zone)
    if (!target.isAfter(start.plusMinutes(FREEZE_MIN_MINUTES.toLong()))) target = target.plusDays(1)
    val minutes = (target.toInstant().toEpochMilli() - now + 59_999) / 60_000
    return minutes.toInt().coerceIn(FREEZE_MIN_MINUTES, FREEZE_MAX_MINUTES)
}

/** A window with its project, for the windows screen, the banners and the reminders. */
data class ProjectWindow(val project: ArgoProject, val window: ArgoWindow) {
    val key: String get() = "${project.key}/${window.id}"
}

enum class WindowSection { ACTIVE, UPCOMING, EXPIRED }

private val ArgoStatus.allWindows: List<ProjectWindow>
    get() = projects.flatMap { p -> p.windows.map { ProjectWindow(p, it) } }

/**
 * The windows screen: active windows ending first, then the others by next start (unreadable
 * ones last), then Ichor's ended freezes. An ended freeze Argo CD fires again a year later is
 * listed as ended: the app removes it.
 */
fun ArgoStatus.windowSections(): Map<WindowSection, List<ProjectWindow>> {
    val (expired, live) = allWindows.partition { it.window.ichor?.expired == true }
    val (active, upcoming) = live.partition { it.window.active }
    return mapOf(
        WindowSection.ACTIVE to active.sortedBy { it.window.endsAt },
        WindowSection.UPCOMING to upcoming.sortedWith(compareBy<ProjectWindow> { it.window.start == 0L }.thenBy { it.window.start }),
        WindowSection.EXPIRED to expired.sortedByDescending { it.window.endsAt },
    )
}

/** The deny windows freezing apps now (Ichor's ended ones aside), ending first. */
val ArgoStatus.activeFreezes: List<ProjectWindow>
    get() = windowSections().getValue(WindowSection.ACTIVE).filter { it.window.isDeny }

/** Ichor's freezes still running, for the reminders before they end. */
val ArgoStatus.runningIchorFreezes: List<ProjectWindow>
    get() = activeFreezes.filter { it.window.ichor != null }

/** Projects holding an ended Ichor freeze: the app clears them before they come back next year. */
val ArgoStatus.projectsToClear: List<ArgoProject>
    get() = projects.filter { p -> p.windows.any { it.ichor?.expired == true } }

/** The windows freezing [app], with its project. */
fun ArgoStatus.freezeWindowsOf(app: ArgoApp): List<ProjectWindow> {
    val freeze = app.freeze ?: return emptyList()
    val project = projectOf(app) ?: return emptyList()
    return project.windows.filter { it.id in freeze.windows }.map { ProjectWindow(project, it) }
}

/** What Argo CD puts back as Git has it once the freeze ends: the hand-made changes. */
val ArgoApp.drifted: List<ArgoResource> get() = resources.filter { it.sync == "OutOfSync" }

/**
 * The app that would undo a hand-made change to [kind] [namespace]/[name] within minutes: it
 * deploys that resource with auto-sync and self-heal on, and is not frozen.
 */
fun ArgoStatus.selfHealingOwner(kind: String, namespace: String, name: String): ArgoApp? = apps.firstOrNull { a ->
    a.autoSync.enabled && a.autoSync.selfHeal && a.freeze == null &&
        a.resources.any { it.kind == kind && it.namespace == namespace && it.name == name }
}
