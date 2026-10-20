package name.levis.ichor.ui.dataservices

import name.levis.ichor.model.DnsCheckEntry
import name.levis.ichor.model.DnsCheckResult
import name.levis.ichor.model.ExternalDnsRecord
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The check's verdicts pair with the records by position, one per row. */
class DnsCheckStateTest {
    private fun record(type: String, target: String) = ExternalDnsRecord(
        dnsName = "web.example.com", recordType = type, targets = listOf(target),
        sourceKind = "Service", sourceNamespace = "demo", sourceName = "web",
    )

    @Test
    fun theARecordAndTheAaaaRecordOfOneNameEachGetTheirOwnVerdict() {
        val a = record("A", "203.0.113.10")
        val aaaa = record("AAAA", "2001:db8::10")
        val state = DnsCheckState(
            result = DnsCheckResult(
                listOf(
                    DnsCheckEntry(dnsName = "web.example.com", status = "match"),
                    DnsCheckEntry(dnsName = "web.example.com", status = "missing"),
                ),
            ),
            records = listOf(a, aaaa),
        )

        assertEquals("match", state.byRecord[a.rowKey]?.status)
        assertEquals("missing", state.byRecord[aaaa.rowKey]?.status)
    }

    @Test
    fun recordsPastTheCheckedOnesHaveNoVerdict() {
        val a = record("A", "203.0.113.10")
        val aaaa = record("AAAA", "2001:db8::10")
        val state = DnsCheckState(
            result = DnsCheckResult(listOf(DnsCheckEntry(dnsName = "web.example.com", status = "match"))),
            records = listOf(a, aaaa),
        )

        assertNull(state.byRecord[aaaa.rowKey])
    }
}
