package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GarageLayoutTest {
    private val a = "a".repeat(64)
    private val b = "b".repeat(64)
    private val c = "c".repeat(64)
    private val d = "d".repeat(64)

    // Shaped like KubeGarageLayout's answer, with a field the app does not know.
    private val layoutJson = """
        {"version":12,"nextVersion":13,"partitionSize":1835008000,"zoneRedundancy":"maximum","stagedZoneRedundancy":"2","futureField":true,
         "preview":{"ok":true,"error":"","messages":["dc3: 256 partitions moved in"],"movedPartitions":256,"usableCapacity":3000,"effectiveCapacity":1000,"totalCapacity":4000,"lowUsableCapacity":true,"lowPartitionSize":false},
         "nodes":[
          {"id":"$a","hostname":"garage-a","kubeNode":"w1","up":false,"known":true,"draining":false,"dataTotal":1100,"role":{"zone":"dc1","capacity":1000,"tags":["a"],"storedPartitions":256,"usableCapacity":900},"staged":"none","stagedRole":null},
          {"id":"$b","hostname":"","kubeNode":"","up":true,"known":true,"dataTotal":0,"role":{"zone":"dc2","capacity":1000,"tags":[]},"staged":"none"},
          {"id":"$c","hostname":"garage-c","up":true,"known":true,"role":{"zone":"dc2","capacity":-1,"tags":["gw"]},"staged":"remove","stagedRole":null},
          {"id":"$d","hostname":"garage-d","up":true,"known":true,"dataTotal":2200,"role":null,"staged":"add","stagedRole":{"zone":"dc3","capacity":2000,"tags":["d"]}}],
         "versions":[{"version":12,"status":"current","storageNodes":2,"gatewayNodes":1},{"version":11,"status":"draining","storageNodes":2,"gatewayNodes":1},{"version":10,"status":"someday","storageNodes":1,"gatewayNodes":0}],
         "minAck":11,"trackers":[{"id":"$a","hostname":"garage-a","ack":11,"sync":11,"syncAck":11}],"stuck":true,"messages":[]}
    """.trimIndent()

    @Test
    fun decodesTheLayout() {
        val layout = TalosJson.decodeFromString(GarageLayout.serializer(), layoutJson)
        assertEquals(12L, layout.version)
        assertTrue(layout.hasStaged)
        assertEquals("2", layout.stagedZoneRedundancy)
        assertEquals(256L, layout.preview!!.movedPartitions)
        assertTrue(layout.preview!!.lowUsableCapacity)

        assertEquals(listOf("garage-a", "b".repeat(16), "garage-c"), layout.members.map { it.label })
        assertEquals(2, layout.storageNodes)
        assertEquals(1, layout.gatewayNodes)
        assertEquals(listOf("garage-c", "garage-d"), layout.staged.map { it.label })
        assertTrue(layout.candidates.isEmpty())
        assertEquals(listOf("dc1", "dc2", "dc3"), layout.zones)

        val gateway = layout.node(c)!!
        assertTrue(gateway.role!!.gateway)
        assertEquals(GarageStagedChange.REMOVE, gateway.stagedKind)
        assertNull(gateway.effectiveRole)

        val joining = layout.node(d)!!
        assertEquals(GarageStagedChange.ADD, joining.stagedKind)
        assertEquals("dc3", joining.effectiveRole!!.zone)
        assertEquals(0.9f, layout.node(a)!!.role!!.usableFraction, 0.001f)

        assertEquals(listOf(GarageLayoutStatus.CURRENT, GarageLayoutStatus.DRAINING, GarageLayoutStatus.UNKNOWN), layout.versions.map { it.statusKind })
        assertTrue(layout.stuck)
        assertEquals("garage-a", layout.trackers[0].label)
    }

    @Test
    fun nothingStagedWhenNextVersionIsZero() {
        val layout = GarageLayout(version = 3, nodes = listOf(GarageLayoutNode(id = a, known = true, up = true)))
        assertFalse(layout.hasStaged)
        assertEquals(1, layout.candidates.size)
        assertTrue(layout.members.isEmpty())
    }

    @Test
    fun capacityConversions() {
        assertEquals(1L shl 40, garageCapacityBytes("1", GarageCapacityUnit.TIB))
        assertEquals((1.5 * (1L shl 40)).toLong(), garageCapacityBytes("1,5", GarageCapacityUnit.TIB))
        assertEquals(450L shl 30, garageCapacityBytes(" 450 ", GarageCapacityUnit.GIB))
        assertNull(garageCapacityBytes("", GarageCapacityUnit.GIB))
        assertNull(garageCapacityBytes("0", GarageCapacityUnit.GIB))
        assertNull(garageCapacityBytes("-2", GarageCapacityUnit.GIB))
        assertNull(garageCapacityBytes("big", GarageCapacityUnit.GIB))

        assertEquals("1.5", garageCapacityText((1.5 * (1L shl 40)).toLong(), GarageCapacityUnit.TIB))
        assertEquals("450", garageCapacityText(450L shl 30, GarageCapacityUnit.GIB))
        assertEquals(GarageCapacityUnit.TIB, GarageCapacityUnit.forBytes(1L shl 40))
        assertEquals(GarageCapacityUnit.GIB, GarageCapacityUnit.forBytes(500L shl 30))
    }

    @Test
    fun suggestedCapacityStaysBelowTheDisk() {
        assertEquals(499L shl 30, garageSuggestedCapacity(500L shl 30))
        assertEquals(1L shl 40, garageSuggestedCapacity((1L shl 40) + (200L shl 30)))
        assertEquals(1L shl 40, garageSuggestedCapacity(2L shl 40))
        assertEquals(0L, garageSuggestedCapacity(0))
        assertEquals(1L shl 30, garageSuggestedCapacity(1L shl 30))
    }
}
