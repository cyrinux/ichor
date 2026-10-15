package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class VirtualMachinesTest {
    // A KubeVirtualMachines answer (go/ichorgo/kube_virt.go), cut down, with a field from the future.
    private val json = """
        {"installed":true,"harvester":true,"version":"v1","vms":[
          {"namespace":"vms","name":"build-runner","runStrategy":"Always","status":"CrashLoopBackOff","state":"failing",
           "ready":false,"created":false,"pendingChange":"","conditions":[
             {"type":"Failure","status":"True","reason":"CrashLoopBackOff","message":"virt-launcher exited"}],
           "cpu":8,"memory":"32Gi","node":"","phase":"","ips":[],"guestOS":"","launcherImage":"","migration":null,"disks":[]},
          {"namespace":"vms","name":"db-01","runStrategy":"Always","status":"Migrating","state":"changing","ready":true,"created":true,
           "conditions":[{"type":"Ready","status":"True"}],"cpu":4,"memory":"16Gi","node":"worker-2","phase":"Running",
           "ips":["10.244.2.41","fd00::41"],"guestOS":"Rocky Linux 10.0",
           "migration":{"name":"db-01-migration-x7k2p","phase":"Running","sourceNode":"worker-2","targetNode":"worker-3",
             "startedAt":1760000000000,"completed":false,"failed":false},"disks":[]},
          {"namespace":"lab","name":"win11-test","runStrategy":"Always","status":"Provisioning","state":"changing",
           "disks":[{"name":"win11-test-root","phase":"ImportInProgress","progress":"62.00%","importing":true}],"future":1},
          {"namespace":"vms","name":"web-01","runStrategy":"Always","status":"Running","state":"running","ready":true,
           "node":"worker-1","ips":["10.244.1.23"],"guestOS":"Debian GNU/Linux 13 (trixie)"},
          {"namespace":"lab","name":"k3s-sandbox","runStrategy":"Halted","status":"Stopped","state":"stopped"},
          {"namespace":"lab","name":"paused-one","runStrategy":"Always","status":"Paused","state":"changing"}],
         "namespaces":["lab","vms"],
         "counts":{"running":1,"stopped":1,"failing":1,"migrating":1,"importing":1}}
    """.trimIndent()

    private val report = TalosJson.decodeFromString(VirtualMachines.serializer(), json)
    private fun vm(name: String) = report.vms.first { it.name == name }

    @Test
    fun decodesTheReport() {
        assertTrue(report.installed)
        assertTrue(report.harvester)
        assertEquals(6, report.vms.size)
        assertEquals(listOf("lab", "vms"), report.namespaces)
        assertEquals(VmCounts(running = 1, stopped = 1, failing = 1, migrating = 1, importing = 1), report.counts)
        val db = vm("db-01")
        assertEquals("vms/db-01", db.key)
        assertEquals("10.244.2.41", db.firstIp)
        assertEquals("worker-3", db.migration?.targetNode)
        assertEquals(VmState.FAILING, vm("build-runner").vmState)
        assertEquals(VmState.RUNNING, vm("web-01").vmState)
        assertEquals(VmState.STOPPED, vm("k3s-sandbox").vmState)
        assertEquals(VmState.CHANGING, vm("win11-test").vmState)
    }

    @Test
    fun notInstalledDecodesWithDefaults() {
        val empty = TalosJson.decodeFromString(VirtualMachines.serializer(), """{"installed":false}""")
        assertFalse(empty.installed)
        assertTrue(empty.vms.isEmpty())
        assertEquals(VmCounts(), empty.counts)
    }

    @Test
    fun progressShowsAnImportOrAMigrationInFlight() {
        assertEquals(VmProgress.Importing("win11-test-root", "62.00%"), vm("win11-test").progress)
        assertEquals(VmProgress.Migrating("worker-2", "worker-3"), vm("db-01").progress)
        assertNull(vm("web-01").progress)
    }

    @Test
    fun actionsAreAllowedAsTheGoCoreRefusesThem() {
        val web = vm("web-01")
        assertEquals(setOf(VmAction.STOP, VmAction.RESTART, VmAction.MIGRATE, VmAction.PAUSE), VmAction.entries.filter(web::allows).toSet())
        val stopped = vm("k3s-sandbox")
        // runStrategy Halted: a manual restart is refused, start is the way back.
        assertEquals(setOf(VmAction.START), VmAction.entries.filter(stopped::allows).toSet())
        // Its instance runs: start is pointless (stricter than the Go core, which lets KubeVirt refuse it).
        val migrating = vm("db-01")
        assertEquals(setOf(VmAction.STOP, VmAction.PAUSE), VmAction.entries.filter(migrating::allows).toSet())
        val provisioning = vm("win11-test")
        assertEquals(setOf(VmAction.STOP), VmAction.entries.filter(provisioning::allows).toSet())
        val paused = vm("paused-one")
        assertEquals(setOf(VmAction.STOP, VmAction.UNPAUSE), VmAction.entries.filter(paused::allows).toSet())
        assertFalse(web.allows(VmAction.UNPAUSE))
    }

    @Test
    fun actionsMapToTheirAccessChecks() {
        assertEquals(KubeAction.VM_STOP, VmAction.STOP.kubeAction)
        assertEquals(KubeAction.VM_UNPAUSE, VmAction.UNPAUSE.kubeAction)
        assertEquals("unpause", VmAction.UNPAUSE.wire)
    }

    @Test
    fun aMigrationThatEndedIsNoLongerInFlight() {
        val done = vm("db-01").copy(status = "Running", migration = vm("db-01").migration!!.copy(completed = true))
        assertTrue(done.allows(VmAction.MIGRATE))
        assertNull(done.progress)
    }

    @Test
    fun filtersByNamespace() {
        assertEquals(listOf("win11-test", "k3s-sandbox", "paused-one"), report.inNamespace("lab").map { it.name })
        assertEquals(6, report.inNamespace(null).size)
    }

    @Test
    fun busyWhileSomethingChanges() {
        assertTrue(report.busy)
        val calm = report.copy(vms = listOf(vm("web-01"), vm("k3s-sandbox")))
        assertFalse(calm.busy)
    }

    @Test
    fun actionResultDecodes() {
        val r = TalosJson.decodeFromString(VmActionResult.serializer(), """{"action":"migrate","namespace":"vms","name":"web-01","migration":"web-01-migration-abcde"}""")
        assertEquals("web-01-migration-abcde", r.migration)
    }
}
