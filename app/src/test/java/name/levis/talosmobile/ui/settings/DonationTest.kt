package name.levis.talosmobile.ui.settings

import org.junit.Assert.assertEquals
import org.junit.Test

class DonationTest {

    @Test
    fun urisUseWalletSchemes() {
        assertEquals("bitcoin:bc1qc0dhqrgw6z08du94rkfequk8n5r3lgcr5lnxtl", Donation.Bitcoin.uri)
        assertEquals("ethereum:0xb32676301F9c4abD35Eb2e4c7C8cdA754BA29804", Donation.Ethereum.uri)
    }

    @Test
    fun middleEllipsisKeepsBothEnds() {
        assertEquals("bc1qc0dh…cr5lnxtl", middleEllipsis("bc1qc0dhqrgw6z08du94rkfequk8n5r3lgcr5lnxtl"))
        assertEquals("short", middleEllipsis("short"))
    }
}
