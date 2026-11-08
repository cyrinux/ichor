package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class JoinNodeTest {
    @Test
    fun decodesAnInspection() {
        val got = TalosJson.decodeFromString(
            MaintenanceInspection.serializer(),
            """{"address":"192.168.1.42","version":"v1.14.2","arch":"amd64","platform":"metal",
               "system":{"manufacturer":"Sample Systems","product":"Box 1","version":"","serial":"S-0001","uuid":"","sku":"","biosVersion":""},
               "disks":[{"name":"nvme0n1","devPath":"/dev/nvme0n1","model":"Sample NVMe","serial":"","size":549755813888,
                         "type":"nvme","wwid":"","busPath":"","systemDisk":false,"readonly":false}],
               "links":[{"name":"enp1s0","type":"ether","kind":"","state":"up","hardwareAddr":"02:00:00:00:00:42","mtu":1500,"speedMbit":1000,"virtual":false},
                        {"name":"enp2s0","type":"ether","kind":"","state":"down","hardwareAddr":"02:00:00:00:00:43","mtu":1500,"speedMbit":0,"virtual":false}],
               "addresses":[{"address":"192.168.1.42/24","link":"enp1s0","family":"inet4","scope":"global","virtual":false},
                            {"address":"fd00::42/64","link":"enp1s0","family":"inet6","scope":"global","virtual":false}],
               "maintenance":true,"errors":{}}""",
        )
        assertTrue(got.maintenance)
        assertEquals("v1.14.2", got.version)
        assertEquals("Sample Systems", got.system?.manufacturer)
        assertEquals(549755813888L, got.disks.single().size)
        assertEquals(listOf("192.168.1.42/24", "fd00::42/64"), got.addressesOn("enp1s0"))
        assertEquals(emptyList<String>(), got.addressesOn("enp2s0"))
    }

    @Test
    fun anInstalledNodeIsNotInMaintenance() {
        val got = TalosJson.decodeFromString(
            MaintenanceInspection.serializer(),
            """{"address":"192.168.1.10","version":"","arch":"","platform":"","system":null,"disks":[],"links":[],"addresses":[],"maintenance":false,"errors":{}}""",
        )
        assertFalse(got.maintenance)
        assertNull(got.system)
    }

    @Test
    fun theTypedAddressIsTrimmed() {
        assertEquals("192.168.1.42", joinAddress("  192.168.1.42 \n"))
        assertEquals("", joinAddress("   "))
    }

    @Test
    fun decodesAConfigPreview() {
        val got = TalosJson.decodeFromString(
            JoinConfigPreview.serializer(),
            """{"redactedYaml":"machine:\n    token: ******\n","warnings":["DHCP"],"needsBootstrap":false,"size":4096,
               "role":"worker","hostname":"worker-3","template":"192.0.2.13"}""",
        )
        assertEquals("worker-3", got.hostname)
        assertEquals(4096, got.size)
        assertEquals(listOf("DHCP"), got.warnings)
    }

    private fun node(addr: String, hostname: String, role: String, reachable: Boolean = true) =
        NodeOverview(node = addr, hostname = hostname, reachable = reachable, role = role)

    @Test
    fun templatesAreReachableMembersWorkersFirst() {
        val nodes = listOf(
            node("192.0.2.11", "cp-1", "controlplane"),
            node("192.0.2.12", "worker-2", "worker"),
            node("192.0.2.13", "worker-1", "worker"),
            node("192.0.2.14", "worker-9", "worker", reachable = false),
            node("192.0.2.15", "odd", "unknown"),
            node("192.0.2.16", "cp-0", "init"),
        )
        assertEquals(listOf("worker-1", "worker-2", "cp-0", "cp-1"), joinTemplates(nodes).map { it.hostname })
    }

    @Test
    fun theSuggestedHostnameIsTheFirstFree() {
        assertEquals("worker-1", suggestedHostname("worker", emptyList()))
        assertEquals("worker-3", suggestedHostname("worker", listOf("worker-1", "worker-2", "worker-4")))
        assertEquals("controlplane-2", suggestedHostname("init", listOf("controlplane-1")))
    }

    @Test
    fun installDisksAreWritableOnes() {
        val disks = listOf(
            DiskInfo(name = "sda", devPath = "/dev/sda"),
            DiskInfo(name = "sr0", devPath = "/dev/sr0", readonly = true),
            DiskInfo(name = "loop0"),
        )
        assertEquals(listOf("/dev/sda"), installDisks(disks))
    }

    @Test
    fun theDiskMustBeOneOfTheInspectedNode() {
        val node = MaintenanceInspection(address = "10.0.0.5", maintenance = true, disks = listOf(DiskInfo(name = "sda", devPath = "/dev/sda")))
        assertTrue(joinDiskFits("/dev/sda", node))
        assertFalse(joinDiskFits("/dev/nvme0n1", node))
        assertTrue(joinDiskFits("/dev/vda", node.copy(disks = emptyList())))
    }

    @Test
    fun decodesJoinProgress() {
        val got = TalosJson.decodeFromString(
            JoinProgress.serializer(),
            """{"phase":"rebooting","message":"installing to /dev/sda and rebooting","at":1700000000000,"address":"192.0.2.20"}""",
        )
        assertEquals(JoinPhase.REBOOTING, JoinPhase.of(got.phase))
        assertEquals("192.0.2.20", got.address)
        assertNull(JoinPhase.of("unknown"))
    }

    private fun progress(phase: String, message: String = phase, at: Long = 1) = JoinProgress(phase, message, at)

    @Test
    fun theTimelineMarksTheLatestPhaseCurrent() {
        val steps = joinTimeline(
            listOf(progress("applying", at = 1), progress("installing", at = 2), progress("rebooting", "down", 3), progress("rebooting", "still down", 4)),
            finished = false,
            failed = false,
        )
        assertEquals(JoinPhase.maintenanceRun, steps.map { it.phase })
        assertEquals(
            listOf(StepStatus.DONE, StepStatus.DONE, StepStatus.CURRENT, StepStatus.PENDING, StepStatus.PENDING, StepStatus.PENDING, StepStatus.PENDING),
            steps.map { it.status },
        )
        val rebooting = steps[JoinPhase.maintenanceRun.indexOf(JoinPhase.REBOOTING)]
        assertEquals(3L, rebooting.at)
        assertEquals("still down", rebooting.message)
    }

    @Test
    fun aFailedJoinFailsItsLatestPhaseAndADoneOneIsAllDone() {
        val failed = joinTimeline(listOf(progress("applying"), progress("installing")), finished = true, failed = true)
        assertEquals(StepStatus.FAILED, failed[JoinPhase.maintenanceRun.indexOf(JoinPhase.INSTALLING)].status)
        assertEquals(StepStatus.PENDING, failed[JoinPhase.maintenanceRun.indexOf(JoinPhase.BOOTED)].status)

        val refused = joinTimeline(emptyList(), finished = true, failed = true)
        assertEquals(StepStatus.FAILED, refused.first().status)

        val done = joinTimeline(listOf(progress("applying"), progress("done")), finished = true, failed = false)
        assertTrue(done.all { it.status == StepStatus.DONE })
    }

    @Test
    fun aCloudRunKeepsItsOwnStepsAndItsFailureOnItsPhase() {
        val running = joinTimeline(listOf(progress("creating"), progress("starting")), finished = false, failed = false, phases = JoinPhase.cloudRun)
        assertEquals(JoinPhase.cloudRun, running.map { it.phase })
        assertEquals(StepStatus.DONE, running[0].status)
        assertEquals(StepStatus.CURRENT, running[1].status)

        // The deletion after a failure has no step of its own.
        val failed = joinTimeline(listOf(progress("creating"), progress("cleanup")), finished = true, failed = true, phases = JoinPhase.cloudOmniRun)
        assertEquals(JoinPhase.cloudOmniRun, failed.map { it.phase })
        assertEquals(StepStatus.FAILED, failed[0].status)
    }

    @Test
    fun aSnapshotRunFollowsItsSteps() {
        val phases = JoinPhase.snapshotRun
        val running = joinTimeline(listOf(progress("key"), progress("creating"), progress("rescue"), progress("writing")), finished = false, failed = false, phases = phases)
        assertEquals(phases, running.map { it.phase })
        assertEquals(StepStatus.DONE, running[phases.indexOf(JoinPhase.RESCUE)].status)
        assertEquals(StepStatus.CURRENT, running[phases.indexOf(JoinPhase.WRITING)].status)
        assertEquals(StepStatus.PENDING, running[phases.indexOf(JoinPhase.CLEANUP)].status)
    }

    @Test
    fun theSnapshotLocationsAreThoseOfTheArchitecture() {
        val options = HetznerOptions(
            serverTypes = listOf(
                HetznerServerType(name = "cx23", arch = "amd64", prices = listOf(HetznerPrice(location = "fsn1"))),
                HetznerServerType(name = "cax11", arch = "arm64", prices = listOf(HetznerPrice(location = "hel1"))),
            ),
            locations = listOf(HetznerLocation(name = "fsn1"), HetznerLocation(name = "hel1")),
        )
        assertEquals(listOf("fsn1"), options.locationsFor("amd64").map { it.name })
        assertEquals(listOf("hel1"), options.locationsFor("arm64").map { it.name })
    }
}
