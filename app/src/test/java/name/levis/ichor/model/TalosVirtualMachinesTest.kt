package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TalosVirtualMachinesTest {
    // A ClusterVirtualMachines answer (go/ichorgo/talos_vms.go), invented: one node with VMs,
    // one 1.15 node without, one older node, one that did not answer; a field from the future.
    private val json = """
        {"context":"lab","nodes":[
          {"node":"192.0.2.12","hostname":"hv-2","supported":true,"reason":"","vms":[],"libraries":[],"errors":{}},
          {"node":"192.0.2.11","hostname":"hv-1","supported":true,"reason":"","errors":{"libraries":"permission denied"},
           "libraries":[{"name":"images","path":"/var/mnt/images","phase":"ready","error":""}],"vms":[
            {"name":"ci-runner","powerState":"running","wantedPowerState":"running","stage":"ready","error":"",
             "running":true,"cpus":4,"memoryBytes":8589934592,"profile":"","addresses":["198.51.100.7/24"],
             "interfaces":[{"name":"eth0","mac":"52:54:00:aa:bb:01","addresses":["198.51.100.7/24"]}],
             "disks":[{"name":"root","pool":"pool-a","volume":"ci-runner-root","format":"qcow2",
               "sizeBytes":42949672960,"readOnly":false,"phase":"ready","error":"","future":true}]},
            {"name":"old-app","powerState":"stopped","wantedPowerState":"stopped","stage":"ready","running":false},
            {"name":"desk","powerState":"stopped","wantedPowerState":"running","stage":"error","error":"no image",
             "disks":[{"name":"root","phase":"notReady","error":"no image"}]},
            {"name":"booting","powerState":"stopped","wantedPowerState":"running","stage":"pending"}]},
          {"node":"192.0.2.13","hostname":"old-1","supported":false,
           "reason":"needs Talos v1.15 or newer (this node runs v1.14.2)","vms":[],"libraries":[],"errors":{}},
          {"node":"192.0.2.14","hostname":"192.0.2.14","supported":false,"vms":[],"libraries":[],"errors":{},
           "error":"node 192.0.2.14: connection refused"}]}
    """.trimIndent()

    private fun cluster() = TalosJson.decodeFromString(TalosClusterVms.serializer(), json)

    @Test
    fun decodesTheReport() {
        val c = cluster()
        assertEquals("lab", c.context)
        assertEquals(4, c.nodes.size)
        val hv1 = c.nodes.first { it.hostname == "hv-1" }
        assertTrue(hv1.supported)
        assertEquals("permission denied", hv1.errors["libraries"])
        assertEquals("/var/mnt/images", hv1.libraries.single().path)
        val runner = hv1.vms.first()
        assertEquals(4L, runner.cpus)
        assertEquals(8L shl 30, runner.memoryBytes)
        assertEquals("198.51.100.7", runner.firstAddress)
        assertEquals("52:54:00:aa:bb:01", runner.interfaces.single().mac)
        assertEquals("pool-a", runner.disks.single().pool)
        assertEquals(40L shl 30, runner.disks.single().sizeBytes)
        assertEquals("node 192.0.2.14: connection refused", c.nodes.last().error)
    }

    @Test
    fun states() {
        val vms = cluster().all
        assertEquals(
            mapOf("ci-runner" to VmState.RUNNING, "old-app" to VmState.STOPPED, "desk" to VmState.FAILING, "booting" to VmState.CHANGING),
            vms.associate { it.name to it.vmState },
        )
        assertEquals(TalosVmCounts(running = 1, stopped = 1, failing = 1, changing = 1), talosVmCounts(vms))
        assertTrue(cluster().busy)
    }

    @Test
    fun shownNodesAndTheEntryRule() {
        val c = cluster()
        // The empty 1.15 node is left out; the older and the failed nodes say why.
        assertEquals(listOf("192.0.2.14", "hv-1", "old-1"), c.shown.map { it.hostname })
        assertTrue(c.hasVms)
        val empty = TalosClusterVms.ofNode("192.0.2.12", "hv-2", TalosNodeVms(supported = true))
        assertFalse(empty.hasVms)
        assertTrue(empty.shown.isEmpty())
        val old = TalosNodeVms(supported = false, reason = "needs Talos v1.15 or newer")
        assertFalse(TalosClusterVms.ofNode("192.0.2.13", "old-1", old).hasVms)
        assertEquals(VersionNotice("v1.15"), versionNotice(old.reason))
    }

    @Test
    fun missingFields() {
        val node = TalosJson.decodeFromString(TalosNodeVms.serializer(), """{"vms":[{"name":"x"}]}""")
        assertFalse(node.supported)
        assertFalse(node.hasVms)
        assertEquals("-", node.vms.single().stateLabel)
        assertEquals(VmState.STOPPED, node.vms.single().vmState)
        assertNull(node.vms.single().firstAddress)
    }
}
