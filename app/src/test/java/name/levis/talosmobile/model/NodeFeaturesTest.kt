package name.levis.talosmobile.model

import name.levis.talosmobile.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NodeFeaturesTest {

    private fun features(vararg pairs: Pair<TalosFeature, FeatureSupport>) =
        NodeFeatures(version = "v1.10.0", features = pairs.associate { it.first.key to it.second })

    private val needs115 = FeatureSupport(supported = false, minVersion = "v1.15", reason = "needs Talos v1.15 or newer")

    @Test
    fun decodesGoJson() {
        val json = """{"version":"v1.9.2","features":{"diskHealth":{"supported":false,"minVersion":"v1.15","reason":"needs Talos v1.15 or newer"},"events":{"supported":true}}}"""
        val decoded = TalosJson.decodeFromString(NodeFeatures.serializer(), json)
        assertEquals("v1.9.2", decoded.version)
        assertFalse(decoded.support(TalosFeature.DISK_HEALTH).supported)
        assertEquals("v1.15", decoded.support(TalosFeature.DISK_HEALTH).minVersion)
        assertTrue(decoded.support(TalosFeature.EVENTS).supported)
    }

    @Test
    fun unknownMeansSupported() {
        val none: NodeFeatures? = null
        assertTrue(none.support(TalosFeature.UPGRADE).supported)
        assertTrue(features().support(TalosFeature.UPGRADE).supported)
        assertNull(features().support(TalosFeature.UPGRADE).notice)
    }

    @Test
    fun keysAreUnique() {
        assertEquals(TalosFeature.entries.size, TalosFeature.entries.map { it.key }.toSet().size)
    }

    @Test
    fun noticeNormalisesTheVersion() {
        assertEquals(VersionNotice("v1.15"), needs115.notice)
        assertEquals(VersionNotice("v1.12"), FeatureSupport(supported = false, minVersion = "1.12").notice)
        assertEquals(VersionNotice(""), FeatureSupport(supported = false).notice)
        // The reason wins when there is one.
        assertEquals(VersionNotice("v1.8"), FeatureSupport(supported = false, reason = "needs Talos v1.8 or newer").notice)
        val goReason = "not available on this node's Talos version (v1.2.0): the etcd status API needs Talos v1.3 or newer"
        assertEquals(VersionNotice("v1.3"), FeatureSupport(supported = false, minVersion = "v1.3", reason = goReason).notice)
        // Removed from newer Talos: no "needs vX or newer", although a minimum version exists.
        val removed = "not available on this node's Talos version (v1.18.0): removed in Talos v1.18"
        assertEquals(VersionNotice(""), FeatureSupport(supported = false, minVersion = "v1.2", reason = removed).notice)
    }

    @Test
    fun clusterFeatureIsAvailableWhenAnyNodeHasIt() {
        val old = features(TalosFeature.SUPPORT_BUNDLE to needs115)
        val new = features(TalosFeature.SUPPORT_BUNDLE to FeatureSupport())
        assertTrue(clusterSupport(listOf(old, new), TalosFeature.SUPPORT_BUNDLE).supported)
        assertTrue(clusterSupport(emptyList(), TalosFeature.SUPPORT_BUNDLE).supported)
        // A node that says nothing about the feature does not block it.
        assertTrue(clusterSupport(listOf(features()), TalosFeature.SUPPORT_BUNDLE).supported)
    }

    @Test
    fun clusterFeatureUnsupportedEverywhereReportsLowestVersion() {
        val a = features(TalosFeature.ETCD_MEMBER_ACTIONS to needs115)
        val b = features(TalosFeature.ETCD_MEMBER_ACTIONS to FeatureSupport(supported = false, minVersion = "v1.9"))
        val support = clusterSupport(listOf(a, b), TalosFeature.ETCD_MEMBER_ACTIONS)
        assertFalse(support.supported)
        assertEquals(VersionNotice("v1.9"), support.notice)
    }

    @Test
    fun comparesVersionsNumerically() {
        assertTrue(compareVersions("v1.9", "v1.15") < 0)
        assertTrue(compareVersions("1.15.2", "v1.15") > 0)
        assertEquals(0, compareVersions("v1.15", "1.15.0"))
        assertTrue(compareVersions("v1.12.0-beta.1", "v1.11.9") > 0)
    }

    @Test
    fun recognisesVersionErrors() {
        assertEquals(VersionNotice("v1.15"), versionNotice("disk health needs Talos v1.15 or newer"))
        assertEquals(
            VersionNotice("v1.8"),
            versionNotice("not available on this node's Talos version (v1.7.6): volumes need Talos v1.8 or newer"),
        )
        assertEquals(VersionNotice("v1.9.1"), versionNotice("Needs Talos 1.9.1 or newer (node runs v1.8.0)"))
        assertEquals(VersionNotice(""), versionNotice("Unimplemented: unknown method ContainerLogs"))
        assertEquals(VersionNotice(""), versionNotice("volumes: not available on this node's Talos version"))
    }

    @Test
    fun otherErrorsStayErrors() {
        assertNull(versionNotice("unreachable: connection refused"))
        assertNull(versionNotice("permission denied (talosconfig role too limited): not authorized"))
        assertNull(versionNotice(""))
    }

    @Test
    fun displayVersionAddsThePrefixOnce() {
        assertEquals("v1.15", displayVersion("1.15"))
        assertEquals("v1.15", displayVersion(" v1.15 "))
        assertEquals("", displayVersion(""))
    }
}
