package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.put

// Mirrors go/ichorgo/updates_cluster.go (ClusterUpdates) and updates_edit.go (UpdateEditTarget).

/** Who applied an image's workloads or a chart: [tool] flux, argocd or helm ("" for a bare workload). */
@Serializable
data class UpdateOwner(val tool: String = "", val kind: String = "", val namespace: String = "", val name: String = "")

/** The quick edit hint of an item: [op] image (a workload's container) or path (a manifest field, "" for a rendered image). */
@Serializable
data class UpdateEdit(
    val op: String = "",
    val kind: String = "",
    val name: String = "",
    val container: String = "",
    val image: String = "",
    val path: String = "",
    val value: String = "",
    val hint: String = "",
    val valueFiles: List<String> = emptyList(),
)

@Serializable
data class UpdateWorkloadRef(val kind: String = "", val namespace: String = "", val name: String = "", val containers: List<String> = emptyList())

/** An image the cluster runs, its newer tags and who bumps it. */
@Serializable
data class UpdateImage(
    val repo: String = "",
    val tag: String = "",
    val digest: String = "",
    val workloads: List<UpdateWorkloadRef> = emptyList(),
    val more: Int = 0,
    val owner: UpdateOwner? = null,
    /** "not readable" when its pull secrets cannot be read. */
    val credentials: String = "",
    val current: String = "",
    val patch: String = "",
    val minor: String = "",
    val major: String = "",
    val prerelease: Boolean = false,
    val shape: String = "",
    val reason: String = "",
    val source: String = "",
    val automatedBy: String = "",
    val edit: UpdateEdit? = null,
)

/** A chart a Flux, Argo CD or Helm release installs, its newer versions and who bumps it. */
@Serializable
data class UpdateChart(
    val name: String = "",
    val version: String = "",
    val repoURL: String = "",
    val owner: UpdateOwner? = null,
    val constraint: String = "",
    val current: String = "",
    val patch: String = "",
    val minor: String = "",
    val major: String = "",
    val prerelease: Boolean = false,
    val shape: String = "",
    val reason: String = "",
    val source: String = "",
    val automatedBy: String = "",
    val edit: UpdateEdit? = null,
)

@Serializable
data class UpdateSkipped(val what: String = "", val reason: String = "")

@Serializable
data class UpdateLookups(val queried: Int = 0, val failed: Int = 0)

/** `ClusterUpdates`' answer. */
@Serializable
data class ClusterUpdates(
    val at: Long = 0,
    val images: List<UpdateImage> = emptyList(),
    val charts: List<UpdateChart> = emptyList(),
    val skipped: List<UpdateSkipped> = emptyList(),
    val lookups: UpdateLookups = UpdateLookups(),
)

/** The app whose source holds the file to edit. */
@Serializable
data class UpdateEditApp(val tool: String = "", val kind: String = "", val namespace: String = "", val name: String = "")

/** `UpdateEditTarget`'s quick edit, `GitOpsQuickEdit`'s editJSON. */
@Serializable
data class UpdateQuickEdit(
    val op: String = "",
    val documentIndex: Int = 0,
    val kind: String = "",
    val name: String = "",
    val container: String = "",
    val kustomizeImage: String = "",
    val image: String = "",
    val path: String = "",
    val value: String = "",
)

/** `UpdateEditTarget`'s answer: the file ([path] of source [sourceIndex] of [app]) and the [edit] that bumps the item; [note] when there is none. */
@Serializable
data class UpdateEditTarget(
    val app: UpdateEditApp? = null,
    val sourceIndex: Int = 0,
    val path: String = "",
    val line: Int = 0,
    val documentIndex: Int = 0,
    val edit: UpdateQuickEdit? = null,
    val note: String = "",
    val valueFiles: List<String> = emptyList(),
)

/** A step to a newer version: [PATCH], [MINOR] or [MAJOR]. */
enum class UpdateStep { PATCH, MINOR, MAJOR }

/** One candidate chip: the [step] and its [version]. */
data class UpdateCandidate(val step: UpdateStep, val version: String)

/** What a row automation already bumps: shown as a badge, with no chips. */
enum class UpdateAutomation { FLUX_IMAGE, ARGO_IMAGE_UPDATER, SEMVER_RANGE, BOT }

/**
 * One row of the Updates screen, an image or a chart: [name] (the image repository or the
 * chart's name), [running], the [candidates], the [automation] that bumps it, why it is
 * [reason] not comparable, and the [item] JSON sent back to `UpdateEditTarget`.
 */
data class UpdateRow(
    val key: String,
    val chart: Boolean,
    val name: String,
    val running: String,
    val candidates: List<UpdateCandidate>,
    val automation: UpdateAutomation?,
    val reason: String,
    val owner: UpdateOwner?,
    val editable: Boolean,
    val source: String,
    val item: JsonObject,
) {
    /** The newest candidate, for the row's "running → newest". */
    val newest: String get() = candidates.lastOrNull()?.version.orEmpty()
}

/** The screen's three sections. */
data class UpdateSections(val images: List<UpdateRow>, val charts: List<UpdateRow>, val notComparable: List<UpdateRow>) {
    val isEmpty: Boolean get() = images.isEmpty() && charts.isEmpty() && notComparable.isEmpty()

    /** The rows with a newer version (the GitOps card's count). */
    val outdated: Int get() = (images + charts).count { it.candidates.isNotEmpty() }
}

private val ItemJson = Json { encodeDefaults = false; explicitNulls = false }

private fun candidates(patch: String, minor: String, major: String): List<UpdateCandidate> = listOfNotNull(
    patch.takeIf { it.isNotEmpty() }?.let { UpdateCandidate(UpdateStep.PATCH, it) },
    minor.takeIf { it.isNotEmpty() }?.let { UpdateCandidate(UpdateStep.MINOR, it) },
    major.takeIf { it.isNotEmpty() }?.let { UpdateCandidate(UpdateStep.MAJOR, it) },
)

fun updateAutomation(automatedBy: String): UpdateAutomation? = when (automatedBy) {
    "flux-image-automation" -> UpdateAutomation.FLUX_IMAGE
    "argocd-image-updater" -> UpdateAutomation.ARGO_IMAGE_UPDATER
    "flux-semver" -> UpdateAutomation.SEMVER_RANGE
    "bot" -> UpdateAutomation.BOT
    else -> null
}

private fun UpdateImage.row(): UpdateRow {
    val auto = updateAutomation(automatedBy)
    return UpdateRow(
        key = "image:$repo:$tag:$digest",
        chart = false,
        name = repo,
        running = tag.ifEmpty { digest.take(DIGEST_SHOWN) },
        // An automated image shows its badge, not chips: something else bumps it.
        candidates = if (auto != null) emptyList() else candidates(patch, minor, major),
        automation = auto,
        reason = reason,
        owner = owner,
        editable = edit != null && auto == null,
        source = source,
        item = ItemJson.encodeToJsonElement(this) as JsonObject,
    )
}

private fun UpdateChart.row(): UpdateRow {
    val auto = updateAutomation(automatedBy)
    return UpdateRow(
        key = "chart:$name:${owner?.namespace}/${owner?.name}",
        chart = true,
        name = name,
        running = version,
        candidates = if (auto != null) emptyList() else candidates(patch, minor, major),
        automation = auto,
        reason = reason,
        owner = owner,
        editable = edit != null && auto == null,
        source = source.ifEmpty { repoURL },
        item = ItemJson.encodeToJsonElement(this) as JsonObject,
    )
}

/**
 * [u] in sections: images and charts with a newer version or a badge, then the ones not
 * comparable (a reason, no candidate); up to date rows are left out. Go's order is kept.
 */
fun updateSections(u: ClusterUpdates): UpdateSections {
    val images = u.images.map { it.row() }
    val charts = u.charts.map { it.row() }
    val shown = { r: UpdateRow -> r.candidates.isNotEmpty() || r.automation != null }
    return UpdateSections(
        images = images.filter(shown),
        charts = charts.filter(shown),
        notComparable = (images + charts).filter { !shown(it) && it.reason.isNotEmpty() },
    )
}

/** `UpdateEditTarget`'s itemJSON: [row]'s item with the [version] of the chip tapped. */
fun updateItemJson(row: UpdateRow, version: String): String = buildJsonObject {
    row.item.forEach { (k, v) -> put(k, v) }
    put("version", version)
}.toString()

/** The pull request's title (English: text of the repository): "Update podinfo to 6.7.1". */
fun updateProposalTitle(row: UpdateRow, version: String): String = "Update ${row.name.substringAfterLast('/')} to $version"

/** The pull request's body: what changes and where the version comes from. */
fun updateProposalBody(row: UpdateRow, version: String): String = buildString {
    append("Updates `${row.name}` from `${row.running}` to `$version`.")
    if (row.source.isNotEmpty()) append("\n\nSource: ${row.source}")
    append("\n\nProposed from the Updates screen of Ichor.")
}

/**
 * The quick edit [target] makes for [row] at [version], titled and described for its pull
 * request; null when it has none (a note says why).
 */
fun updateQuickEdit(target: UpdateEditTarget, row: UpdateRow, version: String): QuickEdit? {
    val e = target.edit ?: return null
    val title = updateProposalTitle(row, version)
    val body = updateProposalBody(row, version)
    return when {
        e.op == QuickEdit.OP_PATH -> QuickEdit.path(e.documentIndex, e.kind, e.name, e.path, e.value).copy(title = title, body = body)
        e.kustomizeImage.isNotEmpty() -> QuickEdit.kustomize(e.documentIndex, e.kustomizeImage, e.image).copy(title = title, body = body)
        else -> QuickEdit.image(e.documentIndex, e.container, e.image, e.kind, e.name).copy(title = title, body = body)
    }
}

/** A digest shown when an image has no tag: "sha256:" and the first hex digits. */
private const val DIGEST_SHOWN = 19

/** One item's edit in a planned file: [line] is on the content the items before it already edited. */
@Serializable
data class UpdateProposalChange(val item: String = "", val index: Int = 0, val line: Int = 0, val before: String = "", val after: String = "")

/** A file of the plan: [original] as read at the branch (its [fileSha] the lock), [content] with every change applied. */
@Serializable
data class UpdateProposalFile(
    val path: String = "",
    val content: String = "",
    val original: String = "",
    val fileSha: String = "",
    val changes: List<UpdateProposalChange> = emptyList(),
)

/** An item left out of the plan, [index] in the items sent, and why. */
@Serializable
data class UpdateProposalSkip(val item: String = "", val index: Int = 0, val reason: String = "")

/** `UpdateProposalPlan`'s answer: one pull request on [source]'s repository at [baseRef] for the items selected. */
@Serializable
data class UpdateProposalPlan(
    val source: GitSource? = null,
    val baseRef: String = "",
    val files: List<UpdateProposalFile> = emptyList(),
    val title: String = "",
    val message: String = "",
    val branch: String = "",
    val body: String = "",
    val skipped: List<UpdateProposalSkip> = emptyList(),
)

/** The version a row is selected with: its smallest step (patch, else minor, else major); null when it cannot be proposed. */
fun defaultSelection(row: UpdateRow): String? = row.candidates.firstOrNull()?.version?.takeIf { row.editable }

/** `UpdateProposalPlan`'s itemsJSON: each selected row's item with the version picked, in [selection]'s order. */
fun updateProposalItemsJson(selection: List<Pair<UpdateRow, String>>): String =
    selection.joinToString(",", "[", "]") { (row, version) -> updateItemJson(row, version) }
