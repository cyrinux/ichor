package name.levis.ichor.ui.hardware

import androidx.annotation.StringRes
import name.levis.ichor.R

/** The cards of the Hardware screen, in display order; each one is a section of its rail. */
enum class HardwarePart(val key: String, @StringRes val title: Int) {
    SENSORS("sensors", R.string.hardware_sensors),
    SYSTEM("system", R.string.hardware_system),
    PROCESSORS("processors", R.string.hardware_processors),
    MEMORY("memory", R.string.hardware_memory),
    DISKS("disks", R.string.hardware_disks),
    EXTENSIONS("extensions", R.string.hardware_extensions),
    SECURITY("security", R.string.hardware_security),
    PCI("pci", R.string.hardware_pci),
}

/**
 * The cards shown, in order: sensors first when there are some (or their read failed), PCI last
 * when there are devices; the hardware cards always. One list drives both the `LazyColumn` items
 * and the section rail, so a card's position is its rail index.
 */
fun hardwareParts(sensors: Boolean, pci: Boolean): List<HardwarePart> = HardwarePart.entries.filter {
    when (it) {
        HardwarePart.SENSORS -> sensors
        HardwarePart.PCI -> pci
        else -> true
    }
}
