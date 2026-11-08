package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class LonghornDiskPathTest {
    private fun node(name: String, vararg paths: String) = LonghornNode(name = name, disks = paths.map { LonghornDisk(path = it) })

    @Test fun sameMountOnEveryNodeIsStatedOnce() {
        val nodes = (1..8).map { node("worker-$it", "/var/mnt/storage/longhorn") }
        assertEquals("/var/mnt/storage/longhorn", nodes.sharedDiskPath())
    }

    @Test fun differentMountsAreNamedPerRow() {
        assertNull(listOf(node("a", "/var/lib/longhorn"), node("b", "/mnt/fast")).sharedDiskPath())
        assertNull(listOf(node("a", "/var/lib/longhorn", "/mnt/fast")).sharedDiskPath())
    }

    @Test fun unknownPathsAreIgnored() {
        assertEquals("/var/lib/longhorn", listOf(node("a", "/var/lib/longhorn"), node("b", "")).sharedDiskPath())
        assertNull(listOf(node("a", ""), node("b")).sharedDiskPath())
        assertNull(emptyList<LonghornNode>().sharedDiskPath())
    }

    @Test fun labelIsTheLastSegment() {
        assertEquals("longhorn", diskPathLabel("/var/mnt/storage/longhorn"))
        assertEquals("longhorn", diskPathLabel("/var/mnt/storage/longhorn/"))
        assertEquals("/", diskPathLabel("/"))
        assertEquals("data", diskPathLabel("data"))
    }
}
