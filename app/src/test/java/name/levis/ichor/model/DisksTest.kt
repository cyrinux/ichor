package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DisksTest {
    private val mib = 1L shl 20
    private val gib = 1L shl 30

    @Test
    fun decodesTheCoreJson() {
        val json = """{"supported":true,"reason":"","disks":[{"name":"sda","devPath":"/dev/sda","size":10737418240,
            "type":"ssd","transport":"virtio","systemDisk":true,"unallocated":2097152,
            "partitions":[{"index":3,"devPath":"/dev/sda3","label":"STATE","volumeId":"STATE","offset":1048576,
            "size":104857600,"filesystem":"xfs","encryption":"enabled","encryptionProvider":"luks2",
            "uuid":"11111111-2222-3333-4444-555555555555","mountedOn":"/system/state","phase":"ready","error":""}],
            "segments":[{"kind":"partition","index":3,"label":"STATE","offset":1048576,"size":104857600},
            {"kind":"free","offset":105906176,"size":2097152}]}]}"""
        val list = TalosJson.decodeFromString(DiskLayoutList.serializer(), json)
        val disk = list.disks.single()
        assertEquals("virtio", disk.transport)
        assertTrue(disk.systemDisk)
        assertEquals(EncryptionState.ENABLED, disk.partitions.single().encryptionState)
        assertTrue(disk.segments[1].isFree)
    }

    @Test
    fun fractionsSumToOneAndWidenTinySlices() {
        val disk = DiskLayout(
            size = 120 * gib,
            segments = listOf(
                DiskSegment("partition", 1, "EFI", mib, gib),
                DiskSegment("partition", 2, "META", gib + mib, mib),
                DiskSegment("partition", 3, "EPHEMERAL", gib + 2 * mib, 118 * gib),
                DiskSegment("free", offset = 119 * gib, size = 2 * mib),
            ),
        )
        val slices = disk.barFractions()
        assertEquals(4, slices.size)
        assertEquals(1f, slices.sumOf { it.fraction.toDouble() }.toFloat(), 1e-4f)
        assertEquals(0.015f, slices[1].fraction, 1e-6f)
        assertEquals(0.015f, slices[3].fraction, 1e-6f)
        assertTrue(slices[2].fraction > slices[0].fraction)
    }

    @Test
    fun noBarWithoutSegmentsAndEvenSplitWhenAllTiny() {
        assertTrue(DiskLayout(size = 0).barFractions().isEmpty())
        assertTrue(DiskLayout(size = gib, segments = listOf(DiskSegment("partition", size = 0))).barFractions().isEmpty())

        // 100 slices cannot all get 1.5 %: each gets an equal share.
        val many = DiskLayout(size = 100 * mib, segments = List(100) { DiskSegment("partition", it + 1, size = mib) })
        val slices = many.barFractions()
        assertEquals(1f, slices.sumOf { it.fraction.toDouble() }.toFloat(), 1e-4f)
        assertEquals(0.01f, slices.first().fraction, 1e-6f)
    }

    @Test
    fun labelFallsBackToTheVolume() {
        assertEquals("EFI", Partition(label = "EFI", volumeId = "X").knownLabel())
        assertEquals("u-data", Partition(volumeId = "u-data").knownLabel())
        assertNull(Partition(index = 7).knownLabel())
    }

    @Test
    fun encryptionStates() {
        assertEquals(EncryptionState.DISABLED, Partition(encryption = "disabled").encryptionState)
        assertEquals(EncryptionState.UNKNOWN, Partition(encryption = "unknown").encryptionState)
        assertEquals(EncryptionState.UNKNOWN, Partition().encryptionState)
    }
}
