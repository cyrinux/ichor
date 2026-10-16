package name.levis.ichor

import name.levis.ichor.data.MemoryPrefs
import name.levis.ichor.data.PinStore
import name.levis.ichor.data.PrivacyMask
import name.levis.ichor.data.pinScope
import name.levis.ichor.model.Pin
import name.levis.ichor.model.Pins
import name.levis.ichor.model.ShareTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PinStoreTest {
    private val node = Pin(ShareTarget(cluster = "abcdefghijklmnop", target = ShareTarget.NODE, addr = "10.0.0.11", host = "cp-1"), "cp-1")
    private val app = Pin(ShareTarget.argoApp("argocd", "shop"), "shop")
    private val flux = Pin(ShareTarget.fluxApp("Kustomization", "flux-system", "apps"), "apps")

    @Test
    fun encodeParseRoundTripsInTheSharedJson() {
        val pins = listOf(node, app, flux)
        val text = Pins.encode(pins)
        assertEquals(pins, Pins.parse(text))
        // The share target's JSON without its empty fields, as iOS writes it.
        assertTrue(text.startsWith("""[{"target":{"cluster":"abcdefghijklmnop","target":"node","host":"cp-1","addr":"10.0.0.11"},"label":"cp-1"}"""))
        assertTrue(""""target":{"target":"argo-app","ns":"argocd","name":"shop"},"label":"shop"""" in text)
    }

    @Test
    fun parseReadsWhatIosWrites() {
        val text = """[{"label":"web","target":{"target":"workload","kind":"Deployment","ns":"shop","name":"web","extra":1}}]"""
        assertEquals(listOf(Pin(ShareTarget.workload("Deployment", "shop", "web"), "web")), Pins.parse(text))
        // A pin without a label still reads, as on iOS: an empty one.
        val bare = """[{"target":{"target":"argo-app","ns":"argocd","name":"shop"}}]"""
        assertEquals(listOf(Pin(ShareTarget.argoApp("argocd", "shop"), "")), Pins.parse(bare))
    }

    @Test
    fun parseDropsWhatCannotBeRead() {
        assertEquals(emptyList<Pin>(), Pins.parse(null))
        assertEquals(emptyList<Pin>(), Pins.parse(""))
        assertEquals(emptyList<Pin>(), Pins.parse("not json"))
        // A cluster-wide screen is not a pin; a repeat is dropped.
        val text = Pins.encode(listOf(app, Pin(ShareTarget.screen(ShareTarget.ETCD), "etcd"), app.copy(label = "again")))
        assertEquals(listOf(app), Pins.parse(text))
    }

    @Test
    fun toggleAddsThenRemoves() {
        val added = Pins.toggle(emptyList(), app)!!
        assertEquals(listOf(app), added)
        assertTrue(Pins.contains(added, ShareTarget.argoApp("argocd", "shop")))
        // The same object, from another tab or with the cluster id: unpinned.
        val sameNode = node.copy(target = node.target.copy(cluster = "", tab = "live"))
        assertEquals(listOf(app), Pins.toggle(added + node, sameNode))
        assertEquals(emptyList<Pin>(), Pins.toggle(added, app))
    }

    @Test
    fun atMostTwelvePerCluster() {
        val full = (1..Pins.MAX).map { Pin(ShareTarget.pod("default", "pod-$it"), "pod-$it") }
        assertNull(Pins.toggle(full, app))
        // Unpinning still works when full.
        assertEquals(Pins.MAX - 1, Pins.toggle(full, full.first())!!.size)
        assertEquals(Pins.MAX, Pins.parse(Pins.encode(full + app)).size)
        assertNull(Pins.toggle(emptyList(), Pin(ShareTarget.screen(ShareTarget.CLUSTER), "cluster")))
    }

    @Test
    fun storeKeepsEachClusterApartAndReadsBack() {
        val prefs = MemoryPrefs()
        val store = PinStore(prefs)
        assertTrue(store.toggle("cluster-a", node))
        assertTrue(store.toggle("cluster-b", app))
        assertEquals(listOf(node), store.pins("cluster-a").value)
        assertEquals(listOf(app), store.pins("cluster-b").value)
        // Another store on the same preferences (the next launch) reads them back.
        assertEquals(listOf(node), PinStore(prefs).pins("cluster-a").value)
        assertTrue(store.toggle("cluster-a", node))
        assertEquals(emptyList<Pin>(), PinStore(prefs).pins("cluster-a").value)
    }

    @Test
    fun storeRefusesTheThirteenth() {
        val store = PinStore(MemoryPrefs())
        repeat(Pins.MAX) { assertTrue(store.toggle("c", Pin(ShareTarget.pod("default", "pod-$it"), "pod-$it"))) }
        assertFalse(store.toggle("c", app))
        assertEquals(Pins.MAX, store.pins("c").value.size)
    }

    @Test
    fun screenshotModeKeepsItsOwnPins() {
        assertEquals("fp", pinScope("fp", PrivacyMask()))
        assertNotEquals("fp", pinScope("fp", PrivacyMask(enabled = true)))
    }
}
