package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TailscaleTest {
    // Go KubeTailscale's demo answer.
    private val json = """
        {"installed":true,"version":"v1alpha1","truncated":false,
         "operator":{"namespace":"tailscale","name":"operator","ready":1,"desired":1,"image":"tailscale/k8s-operator:v1.90.0","health":"ok"},
         "exposed":[
          {"kind":"Service","namespace":"payments","name":"legacy","direction":"egress","tailnetNames":["100.64.0.77"],"ports":["53/UDP"],
           "proxyGroup":"","tags":["tag:k8s"],"ready":false,"reason":"ProxyFailed","message":"error provisioning proxy","health":"critical"},
          {"kind":"Ingress","namespace":"docs","name":"wiki","direction":"ingress","tailnetNames":["wiki.example-tailnet.ts.net"],"ports":["443"],
           "proxyGroup":"","tags":["tag:k8s"],"ready":true,"reason":"","message":"","health":"ok"},
          {"kind":"Service","namespace":"shop","name":"storefront","direction":"ingress","tailnetNames":["storefront.example-tailnet.ts.net","100.64.0.51"],
           "ports":["443","80"],"proxyGroup":"ingress-proxies","tags":["tag:k8s"],"ready":true,"reason":"","message":"","health":"ok"}],
         "connectors":[
          {"name":"saas-connector","hostname":"saas-connector","tailnetIPs":[],"subnetRoutes":"","advertisedRoutes":[],"exitNode":false,
           "appConnector":true,"appConnectorRoutes":["203.0.113.0/24"],"tags":["tag:k8s"],"proxyClass":"saas","replicas":1,"devices":[],
           "ready":false,"reason":"ProxyClassMissing","message":"ProxyClass \"saas\" does not exist","health":"critical"},
          {"name":"exit-eu","hostname":"exit-eu","tailnetIPs":["100.64.0.21"],"subnetRoutes":"","advertisedRoutes":[],"exitNode":true,
           "appConnector":false,"appConnectorRoutes":[],"tags":["tag:k8s"],"proxyClass":"","replicas":1,
           "devices":[{"hostname":"exit-eu","tailnetIPs":["100.64.0.21"],"staticEndpoints":[]}],"ready":true,"reason":"","message":"","health":"ok"},
          {"name":"office-router","hostname":"office-router","tailnetIPs":["100.64.0.11","fd7a:115c:a1e0::b"],"subnetRoutes":"10.0.0.0/24",
           "advertisedRoutes":["10.0.0.0/24"],"exitNode":false,"appConnector":false,"appConnectorRoutes":[],"tags":["tag:k8s"],"proxyClass":"prod","replicas":2,
           "devices":[{"hostname":"office-router-0","tailnetIPs":["100.64.0.11"],"staticEndpoints":[]},{"hostname":"office-router-1","tailnetIPs":["100.64.0.12"],"staticEndpoints":[]}],
           "ready":true,"reason":"","message":"","health":"ok"}],
         "proxyGroups":[
          {"name":"api-proxies","type":"kube-apiserver","replicas":2,"proxyClass":"","tags":["tag:k8s"],"url":"https://api-proxies.example.ts.net",
           "devices":[{"hostname":"api-proxies-0","tailnetIPs":["100.64.0.41"],"staticEndpoints":[]},{"hostname":"api-proxies-1","tailnetIPs":["100.64.0.42"],"staticEndpoints":[]}],
           "available":true,"ready":true,"reason":"","message":"","health":"ok"},
          {"name":"ingress-proxies","type":"ingress","replicas":2,"proxyClass":"prod","tags":["tag:k8s"],"url":"","hostnamePrefix":"ingress","serves":1,
           "devices":[{"hostname":"ingress-proxies-0","tailnetIPs":["100.64.0.31"],"staticEndpoints":[]},{"hostname":"ingress-proxies-1","tailnetIPs":["100.64.0.32"],"staticEndpoints":[]}],
           "available":true,"ready":true,"reason":"","message":"","health":"ok"}],
         "proxyClasses":[
          {"name":"metrics","usedBy":0,"ready":false,"reason":"ProxyClassInvalid","message":"spec.metrics.serviceMonitor needs the prometheus-operator CRDs","health":"warning"},
          {"name":"prod","usedBy":2,"ready":true,"reason":"","message":"","health":"ok"}],
         "dnsConfig":{"name":"ts-dns","nameserverIP":"100.100.100.100","ready":true,"reason":"","message":"","health":"ok"},
         "counts":{"ok":7,"warning":1,"critical":1}}
    """.trimIndent()

    private val ts = TalosJson.decodeFromString(Tailscale.serializer(), json)

    @Test
    fun decodesEveryKind() {
        assertTrue(ts.installed)
        assertEquals("tailscale/operator", ts.operator?.let { "${it.namespace}/${it.name}" })
        assertEquals(3, ts.connectors.size)
        assertEquals("https://api-proxies.example.ts.net", ts.proxyGroups[0].url)
        assertEquals(2, ts.proxyClasses.single { it.name == "prod" }.usedBy)
        assertEquals("100.100.100.100", ts.dnsConfig?.nameserverIP)
        assertEquals(TailscaleCounts(ok = 7, warning = 1, critical = 1), ts.counts)
    }

    @Test
    fun problemsComeFirstWithTheirReason() {
        val app = ts.connectors.first()
        assertEquals(ServiceHealth.CRITICAL, app.serviceHealth)
        assertEquals("ProxyClassMissing", app.reason)
        assertTrue(app.appConnector)
        assertEquals(ServiceHealth.WARNING, ts.proxyClasses.first().serviceHealth)
    }

    @Test
    fun exposedRowsDecodeProblemsFirst() {
        assertEquals(3, ts.exposed.size)
        val legacy = ts.exposed.first()
        assertTrue(legacy.isEgress)
        assertEquals(ServiceHealth.CRITICAL, legacy.serviceHealth)
        assertEquals(listOf("53/UDP"), legacy.ports)
        val storefront = ts.exposed.single { it.name == "storefront" }
        assertFalse(storefront.isEgress)
        assertEquals("ingress-proxies", storefront.proxyGroup)
        assertEquals(listOf("storefront.example-tailnet.ts.net", "100.64.0.51"), storefront.tailnetNames)
        val group = ts.proxyGroups.single { it.name == "ingress-proxies" }
        assertEquals(1, group.serves)
        assertEquals("ingress", group.hostnamePrefix)
        assertEquals(0, ts.proxyGroups.single { it.name == "api-proxies" }.serves)
    }

    @Test
    fun exposedRefsOpenTheServiceOrIngress() {
        assertEquals(KubeObjectRef.service("payments", "legacy"), ts.exposed[0].ref)
        assertEquals(
            KubeObjectRef("networking.k8s.io", "v1", "ingresses", "Ingress", "docs", "wiki", editable = true),
            ts.exposed[1].ref,
        )
        assertFalse(Tailscale(installed = true, exposed = ts.exposed).isEmpty)
    }

    @Test
    fun routesFallBackToTheStatus() {
        assertEquals(listOf("10.0.0.0/24"), ts.connectors[2].routes)
        val fromStatus = ts.connectors[2].copy(advertisedRoutes = emptyList(), subnetRoutes = "10.0.0.0/24, 10.1.0.0/16")
        assertEquals(listOf("10.0.0.0/24", "10.1.0.0/16"), fromStatus.routes)
        assertEquals(emptyList<String>(), ts.connectors[1].routes)
    }

    @Test
    fun refsAreClusterScoped() {
        assertEquals(
            KubeObjectRef("tailscale.com", "v1alpha1", "connectors", "Connector", "", "saas-connector", editable = true),
            ts.ref(TailscaleKind.CONNECTOR, "saas-connector"),
        )
        assertEquals("dnsconfigs", ts.ref(TailscaleKind.DNS_CONFIG, "ts-dns").resource)
        assertEquals("v1alpha1", Tailscale(installed = true).ref(TailscaleKind.PROXY_CLASS, "prod").version)
    }

    @Test
    fun anOlderOrEmptyAnswerStillDecodes() {
        val absent = TalosJson.decodeFromString(Tailscale.serializer(), """{"installed":false}""")
        assertFalse(absent.installed)
        assertNull(absent.operator)
        assertTrue(absent.isEmpty)
        assertFalse(ts.isEmpty)
    }
}
