package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class VpnOnlyTest {

    @Test
    fun removedClustersAreForgotten() {
        assertEquals(setOf("fp-prod"), keepVpnOnly(setOf("fp-prod", "fp-gone"), listOf("fp-prod", "fp-lab")))
    }

    @Test
    fun blankFingerprintsAreNeverKept() {
        assertEquals(emptySet<String>(), keepVpnOnly(setOf(""), listOf("", "fp-lab")))
    }

    @Test
    fun vpnOnlyClusterIsHeldBackWithoutVpn() {
        assertTrue(heldBackForVpn(setOf("fp-prod"), "fp-prod", vpnUp = false))
    }

    @Test
    fun vpnOnlyClusterIsReachedWithVpn() {
        assertFalse(heldBackForVpn(setOf("fp-prod"), "fp-prod", vpnUp = true))
    }

    @Test
    fun otherClustersAreNeverHeldBack() {
        assertFalse(heldBackForVpn(setOf("fp-prod"), "fp-lab", vpnUp = false))
        assertFalse(heldBackForVpn(setOf(""), "", vpnUp = false))
        assertFalse(heldBackForVpn(setOf("fp-prod"), null, vpnUp = false))
    }
}
