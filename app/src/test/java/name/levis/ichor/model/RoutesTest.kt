package name.levis.ichor.model

import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class RoutesTest {

    @Test
    fun decodesTheGoJson() {
        val json = """{"routes":[{"kind":"HTTPRoute","namespace":"media","name":"jellyfin","url":"https://jellyfin.home.example/web",""" +
            """"service":"jellyfin"},{"kind":"Ingress","namespace":"demo","name":"hello","url":"http://hello.example"}]}"""
        val routes = TalosJson.decodeFromString(KubeRouteList.serializer(), json).routes
        assertEquals(listOf("jellyfin.home.example/web", "hello.example"), routes.map { it.label })
        assertEquals("", routes[1].service)
        assertEquals(emptyList<KubeRoute>(), TalosJson.decodeFromString(KubeRouteList.serializer(), "{}").routes)
    }

    @Test
    fun routePodsOncePerPodEncodedForGo() {
        val app = InventoryApp(
            id = "web",
            name = "Web",
            pods = listOf(
                InventoryPod("shop", "web-0", "n1"),
                InventoryPod("shop", "web-0", "n2"),
                InventoryPod("shop", "web-1", "n1"),
            ),
        )
        assertEquals(listOf(RoutePod("shop", "web-0"), RoutePod("shop", "web-1")), app.routePods)
        assertEquals(
            """[{"namespace":"shop","pod":"web-0"}]""",
            TalosJson.encodeToString(ListSerializer(RoutePod.serializer()), app.routePods.take(1)),
        )
    }
}
