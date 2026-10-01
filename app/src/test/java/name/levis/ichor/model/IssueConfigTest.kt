package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class IssueConfigTest {

    @Test
    fun renewalKeepsIssuableRolesInOrder() {
        assertEquals(listOf("os:admin"), renewalRoles(listOf("os:admin")))
        assertEquals(listOf("os:operator", "os:etcd:backup"), renewalRoles(listOf("os:etcd:backup", "os:operator", "os:impersonator")))
        assertEquals(emptyList<String>(), renewalRoles(emptyList()))
    }

    @Test
    fun toggleKeepsCanonicalOrder() {
        val roles = DEFAULT_SHARED_ROLES.toggled("os:admin")
        assertEquals(listOf("os:admin", "os:reader"), roles)
        assertEquals(listOf("os:admin"), roles.toggled("os:reader"))
        assertEquals("os:admin,os:reader", rolesArgument(roles))
    }

    @Test
    fun validityPresetsAreWithinGoLimits() {
        CertValidity.entries.forEach { assertTrue(it.name, it.hours in 1..MAX_CERT_TTL_HOURS) }
        assertEquals(CertValidity.YEAR_1, CertValidity.DEFAULT)
        assertEquals(30L * 86_400_000L, CertValidity.DAYS_30.expiresAt(0))
    }

    @Test
    fun qrCapacity() {
        assertTrue(fitsInQr("a".repeat(MAX_QR_BYTES)))
        assertFalse(fitsInQr("a".repeat(MAX_QR_BYTES + 1)))
        // Counted in UTF-8 bytes, as the importer reads the payload back.
        assertFalse(fitsInQr("é".repeat(MAX_QR_BYTES / 2 + 1)))
    }

    @Test
    fun fileNames() {
        assertEquals("talosconfig-lab-reader.yaml", issuedFileName("lab", listOf("os:reader")))
        assertEquals("talosconfig-my_lab-operator-etcd-backup.yaml", issuedFileName("my lab", listOf("os:operator", "os:etcd:backup")))
        assertEquals("talosconfig-cluster.yaml", issuedFileName("", emptyList()))
    }
}
