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
}
