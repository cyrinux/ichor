package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeServicesTest {

    private val json = """{"partialAccess":true,"lbController":"cilium","services":[
        {"namespace":"shop","name":"web-public","type":"LoadBalancer","clusterIP":"10.96.0.11","ports":["443:30443/TCP"],
         "addresses":[],"pending":true,"selector":true,"endpointsKnown":true,"endpoints":2,"readyEndpoints":2,"routes":[],"level":"critical"},
        {"namespace":"shop","name":"web","type":"ClusterIP","clusterIP":"10.96.0.10","ports":["80/TCP"],"addresses":[],
         "selector":true,"endpointsKnown":true,"endpoints":3,"readyEndpoints":2,
         "routes":[{"kind":"Ingress","namespace":"shop","name":"web","url":"https://shop.example.org","service":"web"}],"level":"warning"},
        {"namespace":"shop","name":"queue","type":"ClusterIP","clusterIP":"None","level":"newer-level"},
        {"namespace":"shop","name":"legacy","type":"ExternalName","externalName":"db.example.net","endpointsKnown":true}]}"""

    private val services = TalosJson.decodeFromString(KubeServices.serializer(), json)

    @Test
    fun decodesTheCoreJson() {
        assertTrue(services.partialAccess)
        assertTrue(services.anyPending)
        assertEquals(LbController.CILIUM, LbController.of(services.lbController))
        assertNull(LbController.of(""))
        val public = services.services.first()
        assertEquals(StorageLevel.CRITICAL, public.level)
        assertEquals("shop/web-public", public.key)
        assertEquals("shop.example.org", services.services[1].routes.single().label)
        // A level this version does not know reads as ok; an older row decodes with its defaults.
        val queue = services.services[2]
        assertEquals(StorageLevel.OK, queue.level)
        assertTrue(queue.headless)
        assertEquals(emptyList<String>(), queue.ports)
    }

    @Test
    fun readyCountShownOnlyWhenKnownAndExpected() {
        assertEquals("2/3", services.services[1].readyText)
        // Endpoints not read: nothing to say.
        assertNull(services.services[2].readyText)
        // No selector and no endpoint: an ExternalName has none to count.
        assertNull(services.services[3].readyText)
        assertFalse(services.services[3].headless)
    }

    @Test
    fun searchesNameTypeAddressesPortsAndRoutes() {
        assertEquals(listOf("web"), services.services.filteredServices("SHOP.EXAMPLE").map { it.name })
        assertEquals(listOf("web-public"), services.services.filteredServices("30443").map { it.name })
        assertEquals(listOf("legacy"), services.services.filteredServices("externalname").map { it.name })
        assertEquals(listOf("web"), services.services.filteredServices("10.96.0.10").map { it.name })
        assertEquals(4, services.services.filteredServices(" ").size)
    }
}
