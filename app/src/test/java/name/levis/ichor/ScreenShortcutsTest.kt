package name.levis.ichor

import java.net.URI
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.shortcuts.PinnedShortcut
import name.levis.ichor.shortcuts.appShareLink
import name.levis.ichor.shortcuts.clusterOfAppLink
import name.levis.ichor.shortcuts.isScreenShortcut
import name.levis.ichor.shortcuts.keepsPinned
import name.levis.ichor.shortcuts.screenGlyph
import name.levis.ichor.shortcuts.screenShortcutId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The link a home screen shortcut carries. The Go core (BuildShareLink, ParseShareLink) does
 * not load in JVM tests: the https links below are what BuildShareLink returns for those
 * targets, and ParseShareLink reads the ichor://open query exactly as the https fragment.
 */
class ScreenShortcutsTest {

    private val node = "https://cyrinux.github.io/ichor/open/#a=10.0.0.5&c=abcdefghijklmnop&h=worker-1&t=node&tab=pods&v=1"
    private val workload = "https://cyrinux.github.io/ichor/open/#c=abcdefghijklmnop&k=Deployment&n=api-v2&ns=web-shop&t=workload&v=1"
    private val etcd = "https://cyrinux.github.io/ichor/open/#c=qrstuvwxyzabcdef&t=etcd&v=1"

    @Test
    fun shortcutLinkIsTheAppFormOfTheShareLink() {
        for (link in listOf(node, workload, etcd)) {
            val app = appShareLink(link)!!
            val uri = URI(app)
            // What MainActivity's shareLink() and the manifest's ichor://open filter take.
            assertEquals("ichor", uri.scheme)
            assertEquals("open", uri.host)
            // ParseShareLink reads the ichor:// query as it reads the https fragment.
            assertEquals(URI(link).rawFragment, uri.rawQuery)
        }
        assertEquals("ichor://open?c=qrstuvwxyzabcdef&t=etcd&v=1", appShareLink(etcd))
    }

    @Test
    fun onlyShareLinksHaveAnAppForm() {
        assertNull(appShareLink("https://example.com/ichor/open/#c=abcdefghijklmnop&t=etcd&v=1"))
        assertNull(appShareLink("https://cyrinux.github.io/ichor/open/#"))
        assertNull(appShareLink("ichor://open?c=abcdefghijklmnop&t=etcd&v=1"))
        assertNull(appShareLink(""))
    }

    @Test
    fun clusterOfTheShortcutLink() {
        assertEquals("abcdefghijklmnop", clusterOfAppLink(appShareLink(node)!!))
        assertEquals("qrstuvwxyzabcdef", clusterOfAppLink(appShareLink(etcd)!!))
        assertNull(clusterOfAppLink("ichor://open?t=etcd&v=1"))
        assertNull(clusterOfAppLink(etcd))
        assertNull(clusterOfAppLink("not a link"))
    }

    @Test
    fun idIsStablePerLink() {
        val id = screenShortcutId(appShareLink(node)!!)
        assertEquals(id, screenShortcutId(appShareLink(node)!!))
        assertNotEquals(id, screenShortcutId(appShareLink(workload)!!))
        assertTrue(id.startsWith("screen-"))
        assertEquals("screen-".length + 40, id.length)
        assertTrue(isScreenShortcut(id))
        assertFalse(isScreenShortcut("cluster-fp-prod"))
    }

    @Test
    fun publishKeepsScreenShortcutsOfClustersOnThePhone() {
        val app = appShareLink(node)!!
        val screen = PinnedShortcut(screenShortcutId(app), app)
        val clusters = setOf("abcdefghijklmnop")
        assertTrue(keepsPinned(screen, current = emptySet(), known = emptySet(), clusters = clusters))
        // Its cluster removed: disabled like a removed cluster's shortcut.
        assertFalse(keepsPinned(screen, emptySet(), emptySet(), setOf("qrstuvwxyzabcdef")))
        assertFalse(keepsPinned(screen.copy(link = null), emptySet(), emptySet(), clusters))
        // Cluster shortcuts keep their rule.
        assertTrue(keepsPinned(PinnedShortcut("cluster-fp-prod", null), setOf("cluster-fp-prod"), emptySet(), emptySet()))
        assertTrue(keepsPinned(PinnedShortcut("cluster-fp-lab", null), emptySet(), setOf("cluster-fp-lab"), emptySet()))
        assertFalse(keepsPinned(PinnedShortcut("cluster-fp-gone", null), emptySet(), emptySet(), clusters))
    }

    @Test
    fun glyphPerKindOfScreen() {
        assertEquals("N", screenGlyph(ShareTarget.NODE, "worker-1"))
        assertEquals("P", screenGlyph(ShareTarget.POD, "api-7d9f"))
        assertEquals("E", screenGlyph(ShareTarget.ETCD, "etcd"))
        assertEquals("!", screenGlyph(ShareTarget.ALERTS, "Alerts"))
        assertEquals("C", screenGlyph(ShareTarget.CHECKUP, "Checkup"))
    }
}
