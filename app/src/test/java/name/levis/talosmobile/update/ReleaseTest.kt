package name.levis.talosmobile.update

import name.levis.talosmobile.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ReleaseTest {

    private val arm64 = listOf("arm64-v8a", "armeabi-v7a", "armeabi")

    @Test
    fun versionComparison() {
        assertTrue(isNewer("v0.2.0", "0.1.0"))
        assertTrue(isNewer("v1.0.0", "0.9.9"))
        assertTrue(isNewer("v0.1.10", "0.1.9"))
        assertFalse(isNewer("v0.1.0", "0.1.0"))
        assertFalse("post-tag dev builds equal their tag", isNewer("v0.1.0", "0.1.0+3.gabc123.dirty"))
        assertFalse(isNewer("v0.1.0", "0.2.0"))
        assertTrue("unknown local version takes any release", isNewer("v0.1.0", "0.0.0-unknown".replace("0.0.0", "x")))
        assertFalse("garbage tags are ignored", isNewer("nightly", "0.1.0"))
    }

    private val json = """
        {"tag_name":"v0.2.0","html_url":"https://github.com/o/r/releases/tag/v0.2.0","body":"- fix\n",
         "draft":false,"prerelease":false,"assets":[
           {"name":"TalosViewer-0.2.0-unsigned.ipa","browser_download_url":"https://x/ipa","size":9},
           {"name":"talos-viewer-v0.2.0-arm64-v8a-unsigned.apk","browser_download_url":"https://x/unsigned","size":1},
           {"name":"talos-viewer-v0.2.0-armeabi-v7a.apk","browser_download_url":"https://x/arm32","size":12},
           {"name":"talos-viewer-v0.2.0-arm64-v8a.apk","browser_download_url":"https://x/apk","size":16,
            "digest":"sha256:ABCDEF0123"}],
         "author":{"login":"someone"}}
    """.trimIndent()

    @Test
    fun selectsSignedApkWithDigest() {
        val update = selectUpdate(TalosJson.decodeFromString(GitHubRelease.serializer(), json), "0.1.0", arm64)!!
        assertEquals("0.2.0", update.version)
        assertEquals("https://x/apk", update.apkUrl)
        assertEquals("abcdef0123", update.sha256)
        assertEquals("- fix", update.notes)
    }

    @Test
    fun noUpdateWhenCurrentOrOnlyUnsigned() {
        val release = TalosJson.decodeFromString(GitHubRelease.serializer(), json)
        assertNull(selectUpdate(release, "0.2.0", arm64))
        assertNull(selectUpdate(release.copy(assets = release.assets.filter { "unsigned" in it.name }), "0.1.0", arm64))
        assertNull(selectUpdate(release.copy(prerelease = true), "0.1.0", arm64))
    }

    @Test
    fun picksTheDeviceAbiInPreferenceOrder() {
        val release = TalosJson.decodeFromString(GitHubRelease.serializer(), json)
        assertEquals("https://x/arm32", selectUpdate(release, "0.1.0", listOf("armeabi-v7a", "armeabi"))?.apkUrl)
        assertNull("no APK for x86_64 in this release", selectUpdate(release, "0.1.0", listOf("x86_64")))
    }

    @Test
    fun fileNamePrefixDoesNotMatter() {
        val renamed = """
            {"tag_name":"v0.4.0","assets":[
              {"name":"talosdev-mobile-v0.4.0-unsigned.ipa","browser_download_url":"https://x/ipa","size":9},
              {"name":"talosdev-mobile-v0.4.0-arm64-v8a-unsigned.apk","browser_download_url":"https://x/unsigned","size":1},
              {"name":"talosdev-mobile-v0.4.0-arm64-v8a.apk","browser_download_url":"https://x/new","size":16}]}
        """.trimIndent()
        val update = selectUpdate(TalosJson.decodeFromString(GitHubRelease.serializer(), renamed), "0.3.0", arm64)
        assertEquals("https://x/new", update?.apkUrl)
    }
}
