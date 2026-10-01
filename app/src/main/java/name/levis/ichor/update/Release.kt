package name.levis.ichor.update

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import name.levis.ichor.model.ChangelogRelease

/** Subset of GitHub's "latest release" API response. */
@Serializable
data class GitHubRelease(
    @SerialName("tag_name") val tagName: String,
    @SerialName("html_url") val htmlUrl: String = "",
    val body: String? = null,
    val draft: Boolean = false,
    val prerelease: Boolean = false,
    val assets: List<GitHubAsset> = emptyList(),
)

@Serializable
data class GitHubAsset(
    val name: String,
    @SerialName("browser_download_url") val downloadUrl: String,
    val size: Long = 0,
    /** "sha256:<hex>", published by GitHub for release assets. */
    val digest: String? = null,
)

data class UpdateInfo(
    val version: String,
    val notes: String,
    val pageUrl: String,
    val apkUrl: String,
    val apkSize: Long,
    val sha256: String?,
    /** Structured notes of the releases newer than this build; empty when not fetched. */
    val changes: List<ChangelogRelease> = emptyList(),
)

/**
 * The update to offer, or null: the release must be newer than [currentVersion] and carry a
 * signed APK for one of the device's [abis], in preference order (`Build.SUPPORTED_ABIS`).
 * CI attaches "ichor-<tag>-<abi>.apk" (older releases: "talosdev-mobile-…"); "-unsigned" builds cannot be installed.
 */
fun selectUpdate(release: GitHubRelease, currentVersion: String, abis: List<String>): UpdateInfo? {
    if (release.draft || release.prerelease) return null
    if (!isNewer(release.tagName, currentVersion)) return null
    val signed = release.assets.filter { it.name.endsWith(".apk") && !it.name.contains("unsigned") }
    val apk = abis.firstNotNullOfOrNull { abi -> signed.firstOrNull { it.name.endsWith("-$abi.apk") } } ?: return null
    return UpdateInfo(
        version = release.tagName.removePrefix("v"),
        notes = release.body.orEmpty().trim(),
        pageUrl = release.htmlUrl,
        apkUrl = apk.downloadUrl,
        apkSize = apk.size,
        sha256 = apk.digest?.takeIf { it.startsWith("sha256:") }?.removePrefix("sha256:")?.lowercase(),
    )
}

/** MAJOR.MINOR.PATCH of "v1.2.3", "1.2.3+4.gabc.dirty", "0.0.0-unknown"; null if none. */
fun parseVersion(version: String): Triple<Int, Int, Int>? {
    val match = Regex("""^v?(\d+)\.(\d+)\.(\d+)""").find(version.trim()) ?: return null
    val (major, minor, patch) = match.destructured
    return Triple(major.toInt(), minor.toInt(), patch.toInt())
}

/**
 * True when [tag] is a strictly newer release than [current]. Builds after a tag
 * ("0.1.0+3.gabc") count as that tag, so they are not offered the same release again.
 */
fun isNewer(tag: String, current: String): Boolean {
    val remote = parseVersion(tag) ?: return false
    val local = parseVersion(current) ?: return true
    return compareValuesBy(remote, local, { it.first }, { it.second }, { it.third }) > 0
}
