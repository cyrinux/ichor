package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class CgroupsTest {

    private fun report(at: Long, etcdCpu: Long, kubeletCpu: Long, etcdIo: Long = 0) = CgroupReport(
        at = at,
        root = CgroupNode(
            name = ".",
            children = listOf(
                CgroupNode(
                    name = "podruntime",
                    memCurrent = 300,
                    children = listOf(
                        CgroupNode("etcd", "service", memCurrent = 200, cpuUsec = etcdCpu, ioWrite = etcdIo, pressure = CgroupPressure(io = CgroupPsi(some10 = 30.0))),
                        CgroupNode("kubelet", "service", memCurrent = 100, cpuUsec = kubeletCpu),
                    ),
                ),
                CgroupNode(name = "init", kind = "service", memCurrent = 50),
            ),
        ),
    )

    @Test
    fun decodesGoJson() {
        val json = """{"at":1,"pressure":{"cpu":{"some10":1.5,"some60":1,"full10":0,"full60":0},"memory":{"some10":0,"some60":0,"full10":0,"full60":0},"io":{"some10":0,"some60":0,"full10":0,"full60":0}},
            "hotspots":[{"resource":"io","name":"etcd","parent":"podruntime","some10":7}],"alerts":[{"kind":"oomKill","name":"apid","parent":"system","count":2}],
            "root":{"name":".","kind":"group","children":[{"name":"init","kind":"service","memCurrent":10}]}}"""
        val r = TalosJson.decodeFromString(CgroupReport.serializer(), json)
        assertEquals(1.5, r.pressure.cpu.some10, 0.0)
        assertEquals("etcd", r.hotspots.single().name)
        assertEquals(2L, r.alerts.single().count)
        assertEquals("init", r.root!!.children.single().name)
    }

    @Test
    fun collapsedTreeShowsTopGroupsOnly() {
        val rows = cgroupRows(null, report(1_000, 0, 0), emptySet(), CgroupSort.MEMORY)
        assertEquals(listOf("podruntime", "init"), rows.map { it.node.name })
        assertEquals(listOf(0, 0), rows.map { it.depth })
    }

    @Test
    fun cpuAndIoAreRatesBetweenSamples() {
        val previous = report(10_000, etcdCpu = 1_000_000, kubeletCpu = 0, etcdIo = 1_000)
        val current = report(12_000, etcdCpu = 2_000_000, kubeletCpu = 4_000_000, etcdIo = 5_000)
        val rows = cgroupRows(previous, current, setOf("podruntime"), CgroupSort.CPU)
        assertEquals(listOf("podruntime", "kubelet", "etcd", "init"), rows.map { it.node.name })
        val etcd = rows.first { it.node.name == "etcd" }
        // 1 CPU second over 2 wall seconds; 4000 bytes over 2 seconds.
        assertEquals(50.0, etcd.cpuPercent!!, 1e-9)
        assertEquals(2_000.0, etcd.ioPerSecond!!, 1e-9)
        assertEquals(200.0, rows.first { it.node.name == "kubelet" }.cpuPercent!!, 1e-9)
        assertEquals(1, etcd.depth)
    }

    @Test
    fun noPreviousOrCounterResetIsZero() {
        assertNull(cgroupRows(null, report(2_000, 5, 5), setOf("podruntime"), CgroupSort.CPU)[1].cpuPercent)
        val rows = cgroupRows(report(1_000, 9_000_000, 0), report(2_000, 1, 0), setOf("podruntime"), CgroupSort.MEMORY)
        assertNull(rows.first { it.node.name == "etcd" }.cpuPercent)
    }

    @Test
    fun pressureSortPutsStalledFirst() {
        val rows = cgroupRows(null, report(1, 0, 0), setOf("podruntime"), CgroupSort.PRESSURE)
        assertEquals("etcd", rows[1].node.name)
    }

    @Test
    fun pressureLevels() {
        assertEquals(PressureLevel.OK, pressureLevel(4.99))
        assertEquals(PressureLevel.WARN, pressureLevel(5.0))
        assertEquals(PressureLevel.BAD, pressureLevel(20.0))
    }

    @Test
    fun defaultExpansionKeepsKubepodsClosed() {
        val r = CgroupReport(
            at = 0,
            root = CgroupNode(
                ".",
                children = listOf(
                    CgroupNode("kubepods", children = listOf(CgroupNode("burstable", children = listOf(CgroupNode("ns/p", "pod"))), CgroupNode("ns/g", "pod"))),
                    CgroupNode("init", "service"),
                ),
            ),
        )
        val rows = cgroupRows(null, r, defaultExpandedCgroups(r), CgroupSort.MEMORY)
        // kubepods stays closed: its pods are the Pods tab's.
        assertEquals(listOf("kubepods", "init"), rows.map { it.node.name })
    }
}
