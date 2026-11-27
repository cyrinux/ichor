package name.levis.ichor.ui.hardware

import org.junit.Assert.assertEquals
import org.junit.Test

class HardwarePartsTest {
    private val always = listOf(
        HardwarePart.SYSTEM, HardwarePart.PROCESSORS, HardwarePart.MEMORY, HardwarePart.DISKS,
        HardwarePart.EXTENSIONS, HardwarePart.SECURITY, HardwarePart.WATCHDOG,
    )

    @Test
    fun aTalosWithoutWatchdogTimersHasNoWatchdogCard() {
        assertEquals(always - HardwarePart.WATCHDOG, hardwareParts(sensors = false, pci = false, watchdog = false))
    }

    @Test
    fun aVirtualMachineHasNeitherSensorsNorPci() {
        assertEquals(always, hardwareParts(sensors = false, pci = false))
    }

    @Test
    fun sensorsComeFirstAndPciLast() {
        assertEquals(listOf(HardwarePart.SENSORS) + always + HardwarePart.PCI, hardwareParts(sensors = true, pci = true))
    }

    @Test
    fun cpuScalingAndNumaFollowTheProcessors() {
        val parts = hardwareParts(sensors = false, pci = false, cpuScaling = true, numa = true)
        assertEquals(
            listOf(HardwarePart.SYSTEM, HardwarePart.PROCESSORS, HardwarePart.CPU_SCALING, HardwarePart.NUMA, HardwarePart.MEMORY),
            parts.take(5),
        )
        assertEquals(always.size + 2, parts.size)
    }

    @Test
    fun keysAreUniqueSoEachCardIsOneItem() {
        assertEquals(HardwarePart.entries.size, HardwarePart.entries.map { it.key }.toSet().size)
    }
}
