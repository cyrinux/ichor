package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NodeFilesTest {

    @Test
    fun crumbsGoFromTheRootToTheDirectory() {
        assertEquals(
            listOf(NodePathCrumb("/", "/"), NodePathCrumb("var", "/var"), NodePathCrumb("log", "/var/log")),
            NodePaths.crumbs("/var/log"),
        )
        assertEquals(listOf(NodePathCrumb("/", "/")), NodePaths.crumbs("/"))
    }

    @Test
    fun aTypedPathMustBeAbsoluteWithoutClimbing() {
        assertEquals("/etc/resolv.conf", NodePaths.typed("  /etc/resolv.conf "))
        assertEquals("/var/log/", NodePaths.typed("/var/log/"))
        assertNull(NodePaths.typed("etc/hosts"))
        assertNull(NodePaths.typed("/var/log/../../etc"))
        assertNull(NodePaths.typed(""))
        // A name that only starts with dots is not "..".
        assertEquals("/run/..data", NodePaths.typed("/run/..data"))
    }

    @Test
    fun aSharedCopyKeepsItsExtensionOrBecomesText() {
        assertEquals("config.yaml", NodePaths.shareName("/system/state/config.yaml"))
        assertEquals("route.txt", NodePaths.shareName("/proc/net/route"))
        assertEquals("a_b.log", NodePaths.shareName("/var/log/a b.log"))
    }

    @Test
    fun entriesDecodeWithTheirKinds() {
        val listing = TalosJson.decodeFromString(
            NodeDirListing.serializer(),
            """{"dir":"/var/log","roots":["/var/log"],"entries":[
                {"name":"pods","path":"/var/log/pods","type":"dir","size":0,"modified":"","target":""},
                {"name":"cur","path":"/var/log/cur","type":"link","size":0,"modified":"","target":"/x"}
            ],"truncated":true}""",
        )
        assertTrue(listing.truncated)
        assertTrue(listing.entries[0].isDir && !listing.entries[0].isLink)
        assertTrue(listing.entries[1].isLink && !listing.entries[1].isDir)
        val file = TalosJson.decodeFromString(NodeFileText.serializer(), """{"path":"/var/log/lastlog","binary":true}""")
        assertTrue(file.binary)
        assertFalse(file.truncated)
    }
}
