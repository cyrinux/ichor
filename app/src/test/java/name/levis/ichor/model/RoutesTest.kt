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
    fun decodesATraefikRoute() {
        val json = """{"routes":[{"kind":"IngressRoute","namespace":"monitoring","name":"grafana",""" +
            """"url":"https://grafana.home.example","service":"grafana"}]}"""
        val route = TalosJson.decodeFromString(KubeRouteList.serializer(), json).routes.single()
        assertEquals("IngressRoute", route.kind)
        assertEquals("grafana.home.example", route.label)
    }

    @Test
    fun decodesAGatewayRouteWithoutPath() {
        val json = """{"routes":[{"kind":"GRPCRoute","namespace":"shop","name":"api",""" +
            """"url":"https://api.shop.example","service":"web"},""" +
            """{"kind":"TLSRoute","namespace":"home","name":"mosquitto","url":"https://mqtt.home.example","service":"mosquitto"}]}"""
        val routes = TalosJson.decodeFromString(KubeRouteList.serializer(), json).routes
        assertEquals(listOf("GRPCRoute", "TLSRoute"), routes.map { it.kind })
        assertEquals(listOf("api.shop.example", "mqtt.home.example"), routes.map { it.label })
    }

    @Test
    fun carriesWhereTheRouteIsRead() {
        val json = """{"routes":[{"kind":"HTTPRoute","namespace":"media","name":"jellyfin","url":"https://jellyfin.home.example",""" +
            """"service":"jellyfin","group":"gateway.networking.k8s.io","version":"v1","resource":"httproutes"}]}"""
        val route = TalosJson.decodeFromString(KubeRouteList.serializer(), json).routes.single()
        assertEquals(
            KubeObjectRef("gateway.networking.k8s.io", "v1", "httproutes", "HTTPRoute", "media", "jellyfin", editable = false),
            route.objectRef,
        )
    }

    @Test
    fun aRouteWithoutItsResourceHasNoObject() {
        val json = """{"routes":[{"kind":"Ingress","namespace":"demo","name":"hello","url":"http://hello.example"}]}"""
        assertEquals(null, TalosJson.decodeFromString(KubeRouteList.serializer(), json).routes.single().objectRef)
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

    @Test
    fun certificateFieldsDecodeAndDefault() {
        val routes = TalosJson.decodeFromString(
            KubeRouteList.serializer(),
            """{"routes":[
              {"kind":"Ingress","namespace":"default","name":"vaultwarden","url":"https://vault.home.example","certNotAfter":1000000,"certIssue":"expiring","certSecret":"default/vaultwarden-tls"},
              {"kind":"Ingress","namespace":"default","name":"paperless","url":"https://paperless.home.example","certIssue":"hostMismatch","certManaged":true},
              {"kind":"Ingress","namespace":"demo","name":"hello","url":"https://hello.home.example"}]}""",
        ).routes
        assertEquals(ServiceHealth.WARNING, routes[0].certHealth)
        assertEquals("default/vaultwarden-tls", routes[0].certSecret)
        assertEquals(9L, routes[0].certDaysLeft(1_000_000L - 9 * 86_400L - 3_600L))
        assertEquals(ServiceHealth.WARNING, routes[1].certHealth)
        assertEquals(true, routes[1].certManaged)
        assertEquals(null, routes[2].certHealth)
        assertEquals(0L, routes[2].certNotAfter)
    }

    @Test
    fun routeCertKindsKeepTheirWireNames() {
        assertEquals("routeCertExpired", CheckupKind.ROUTE_CERT_EXPIRED)
        assertEquals("routeCertExpiring", CheckupKind.ROUTE_CERT_EXPIRING)
        assertEquals("routeCertHostMismatch", CheckupKind.ROUTE_CERT_HOST_MISMATCH)
    }
}
