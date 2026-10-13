package name.levis.ichor.ui.hardware

import org.junit.Assert.assertEquals
import org.junit.Test

class HardwarePartsTest {
    private val always = listOf(
        HardwarePart.SYSTEM, HardwarePart.PROCESSORS, HardwarePart.MEMORY, HardwarePart.DISKS,
        HardwarePart.EXTENSIONS, HardwarePart.SECURITY,
    )

    @Test
    fun aVirtualMachineHasNeitherSensorsNorPci() {
        assertEquals(always, hardwareParts(sensors = false, pci = false))
    }

    @Test
    fun sensorsComeFirstAndPciLast() {
        assertEquals(listOf(HardwarePart.SENSORS) + always + HardwarePart.PCI, hardwareParts(sensors = true, pci = true))
    }

    @Test
    fun keysAreUniqueSoEachCardIsOneItem() {
        assertEquals(HardwarePart.entries.size, HardwarePart.entries.map { it.key }.toSet().size)
    }
}
