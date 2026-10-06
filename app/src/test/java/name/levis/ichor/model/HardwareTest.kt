package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class HardwareTest {

    @Test
    fun totalsAndDiskOrder() {
        val hw = NodeHardware(
            processors = listOf(ProcessorInfo(cores = 8, threads = 16), ProcessorInfo(cores = 8, threads = 16)),
            memory = listOf(MemoryModule(sizeMib = 16_384), MemoryModule(sizeMib = 16_384)),
            disks = listOf(DiskInfo("sda"), DiskInfo("nvme0n1", systemDisk = true), DiskInfo("sdb")),
        )
        assertEquals(32L * 1024 * 1024 * 1024, hw.totalMemoryBytes)
        assertEquals(listOf("nvme0n1", "sda", "sdb"), hw.sortedDisks.map { it.name })
    }

    @Test
    fun decodesPartialGoJson() {
        val json = """
            {"system":null,"processors":[],"memory":[{"slot":"DIMM 0","bank":"","sizeMib":8192,"type":"","speed":3200,"manufacturer":"","serial":""}],
             "disks":[{"name":"sda","devPath":"/dev/sda","model":"QEMU","serial":"","size":10737418240,"type":"hdd","wwid":"","busPath":"","systemDisk":true,"readonly":false}],
             "extensions":[],"security":null,"errors":{"system":"not found","security":"not found"}}
        """.trimIndent()
        val hw = TalosJson.decodeFromString(NodeHardware.serializer(), json)
        assertNull(hw.system)
        assertNull(hw.security)
        assertEquals(10_737_418_240L, hw.disks.single().size)
        assertEquals(3200, hw.memory.single().speed)
        assertEquals("not found", hw.errors[HardwareSection.SYSTEM])
    }
}
