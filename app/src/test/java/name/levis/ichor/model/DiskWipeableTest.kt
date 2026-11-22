package name.levis.ichor.model

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DiskWipeableTest {
    private val stray = Partition(index = 1, devPath = "/dev/sdb1", label = "old-data")

    @Test
    fun aDataDiskWithAStrayPartitionIsWipeable() {
        assertTrue(DiskLayout(name = "sdb", partitions = listOf(stray)).wipeable)
        assertTrue(DiskLayout(name = "sdc").wipeable)
    }

    @Test
    fun theSystemDiskAReadOnlyDiskAndAUsedDiskAreNot() {
        assertFalse(DiskLayout(name = "sda", systemDisk = true).wipeable)
        assertFalse(DiskLayout(name = "sdd", readonly = true).wipeable)
        assertFalse(DiskLayout(name = "sde", partitions = listOf(stray.copy(volumeId = "u-data"))).wipeable)
        assertFalse(DiskLayout(name = "sdf", partitions = listOf(stray.copy(mountedOn = "/var/mnt/data"))).wipeable)
    }
}
