package name.levis.ichor.ui.search

import name.levis.ichor.model.RecentTarget

/** What the search shows before anything is typed. */
internal enum class BlankBody {
    /** No cluster was ever read: open one first. */
    NOTHING_KNOWN,

    /** Clusters are known but nothing was searched or opened yet: what typing does, and screens to open. */
    FIRST_USE,

    /** The recently opened objects and recent searches. */
    RECENTS,
}

/** Recents win even over an empty snapshot: a recently opened object can still be opened. */
internal fun blankBody(snapshotEmpty: Boolean, recent: List<String>, opened: List<RecentTarget>): BlankBody = when {
    recent.isNotEmpty() || opened.isNotEmpty() -> BlankBody.RECENTS
    snapshotEmpty -> BlankBody.NOTHING_KNOWN
    else -> BlankBody.FIRST_USE
}

/** The cluster screens offered first, most used first; node and settings screens never are. */
internal val FIRST_USE_SHORTCUT_IDS = listOf("nodes", "workloads", "health", "alerts", "argocd", "flux", "etcd", "events")

/** The first [max] of [specs] in [FIRST_USE_SHORTCUT_IDS] order: only what the role's catalog allows. */
internal fun firstUseShortcuts(specs: List<ScreenSpec>, max: Int = 4): List<ScreenSpec> {
    val byId = specs.associateBy { it.id }
    return FIRST_USE_SHORTCUT_IDS.mapNotNull(byId::get).take(max)
}
