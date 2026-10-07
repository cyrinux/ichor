package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/imagescan*.go.

const val IMAGESCAN_PHASE_PREPARING = "preparing"
const val IMAGESCAN_PHASE_STARTING = "starting"
const val IMAGESCAN_PHASE_DATABASE = "database"
const val IMAGESCAN_PHASE_SCANNING = "scanning"
const val IMAGESCAN_PHASE_CLEANING = "cleaning"

const val IMAGESCAN_SOURCE_OPERATOR = "operator"

/** Trivy's severities, most severe first. */
enum class VulnSeverity { CRITICAL, HIGH, MEDIUM, LOW, UNKNOWN }

fun severityOf(name: String): VulnSeverity = VulnSeverity.entries.firstOrNull { it.name == name } ?: VulnSeverity.UNKNOWN

/** The formats ImageScanExport writes: its name, the file extension and the type it is shared as. */
enum class ImageScanFormat(val id: String, val extension: String, val mime: String) {
    HTML("html", "html", "text/html"),
    // Shared as plain JSON: few apps accept the formats' own types; the extension tells them apart.
    SARIF("sarif", "sarif.json", "application/json"),
    CYCLONEDX("cyclonedx", "cdx.json", "application/json"),
    CSV("csv", "csv", "text/csv"),
    JSON("json", "json", "application/json"),
}

@Serializable
data class ImageScanProgress(
    val phase: String = "",
    val step: Int = 0,
    val steps: Int = 0,
    val image: String = "",
    /** The container's waiting reason while the pod starts (ContainerCreating). */
    val message: String = "",
)

@Serializable
data class ImageScanReport(
    /** "scan" (a Trivy Job run by the app) or [IMAGESCAN_SOURCE_OPERATOR]. */
    val source: String = "",
    val scanner: String = "",
    val started: Long = 0,
    val finished: Long = 0,
    val images: List<ScannedImage> = emptyList(),
) {
    val summary: VulnSummary get() = images.fold(VulnSummary()) { sum, image -> sum + image.summary }
    val failed: Int get() = images.count { it.error.isNotEmpty() }
}

@Serializable
data class ScannedImage(
    /** As the pods name it (tag). */
    val image: String = "",
    /** What was scanned: repo@digest when known. */
    val ref: String = "",
    val digest: String = "",
    val os: String = "",
    val pods: List<String> = emptyList(),
    val scannedAt: Long = 0,
    val error: String = "",
    val summary: VulnSummary = VulnSummary(),
    val vulnerabilities: List<ImageVuln> = emptyList(),
)

@Serializable
data class VulnSummary(
    val critical: Int = 0,
    val high: Int = 0,
    val medium: Int = 0,
    val low: Int = 0,
    val unknown: Int = 0,
    /** With a fixed version. */
    val fixable: Int = 0,
    /** In the OS packages: a newer base image fixes them. */
    val os: Int = 0,
) {
    val total: Int get() = critical + high + medium + low + unknown

    fun count(severity: VulnSeverity): Int = when (severity) {
        VulnSeverity.CRITICAL -> critical
        VulnSeverity.HIGH -> high
        VulnSeverity.MEDIUM -> medium
        VulnSeverity.LOW -> low
        VulnSeverity.UNKNOWN -> unknown
    }

    operator fun plus(o: VulnSummary) = VulnSummary(
        critical + o.critical, high + o.high, medium + o.medium, low + o.low, unknown + o.unknown, fixable + o.fixable, os + o.os,
    )
}

@Serializable
data class ImageVuln(
    val id: String = "",
    val `package`: String = "",
    val installed: String = "",
    val fixed: String = "",
    val severity: String = "",
    val title: String = "",
    val description: String = "",
    val url: String = "",
    val score: Double = 0.0,
    val vector: String = "",
    val purl: String = "",
    /** "debian 12.5" for an OS package, the file that brought a library ("usr/local/bin/app"). */
    val target: String = "",
    val `class`: String = "",
    val type: String = "",
    val published: String = "",
) {
    val level: VulnSeverity get() = severityOf(severity)
    val fixable: Boolean get() = fixed.isNotEmpty()
}

/** ImageScanOperatorReports' answer: [available] false without the Trivy Operator. */
@Serializable
data class OperatorReports(val available: Boolean = false, val report: ImageScanReport = ImageScanReport())

/** What the report shows: the fixable findings only, of the chosen severities. */
data class VulnFilter(val fixableOnly: Boolean = true, val severities: Set<VulnSeverity> = VulnSeverity.entries.toSet()) {
    fun keeps(v: ImageVuln) = (!fixableOnly || v.fixable) && v.level in severities

    fun toggle(severity: VulnSeverity) = copy(severities = if (severity in severities) severities - severity else severities + severity)
}

/** [image]'s findings [filter] keeps, grouped by package (with its installed version), in report order. */
fun ScannedImage.byPackage(filter: VulnFilter): List<Pair<String, List<ImageVuln>>> =
    vulnerabilities.filter(filter::keeps)
        .groupBy { "${it.`package`} ${it.installed}" }
        .toList()

/** "debian:12" style name of an image without its registry, for titles. */
val ScannedImage.shortName: String get() = image.substringAfterLast('/').ifEmpty { ref.substringAfterLast('/') }

/** What the core puts in an image it did not get to scan (stopped, failed before). */
const val IMAGESCAN_NOT_SCANNED = "not scanned"

/** The core's messages for a scan that timed out or was stopped (go/ichorgo/imagescan.go). */
const val IMAGESCAN_TIMED_OUT = "image scan timed out"
