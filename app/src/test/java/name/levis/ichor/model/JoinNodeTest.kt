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
}
