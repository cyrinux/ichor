package name.levis.talosmobile.model

import name.levis.talosmobile.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class StorageTest {

    @Test
    fun usageLevels() {
        assertEquals(UsageLevel.OK, usageLevel(0.0))
        assertEquals(UsageLevel.OK, usageLevel(79.9))
        assertEquals(UsageLevel.WARN, usageLevel(80.0))
        assertEquals(UsageLevel.WARN, usageLevel(89.9))
        assertEquals(UsageLevel.BAD, usageLevel(90.0))
        assertEquals(UsageLevel.BAD, usageLevel(100.0))
    }

    @Test
    fun mountFractionIsClamped() {
        assertEquals(0.5f, Mount(usedPercent = 50.0).usedFraction, 0.0001f)
        assertEquals(1f, Mount(usedPercent = 140.0).usedFraction, 0.0001f)
        assertEquals(0f, Mount(usedPercent = -3.0).usedFraction, 0.0001f)
        assertEquals(UsageLevel.BAD, Mount(usedPercent = 93.0).level)
    }

    @Test
    fun decodesMountsAndNullLists() {
        val json = """{"mounts":[{"filesystem":"/dev/sda4","mountedOn":"/var","size":1000,"available":150,"used":850,"usedPercent":85}]}"""
        val mounts = TalosJson.decodeFromString(MountList.serializer(), json).mounts
        assertEquals(UsageLevel.WARN, mounts.single().level)
        assertTrue(TalosJson.decodeFromString(MountList.serializer(), """{"mounts":null}""").mounts.isEmpty())
    }

    @Test
    fun shortMountListKeepsRealDevicesFullestFirst() {
        val mounts = listOf(
            Mount(filesystem = "proc", mountedOn = "/proc"),
            Mount(filesystem = "tmpfs", mountedOn = "/run", size = 10),
            Mount(filesystem = "/dev/sda4", mountedOn = "/var", size = 10, usedPercent = 20.0),
            Mount(filesystem = "/dev/sda3", mountedOn = "/system/state", size = 10, usedPercent = 60.0),
            Mount(filesystem = "/dev/sda9", mountedOn = "/empty"),
            Mount(filesystem = "/dev/sda4", mountedOn = "/var/lib/kubelet/pods/x/volumes/y", size = 10, usedPercent = 99.0),
        )
        assertEquals(listOf("/system/state", "/var"), mounts.shortList().map { it.mountedOn })
        assertTrue(emptyList<Mount>().shortList().isEmpty())
    }

    @Test
    fun normalizesPaths() {
        assertEquals("/", normalizePath(""))
        assertEquals("/", normalizePath("/"))
        assertEquals("/var/lib", normalizePath("/var/lib/"))
        assertEquals("/var/lib", normalizePath("var//lib"))
    }

    @Test
    fun rowsAreChildrenBySizeWithoutTheRoot() {
        val entries = listOf(
            DiskUsageEntry("/var", 1000, true),
            DiskUsageEntry("/var/log", 100, true),
            DiskUsageEntry("/var/lib", 800, true),
            DiskUsageEntry("/var/run.pid", 4, false),
        )
        val rows = diskUsageRows(entries, "/var/")
        assertEquals(listOf("lib", "log", "run.pid"), rows.map { it.name })
        assertEquals(listOf("/var/lib", "/var/log", "/var/run.pid"), rows.map { it.path })
        assertEquals(1f, rows[0].fraction, 0.0001f)
        assertEquals(0.125f, rows[1].fraction, 0.0001f)
        assertFalse(rows[2].isDir)
        assertEquals(1000, diskUsageTotal(entries, "/var"))
    }

    @Test
    fun rowsAtTheRoot() {
        val entries = listOf(DiskUsageEntry("/var", 10, true), DiskUsageEntry("/etc", 10, true), DiskUsageEntry("/", 20, true))
        val rows = diskUsageRows(entries, "/")
        // Same size: by path.
        assertEquals(listOf("etc", "var"), rows.map { it.name })
        assertEquals(20, diskUsageTotal(entries, "/"))
    }

    @Test
    fun totalWithoutRootEntryIsTheSumOfChildren() {
        val entries = listOf(DiskUsageEntry("/var/log", 100, true), DiskUsageEntry("/var/lib", 800, true))
        assertEquals(900, diskUsageTotal(entries, "/var"))
        assertEquals(0, diskUsageTotal(emptyList(), "/var"))
        assertTrue(diskUsageRows(emptyList(), "/var").isEmpty())
    }

    @Test
    fun emptyEntriesHaveNoBar() {
        val rows = diskUsageRows(listOf(DiskUsageEntry("/var/a", 0, true)), "/var")
        assertEquals(0f, rows.single().fraction, 0.0001f)
    }

    @Test
    fun breadcrumbsLeadBackToTheRoot() {
        assertEquals(listOf(Crumb("/", "/")), breadcrumbs("/"))
        assertEquals(
            listOf(Crumb("/", "/"), Crumb("var", "/var"), Crumb("lib", "/var/lib")),
            breadcrumbs("/var/lib/"),
        )
    }

    @Test
    fun explorerOffersStartPathsQuickestFirst() {
        assertEquals(
            listOf("/var/log", "/var/lib/etcd", "/etc", "/opt", "/system/state", "/var/lib", "/var", "/"),
            DISK_USAGE_SHORTCUTS,
        )
    }

    @Test
    fun diskVerdicts() {
        assertEquals(DiskVerdict.HEALTHY, DiskHealth(healthy = true).verdict)
        assertEquals(DiskVerdict.FAILING, DiskHealth(healthy = false).verdict)
        assertEquals(DiskVerdict.UNKNOWN, DiskHealth().verdict)
        assertEquals(DiskVerdict.FAILING, DiskHealth(healthy = true, criticalWarnings = listOf("spare below threshold")).verdict)
    }

    @Test
    fun decodesDiskHealth() {
        val json = """{"supported":true,"disks":[{"device":"nvme0n1","model":"X","healthy":null,"temperatureC":41,
            "powerOnHours":1234,"wearPercent":3,"criticalWarnings":null,"attributes":[{"k":"media_errors","v":"0"}]}]}"""
        val disk = TalosJson.decodeFromString(DiskHealthReport.serializer(), json).disks.single()
        assertNull(disk.healthy)
        assertEquals(41L, disk.temperatureC)
        assertEquals(1234L, disk.powerOnHours)
        assertEquals("media_errors", disk.attributes.single().k)

        val unsupported = TalosJson.decodeFromString(DiskHealthReport.serializer(), """{"supported":false,"reason":"needs Talos v1.15 or newer"}""")
        assertFalse(unsupported.supported)
        assertEquals(VersionNotice("v1.15"), versionNotice(unsupported.reason))
    }
}
