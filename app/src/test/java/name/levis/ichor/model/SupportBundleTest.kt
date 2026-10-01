package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.ZoneOffset

class SupportBundleTest {

    @Test
    fun fileNameIsSafeAndRecognised() {
        val name = supportBundleFileName("admin@home lab/1", 1_759_340_000_000, ZoneOffset.UTC)
        assertEquals("support-admin-home-lab-1-20251001-173320.zip", name)
        assertTrue(isSupportBundleName(name))
        assertTrue(isSupportBundleName(supportBundleFileName("", 0, ZoneOffset.UTC)))
        assertTrue(supportBundleFileName("../..", 0, ZoneOffset.UTC).startsWith("support-cluster-"))
    }

    @Test
    fun foreignFilesAreNotBundles() {
        assertFalse(isSupportBundleName("support-x.zip.part"))
        assertFalse(isSupportBundleName("capture.pcap"))
        assertFalse(isSupportBundleName("support-../x.zip"))
    }

    @Test
    fun progressFractions() {
        val p = TalosJson.decodeFromString(SupportProgress.serializer(), """{"node":"10.0.0.1","step":"logs","done":3,"total":12}""")
        assertEquals(0.25f, p.fraction!!, 0.0001f)
        assertFalse(p.finished)
        assertTrue(p.copy(done = 12).finished)
        assertNull(SupportProgress(node = "n").fraction)
        assertFalse(SupportProgress(node = "n").finished)
    }

    @Test
    fun nodesFinishOnTheirOwnCount() {
        val nodes = listOf("a", "b")
        var p = BundleProgress()
        assertEquals(BundleRowState.WAITING, p.stateOf("a"))
        assertEquals(0f, p.overallFraction(nodes), 0.0001f)

        p = p.with(SupportProgress("a", "version", 0, 8)).with(SupportProgress("a", "kernel log", 1, 8))
        assertEquals(BundleRowState.COLLECTING, p.stateOf("a"))
        assertEquals("kernel log", p.stepOf("a"))
        assertEquals(BundleRowState.WAITING, p.stateOf("b"))
        assertNull(p.stepOf("b"))

        // Another node reporting does not finish the first one: only its closing report does.
        p = p.with(SupportProgress("b", "version", 0, 8))
        assertEquals(BundleRowState.COLLECTING, p.stateOf("a"))
        p = p.with(SupportProgress("a", "done", 8, 8))
        assertEquals(BundleRowState.DONE, p.stateOf("a"))
        // "done" closes a node, it is not a step to show.
        assertNull(p.stepOf("a"))
        assertEquals(1f / 3, p.overallFraction(nodes), 0.0001f)

        p = p.with(SupportProgress("b", "done", 8, 8)).with(SupportProgress(BUNDLE_CLUSTER, "etcd", 0, 1))
        assertEquals(BundleRowState.COLLECTING, p.stateOf(BUNDLE_CLUSTER))
        assertEquals(2f / 3, p.overallFraction(nodes), 0.0001f)
        p = p.with(SupportProgress(BUNDLE_CLUSTER, "done", 1, 1))
        assertEquals(1f, p.overallFraction(nodes), 0.0001f)
    }

    @Test
    fun allNodesArePreselected() {
        val up = NodeOverview(node = "1", hostname = "a", reachable = true)
        val down = NodeOverview(node = "2", hostname = "b", reachable = false)
        assertEquals(setOf("1", "2"), defaultBundleNodes(listOf(up, down)))
        assertTrue(defaultBundleNodes(emptyList()).isEmpty())
    }
}
