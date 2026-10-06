package name.levis.ichor.model

// What the Flux screens derive from FluxStatus: filters, counts and the apps that need a look.
// Pure, so it is unit-tested.

/** The chips above the apps list; [ALL] has none. */
enum class FluxFilter {
    ALL,
    FAILING,
    RECONCILING,
    SUSPENDED,
    KUSTOMIZATIONS,
    HELM_RELEASES,
    ;

    fun matches(app: FluxApp): Boolean = when (this) {
        ALL -> true
        FAILING -> app.state == FluxState.FAILING
        RECONCILING -> app.state == FluxState.RECONCILING
        SUSPENDED -> app.state == FluxState.SUSPENDED
        KUSTOMIZATIONS -> app.isKustomization
        HELM_RELEASES -> app.isHelmRelease
    }
}

/** Worst level first, then by name and kind. */
val FluxStatus.sortedApps: List<FluxApp>
    get() = apps.sortedWith(compareBy<FluxApp> { it.serviceHealth.ordinal }.thenBy { it.name }.thenBy { it.namespace }.thenBy { it.kind })

/** Worst level first, then by name. */
val FluxStatus.sortedSources: List<FluxSource>
    get() = sources.sortedWith(compareBy<FluxSource> { it.serviceHealth.ordinal }.thenBy { it.name }.thenBy { it.namespace }.thenBy { it.kind })

/** How many apps each chip would show. */
fun List<FluxApp>.fluxFilterCounts(): Map<FluxFilter, Int> = FluxFilter.entries.associateWith { f -> count(f::matches) }

/** Apps of [filter] whose name, namespace, chart, path or source contain [query] (case-insensitive). */
fun List<FluxApp>.fluxFiltered(filter: FluxFilter, query: String): List<FluxApp> {
    val q = query.trim()
    return filter { a ->
        filter.matches(a) &&
            (q.isEmpty() || listOf(a.name, a.namespace, a.chart, a.path, a.source?.name.orEmpty(), a.targetNamespace).any { it.contains(q, ignoreCase = true) })
    }
}

/** How many apps are in each state, for the overview card's bar and counts. */
fun List<FluxApp>.stateCounts(): Map<FluxState, Int> = groupingBy { it.state }.eachCount()

/** Apps that fail, worst first: the overview card names a couple. */
val FluxStatus.failingApps: List<FluxApp> get() = sortedApps.filter { it.state == FluxState.FAILING }

/** A controller is at work, or a requested reconcile is not handled yet: worth polling. */
val FluxStatus.anyBusy: Boolean get() = apps.any { it.isBusy } || sources.any { it.isBusy }

/** Nothing fails and nothing reconciles (suspended ones are the user's choice). */
val FluxStatus.allFine: Boolean get() = apps.none { it.state == FluxState.FAILING || it.state == FluxState.RECONCILING }
