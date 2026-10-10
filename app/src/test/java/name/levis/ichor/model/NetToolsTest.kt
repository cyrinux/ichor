package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NetToolsTest {
    private fun result(json: String) = TalosJson.decodeFromString(NetToolResult.serializer(), json)

    @Test
    fun dnsAndHttpResultsDecode() {
        val dns = result(
            """{"tool":"dns","target":"kubernetes.default.svc.cluster.local","ok":true,"exitCode":0,
               "dns":{"status":"NOERROR","records":[{"name":"kubernetes.default.svc.cluster.local.","type":"A","ttl":30,"value":"10.96.0.1"}],
               "server":"10.96.0.10","queryMs":2},"raw":"..."}""",
        )
        assertEquals("10.96.0.1", dns.dns!!.records.single().value)
        assertEquals(30, dns.dns!!.records.single().ttl)
        assertNull(dns.ping)

        val http = result(
            """{"tool":"http","target":"https://www.example.test","ok":false,"exitCode":0,
               "http":{"status":200,"redirectUrl":"","totalMs":84.2,"tls":true,"tlsOk":false,"subject":"CN = www.example.test",
               "issuer":"CN = Example CA","notBefore":"2026-09-01T00:00:00Z","notAfter":"2026-11-30T23:59:59Z","daysLeft":51},"raw":""}""",
        )
        assertTrue(http.http!!.tls)
        assertFalse(http.http!!.tlsOk)
        assertEquals(51, http.http!!.daysLeft)
    }

    @Test
    fun traceMarksSilentHops() {
        val trace = result(
            """{"tool":"trace","target":"203.0.113.5","ok":true,"trace":[
               {"hop":1,"host":"10.0.0.1","lossPct":0,"avgMs":0.3},{"hop":2,"host":"???","lossPct":100,"avgMs":0}],"raw":""}""",
        )
        assertEquals(listOf(false, true), trace.trace!!.map { it.silent })
    }

    @Test
    fun targetsAreCheckedPerTool() {
        assertEquals(NetTargetProblem.EMPTY, netTargetProblem(NetTool.DNS, "  "))
        assertEquals(NetTargetProblem.CHARACTERS, netTargetProblem(NetTool.DNS, "example.test; reboot"))
        assertEquals(NetTargetProblem.CHARACTERS, netTargetProblem(NetTool.PING, "-f"))
        assertEquals(NetTargetProblem.HOST_PORT, netTargetProblem(NetTool.PORT, "203.0.113.5"))
        assertEquals(NetTargetProblem.URL, netTargetProblem(NetTool.HTTP, "example.test"))
        assertEquals(NetTargetProblem.HOST, netTargetProblem(NetTool.TRACE, "https://example.test"))
        assertNull(netTargetProblem(NetTool.PORT, "[2001:db8::1]:6443"))
        assertNull(netTargetProblem(NetTool.PORT, "kube.example.test:6443"))
        assertNull(netTargetProblem(NetTool.HTTP, "https://example.test/healthz?verbose"))
        assertNull(netTargetProblem(NetTool.DNS, "_https._tcp.example.test"))
    }

    @Test
    fun recentTargetsMoveToTheFront() {
        val recent = rememberTarget(listOf("a.example.test", "b.example.test"), " b.example.test ")
        assertEquals(listOf("b.example.test", "a.example.test"), recent)
        assertEquals(10, rememberTarget((1..12).map { "h$it.example.test" }, "new.example.test").size)
    }
}
