package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The wire format of go/ichorgo/dns_check.go, as the ExternalDNS tab reads it back. */
class DnsCheckTest {
    private val json = """
        {"results":[
         {"dnsName":"app.example.com","status":"match","answers":["203.0.113.10"],"owner":"homelab","ownerMatch":true},
         {"dnsName":"status.example.com","status":"unverified","answers":["203.0.113.99"]},
         {"dnsName":"gone.example.com","status":"missing","answers":[]},
         {"dnsName":"later.example.com","status":"someday"}],
         "resolver":"system"}
    """.trimIndent()

    private val result = TalosJson.decodeFromString(DnsCheckResult.serializer(), json)

    @Test
    fun aNameWithNoTargetReadsUnverifiedWithItsAnswers() {
        val entry = result.results[1]
        assertEquals(DnsCheckStatus.UNVERIFIED, entry.verdict)
        assertEquals(listOf("203.0.113.99"), entry.answers)
    }

    @Test
    fun aStatusFromANewerCoreReadsAsNoVerdict() {
        assertEquals(listOf(DnsCheckStatus.MATCH, DnsCheckStatus.UNVERIFIED, DnsCheckStatus.MISSING, null), result.results.map { it.verdict })
        assertNull(result.results[3].ownerMatch)
    }
}
