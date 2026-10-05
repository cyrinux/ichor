package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CertDetailsTest {
    private val json = """
        {"conditions":[{"type":"Ready","status":"False","reason":"Failed","message":"order failed","time":1791194400000}],
         "requests":[{"name":"site-2","created":1791190800000,"conditions":[{"type":"Ready","status":"False","reason":"Pending"}],
           "orders":[{"name":"site-2-77","state":"pending","challenges":[
             {"name":"site-2-77-1","type":"HTTP-01","dnsName":"site.example.com","state":"pending","reason":"wrong status code '404'","presented":true}]}]}],
         "events":[{"time":1791195900000,"type":"Warning","reason":"PresentError","message":"404","object":"Challenge/site-2-77-1","count":5}],
         "log":["I2 propagation check failed"],"error":"","future":1}
    """.trimIndent()

    @Test
    fun decodesTheChain() {
        val d = TalosJson.decodeFromString(CertDetails.serializer(), json)
        assertFalse(d.conditions.single().isTrue)
        val challenge = d.requests.single().orders.single().challenges.single()
        assertEquals("site.example.com", challenge.dnsName)
        assertEquals("wrong status code '404'", challenge.reason)
        assertTrue(d.events.single().warning)
        assertEquals("Challenge/site-2-77-1", d.events.single().`object`)
        assertEquals(listOf("I2 propagation check failed"), d.log)
    }

    @Test
    fun failedAcmeStates() {
        assertEquals(listOf("invalid", "errored", "expired"), listOf("pending", "ready", "valid", "invalid", "errored", "expired", "").filter(::acmeFailed))
    }
}
