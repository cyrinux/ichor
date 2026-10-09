package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_browser.go and kube_edit.go: any kind the API server serves, listed
// with the columns of its server-side Table, and one object as YAML (plans/roadmap/kubeconfig-only.md §7).

/** A listable resource, as `kubectl api-resources` shows it. [group] "" for the core one. */
@Serializable
data class ApiResource(
    val group: String = "",
    val version: String = "",
    val resource: String = "",
    val kind: String = "",
    val namespaced: Boolean = false,
    val verbs: List<String> = emptyList(),
    val shortNames: List<String> = emptyList(),
    val categories: List<String> = emptyList(),
    /** It serves /scale (any CRD declaring it), or is a Job (its parallelism). */
    val scalable: Boolean = false,
) {
    val groupVersion: String get() = if (group.isEmpty()) version else "$group/$version"
    val key: String get() = "$group/$resource"

    /** The object screen offers to edit only what the server lets update. */
    val editable: Boolean get() = "update" in verbs
}

@Serializable
data class ApiResourceList(
    val resources: List<ApiResource> = emptyList(),
    /** Group versions discovery could not read (an aggregated API down). */
    val failed: List<String> = emptyList(),
)

/** The resources of one API group, under its name ("" for core). */
data class ResourceGroup(val group: String, val resources: List<ApiResource>)

/** Built-in groups shown first, in this order; other built-ins follow, then CRD groups. */
private val LEADING_GROUPS = listOf("", "apps", "batch", "networking.k8s.io")

/** Whether [group] ships with Kubernetes (no dot, or *.k8s.io) rather than with a CRD. */
fun isBuiltInGroup(group: String): Boolean = !group.contains('.') || group.endsWith(".k8s.io")

/**
 * [resources] matching [query] (kind, resource, short name or group, case-insensitive),
 * grouped by API group: core, apps, batch and networking first, the other built-in groups,
 * then the CRD groups, each sorted by name; kinds sorted within a group.
 */
fun groupResources(resources: List<ApiResource>, query: String): List<ResourceGroup> {
    val q = query.trim()
    val matching = if (q.isEmpty()) resources else resources.filter { it.matches(q) }
    return matching.groupBy { it.group }
        .map { (group, list) -> ResourceGroup(group, list.sortedWith(compareBy({ it.kind.lowercase() }, { it.resource }))) }
        .sortedWith(compareBy<ResourceGroup>({ groupRank(it.group) }, { it.group }))
}

private fun groupRank(group: String): Int {
    val leading = LEADING_GROUPS.indexOf(group)
    return when {
        leading >= 0 -> leading
        isBuiltInGroup(group) -> LEADING_GROUPS.size
        else -> LEADING_GROUPS.size + 1
    }
}

private fun ApiResource.matches(q: String): Boolean =
    kind.contains(q, ignoreCase = true) || resource.contains(q, ignoreCase = true) ||
        group.contains(q, ignoreCase = true) || shortNames.any { it.equals(q, ignoreCase = true) }

/** One column of the server's Table. Priority 0 is what `kubectl get` prints, higher what `-o wide` adds. */
@Serializable
data class ResourceColumn(val name: String = "", val priority: Int = 0, val type: String = "")

@Serializable
data class ResourcePageJson(
    val columns: List<ResourceColumn> = emptyList(),
    val rows: List<ResourceRowJson> = emptyList(),
    @SerialName("continue") val continueToken: String = "",
    val remaining: Long = -1,
) {
    /** Each row carries the page's columns (one shared list) so it can show itself. */
    fun toPage(): KubePage<ResourceRow> =
        KubePage(rows.map { it.toRow(columns) }, continueToken, remaining, complete = continueToken.isEmpty())
}

/**
 * The rows of one Table watch (StartKubeWatch): a SYNC page sets the columns, and each row
 * after it gets them, since the watch sends them with the list only. One per watch.
 */
class ResourceWatchRows {
    private var columns: List<ResourceColumn> = emptyList()

    fun page(page: ResourcePageJson): List<ResourceRow> {
        columns = page.columns
        return page.toPage().items
    }

    fun row(row: ResourceRowJson): ResourceRow = row.toRow(columns)
}

@Serializable
data class ResourceRowJson(
    val name: String = "",
    val namespace: String = "",
    val cells: List<String> = emptyList(),
    /** Unix seconds. */
    val created: Long = 0,
    val deleting: Boolean = false,
) {
    fun toRow(columns: List<ResourceColumn>) = ResourceRow(name, namespace, cells, created, deleting, columns)
}

/** One object of a resource list, with the Table's [columns] its [cells] follow. */
data class ResourceRow(
    val name: String,
    val namespace: String = "",
    val cells: List<String> = emptyList(),
    /** Unix seconds. */
    val created: Long = 0,
    val deleting: Boolean = false,
    val columns: List<ResourceColumn> = emptyList(),
) {
    val key: String get() = "$namespace/$name"
}

/** A cell worth showing on a row card: its column's name and the value. */
data class RowField(val label: String, val value: String)

/** Columns the card shows elsewhere: the name as its title, the namespace under it, the age from created. */
private val OWN_COLUMNS = setOf("name", "namespace", "age")

/**
 * The cells of [row] to show as "label: value": priority 0 columns, and the wide ones when
 * [wide]; without the name, namespace and age (shown apart) and empty or "<none>" values.
 */
fun rowFields(row: ResourceRow, wide: Boolean): List<RowField> =
    row.columns.mapIndexedNotNull { i, column ->
        val value = row.cells.getOrNull(i)?.trim().orEmpty()
        when {
            column.priority > 0 && !wide -> null
            column.name.lowercase() in OWN_COLUMNS -> null
            value.isEmpty() || value == "<none>" -> null
            else -> RowField(column.name, value)
        }
    }

/** Whether the Table has columns only `-o wide` shows: the toggle is offered then. */
fun hasWideColumns(columns: List<ResourceColumn>): Boolean = columns.any { it.priority > 0 }

/** [rows] whose name, namespace or a cell contains [query] (case-insensitive). */
fun List<ResourceRow>.filteredRows(query: String): List<ResourceRow> {
    val q = query.trim()
    if (q.isEmpty()) return this
    return filter { r -> r.name.contains(q, true) || r.namespace.contains(q, true) || r.cells.any { it.contains(q, true) } }
}

/** How a status-like cell reads at a glance. */
enum class CellTone { OK, WARN, BAD, NONE }

private val STATUS_COLUMNS = setOf("status", "phase", "state", "health", "sync status")
private val OK_VALUES = setOf("running", "ready", "bound", "active", "available", "succeeded", "completed", "true", "healthy", "synced", "deployed", "established")
private val WARN_VALUES = setOf("pending", "terminating", "containercreating", "progressing", "unknown", "released", "suspended", "outofsync")
private val BAD_VALUES = setOf("failed", "error", "crashloopbackoff", "imagepullbackoff", "errimagepull", "false", "lost", "degraded", "oomkilled", "evicted")

/** The tone of [value] in a status column ([column] Status, Phase, ...), NONE for other columns. */
fun cellTone(column: String, value: String): CellTone {
    if (column.lowercase() !in STATUS_COLUMNS) return CellTone.NONE
    val v = value.lowercase()
    return when {
        v in OK_VALUES -> CellTone.OK
        v in BAD_VALUES || v.startsWith("init:error") || v.startsWith("init:crash") -> CellTone.BAD
        v in WARN_VALUES || v.startsWith("init:") -> CellTone.WARN
        else -> CellTone.NONE
    }
}

/** What saving the edited YAML would change (KubeObjectUpdatePreview). */
@Serializable
data class KubeEditPreview(val changed: Boolean = false, val diff: String = "") {
    val lines: List<DiffLine> get() = parseDiffLines(diff)
}

/** What [propagation] does with what the deleted object owns (DeleteOptions.propagationPolicy). */
enum class DeletePropagation(val api: String) {
    /** The object goes now, the garbage collector deletes what it owned afterwards. */
    BACKGROUND("Background"),

    /** What the object owns is deleted first; the object waits for it. */
    FOREGROUND("Foreground"),

    /** What the object owns is kept, without an owner. */
    ORPHAN("Orphan"),
}

/** An object a deletion's propagation decides about: it is owned by the deleted one. */
@Serializable
data class KubeDeleteDependent(val kind: String = "", val namespace: String = "", val name: String = "")

/** What deleting an object would do (KubeObjectDeletePreview). */
@Serializable
data class KubeDeletePreview(
    @SerialName("protected") val isProtected: Boolean = false,
    /** Why it is protected: deleting it breaks the cluster or more than the object. */
    val reason: String = "",
    val clusterScoped: Boolean = false,
    val finalizers: List<String> = emptyList(),
    val dependents: List<KubeDeleteDependent> = emptyList(),
    val moreDependents: Int = 0,
    /** The version the preview read: the delete is refused if the object changed since. */
    val resourceVersion: String = "",
    /** A deletion is already pending, held by [finalizers]. */
    val deleting: Boolean = false,
) {
    /** A cluster-scoped or protected object is deleted only once its name is typed. */
    val needsTypedName: Boolean get() = isProtected || clusterScoped
}

/** One object the browser opens: its resource and name ([namespace] "" for a cluster-scoped one). */
data class KubeObjectRef(
    val group: String,
    val version: String,
    val resource: String,
    val kind: String,
    val namespace: String,
    val name: String,
    val editable: Boolean,
    /** The object screen offers Scale: see [ApiResource.scalable]. */
    val scalable: Boolean = false,
) {
    val isSecret: Boolean get() = group.isEmpty() && resource == "secrets"
    val isPod: Boolean get() = group.isEmpty() && resource == "pods"
    val isJob: Boolean get() = group == "batch" && resource == "jobs"

    /** The resource a scale patches, as KubeCan takes it: a Job itself, the /scale subresource otherwise. */
    val scaleResource: String get() = if (isJob) resource else "$resource/scale"

    companion object {
        fun pod(namespace: String, name: String) = KubeObjectRef("", "v1", "pods", "Pod", namespace, name, editable = true)

        fun pvc(namespace: String, name: String) =
            KubeObjectRef("", "v1", "persistentvolumeclaims", "PersistentVolumeClaim", namespace, name, editable = true)
    }
}

/**
 * How many pods an object wants ([replicas]) and runs ([current]) (KubeObjectScale). A Job's
 * count is its parallelism ([field] "parallelism"): how many of its pods run at once.
 */
@Serializable
data class KubeObjectScale(val replicas: Int = 0, val current: Int = 0, val field: String = "replicas") {
    // `this.`: a bare `field` in a getter is the backing field.
    val isParallelism: Boolean get() = this.field == "parallelism"
}

/** A container port of a pod's YAML: [name] "" when unnamed. */
data class ContainerPort(val port: Int, val name: String = "", val protocol: String = "TCP")

private val PORT_LINE = Regex("""^\s*(?:-\s+)?containerPort:\s*(\d+)\s*$""")
private val NAME_LINE = Regex("""^\s*(?:-\s+)?name:\s*["']?([^"'\s]+)["']?\s*$""")
private val PROTOCOL_LINE = Regex("""^\s*(?:-\s+)?protocol:\s*(\w+)\s*$""")

/**
 * The TCP container ports a pod's YAML (KubeObjectYAML) declares, in order, without
 * duplicates: the forward offers them. Reads each `ports:` list entry's containerPort, name
 * and protocol; UDP ones cannot be forwarded.
 */
fun containerPorts(yaml: String): List<ContainerPort> {
    val ports = mutableListOf<ContainerPort>()
    var entry: MutableMap<String, String>? = null
    var entryIndent = -1
    fun flush() {
        val e = entry ?: return
        val port = e["port"]?.toIntOrNull()
        if (port != null && port in 1..65535) ports += ContainerPort(port, e["name"].orEmpty(), e["protocol"] ?: "TCP")
        entry = null
    }
    var inPorts = false
    var portsIndent = -1
    for (line in yaml.lines()) {
        if (line.isBlank()) continue
        val indent = line.indexOfFirst { it != ' ' }
        if (line.trim() == "ports:") {
            flush()
            inPorts = true
            portsIndent = indent
            entryIndent = -1
            continue
        }
        if (!inPorts) continue
        // A key at the level of "ports:" (or above) ends the list; list items may sit at its indent.
        val item = line.trimStart().startsWith("- ")
        if (indent < portsIndent || indent == portsIndent && !item) {
            flush()
            inPorts = false
            entryIndent = -1
            continue
        }
        if (item && (entryIndent < 0 || indent <= entryIndent)) {
            flush()
            entry = mutableMapOf()
            entryIndent = indent
        }
        val e = entry ?: continue
        PORT_LINE.find(line)?.let { e["port"] = it.groupValues[1] }
        NAME_LINE.find(line)?.let { e["name"] = it.groupValues[1] }
        PROTOCOL_LINE.find(line)?.let { e["protocol"] = it.groupValues[1] }
    }
    flush()
    return ports.filter { it.protocol.equals("TCP", ignoreCase = true) }.distinctBy { it.port }
}

/** The URL to open for a forward listening on [address] ("127.0.0.1:PORT"). */
fun forwardUrl(address: String): String = "http://$address"
