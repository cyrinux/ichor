package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Test

class DonationAddressTest {

    @Test
    fun addressesAreExact() {
        assertEquals("bc1qc0dhqrgw6z08du94rkfequk8n5r3lgcr5lnxtl", BTC_ADDRESS)
        assertEquals("0xb32676301F9c4abD35Eb2e4c7C8cdA754BA29804", ETH_ADDRESS)
    }
}
