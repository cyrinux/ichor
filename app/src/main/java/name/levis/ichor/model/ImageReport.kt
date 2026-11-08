package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/image_report.go.

/**
 * The images report of an inventory: images not pinned, repositories running several tags,
 * the registries pulled from, and (Talos) the images a node keeps that no container runs.
 */
@Serializable
data class ImageReport(
    val unpinned: List<ReportUnpinned> = emptyList(),
    val multiTag: List<ReportRepo> = emptyList(),
    /** Most images first. */
    val registries: List<ReportRegistry> = emptyList(),
    val unusedOnNode: List<ReportUnused> = emptyList(),
    val summary: ReportSummary = ReportSummary(),
)

/** An app of the inventory ([id] opens its sheet), with a namespace it runs in. */
@Serializable
data class ReportAppRef(val id: String = "", val name: String = "", val namespace: String = "")

/** An image run as `:latest` or untagged, without a digest. */
@Serializable
data class ReportUnpinned(
    val image: String = "",
    val apps: List<ReportAppRef> = emptyList(),
    /** Node addresses. */
    val nodes: List<String> = emptyList(),
)

/** A repository running with several tags (or digests) at once. */
@Serializable
data class ReportRepo(val repository: String = "", val tags: List<ReportTag> = emptyList())

@Serializable
data class ReportTag(
    /** The tag, "@sha256:…" for a digest alone, "latest" for neither. */
    val tag: String = "",
    val nodes: List<String> = emptyList(),
    val apps: List<ReportAppRef> = emptyList(),
)

@Serializable
data class ReportRegistry(
    val host: String = "",
    /** Distinct images, and the apps running them. */
    val images: Int = 0,
    val apps: Int = 0,
    /** Docker Hub: anonymous pulls are rate limited. */
    val dockerHub: Boolean = false,
)

/** An image in [node]'s CRI store that none of its containers runs. */
@Serializable
data class ReportUnused(val node: String = "", val image: String = "", val sizeBytes: Long = 0)

@Serializable
data class ReportSummary(
    val images: Int = 0,
    val unpinned: Int = 0,
    val multiTag: Int = 0,
    /** Distinct images pulled from Docker Hub. */
    val dockerHub: Int = 0,
    val unused: Int = 0,
)

/** The nodes whose CRI images the report compares with the containers: every node running a pod. */
val Inventory.reportNodes: List<String>
    get() = apps.flatMap { it.nodes }.filter { it.isNotEmpty() }.distinct().sorted()

/** Bytes the unused images take, all nodes together. */
val ImageReport.unusedBytes: Long get() = unusedOnNode.sumOf { it.sizeBytes }
