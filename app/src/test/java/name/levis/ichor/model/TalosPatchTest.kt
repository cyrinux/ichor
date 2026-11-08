package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TalosPatchTest {
    private val nodes = listOf(
        NodeOverview(node = "192.0.2.11", hostname = "cp-1.lab.example.test", reachable = true),
        NodeOverview(node = "192.0.2.21", hostname = "worker-1", reachable = true),
    )

    private fun info(vararg hostnames: String) = TalosPatchInfo(
        talos = true,
        tryable = true,
        documents = hostnames.map { TalosPatchDoc(kind = "HostnameConfig", hostname = it) } + TalosPatchDoc(kind = "v1alpha1"),
    )

    @Test
    fun `the node the patch sets the hostname of, by full or short name`() {
        assertEquals("192.0.2.21", patchNode(info("worker-1"), nodes)?.node)
        assertEquals("192.0.2.11", patchNode(info("cp-1"), nodes)?.node)
        assertEquals("192.0.2.11", patchNode(info("CP-1.lab.example.test"), nodes)?.node)
    }

    @Test
    fun `no node when the patch names none or another one`() {
        assertNull(patchNode(info(), nodes))
        assertNull(patchNode(info("cp-9"), nodes))
        assertEquals(listOf("cp-1"), info("cp-1", "cp-1").hostnames)
    }

    @Test
    fun `decodes the core's answer`() {
        val json = """{"talos":true,"kind":"documents","documents":[{"kind":"HostnameConfig","hostname":"cp-1"}],"touchesSecrets":false,"tryable":true}"""
        val decoded = name.levis.ichor.data.TalosJson.decodeFromString(TalosPatchInfo.serializer(), json)
        assertEquals(listOf("cp-1"), decoded.hostnames)
        assertEquals("documents", decoded.kind)
    }
}
