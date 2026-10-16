package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ExternalDnsTest {
    private val json = """
        {"externalDns":{"version":"v1alpha1","error":"",
         "controllers":[
          {"namespace":"external-dns","name":"external-dns","ready":true,"replicas":1,"readyReplicas":1,
           "provider":"cloudflare","policy":"sync","registry":"txt","ownerId":"homelab",
           "domainFilters":["example.com"],"sources":["ingress","service","crd"],"dryRun":false,
           "intervalSeconds":30,"annotationFilter":"","health":"ok","reasons":[]},
          {"namespace":"dns-2","name":"legacy","ready":false,"replicas":2,"readyReplicas":0,
           "provider":"inmemory","policy":"","registry":"txt","ownerId":"legacy",
           "domainFilters":["example.org"],"sources":["ingress"],"dryRun":true,
           "intervalSeconds":60,"annotationFilter":"","health":"critical","reasons":["notReady","dryRun"]}],
         "records":[
          {"dnsName":"*.example.net","recordType":"A","targets":["203.0.113.50"],"ttl":300,
           "sourceKind":"Ingress","sourceNamespace":"demo","sourceName":"legacy",
           "sourceGroup":"networking.k8s.io","sourceVersion":"v1","sourceResource":"ingresses",
           "controller":"","health":"warning","reasons":["unmanaged"]},
          {"dnsName":"db.example.com","recordType":"A","targets":["203.0.113.40"],"ttl":300,
           "sourceKind":"DNSEndpoint","sourceNamespace":"demo","sourceName":"db",
           "sourceGroup":"externaldns.k8s.io","sourceVersion":"v1alpha1","sourceResource":"dnsendpoints",
           "controller":"external-dns","health":"warning","reasons":["stale"]},
          {"dnsName":"status.example.com","recordType":"A","targets":[],"ttl":0,
           "sourceKind":"Ingress","sourceNamespace":"demo","sourceName":"status",
           "sourceGroup":"networking.k8s.io","sourceVersion":"v1","sourceResource":"ingresses",
           "controller":"external-dns","health":"warning","reasons":["noTarget"]},
          {"dnsName":"app.example.com","recordType":"A","targets":["203.0.113.10"],"ttl":300,
           "sourceKind":"Ingress","sourceNamespace":"demo","sourceName":"app",
           "sourceGroup":"networking.k8s.io","sourceVersion":"v1","sourceResource":"ingresses",
           "controller":"external-dns","health":"ok","reasons":[]},
          {"dnsName":"extra.example.com","recordType":"CNAME","targets":["lb.example.com"],"ttl":300,
           "sourceKind":"Ingress","sourceNamespace":"demo","sourceName":"extra",
           "sourceGroup":"networking.k8s.io","sourceVersion":"v1","sourceResource":"ingresses",
           "controller":"external-dns","health":"ok","reasons":[]}],
         "recordCount":5,"truncated":false}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesControllersAndRecords() {
        val e = services.externalDns!!
        assertEquals("v1alpha1", e.version)
        assertEquals(2, e.controllers.size)
        val main = e.controllers[0]
        assertEquals("cloudflare", main.provider)
        assertEquals("sync", main.policy)
        assertEquals(listOf("example.com"), main.domainFilters)
        assertEquals(30, main.intervalSeconds)
        assertEquals(ServiceHealth.OK, main.serviceHealth)
        val legacy = e.controllers[1]
        assertEquals(ServiceHealth.CRITICAL, legacy.serviceHealth)
        assertEquals(listOf(ExternalDnsReason.NOT_READY, ExternalDnsReason.DRY_RUN), legacy.reasonList)
        assertEquals(5, e.recordCount)
        // Problems first: the unmanaged name, then the stale DNSEndpoint, then the ok ones.
        assertEquals("*.example.net", e.records[0].dnsName)
        assertEquals(listOf(ExternalDnsReason.UNMANAGED), e.records[0].reasonList)
        assertEquals("external-dns/demo/db", e.records[1].controller + "/demo/db")
        assertEquals("DNSEndpoint/demo/db", e.records[1].sourceLabel)
        assertNull(e.records[0].controller.takeIf { it.isNotEmpty() })
        assertEquals("CNAME", e.records[4].recordType)
        assertEquals(listOf(DataServiceKind.EXTERNAL_DNS), services.detected)
    }

    @Test
    fun summaryCountsRecordsAndLooksAtControllers() {
        // 5 records, of which 3 need a look (unmanaged, stale, no target) plus the critical controller.
        assertEquals(ServiceSummary(total = 5, attention = 4, health = ServiceHealth.CRITICAL), services.summary(DataServiceKind.EXTERNAL_DNS))
    }

    @Test
    fun alertsCoverCriticalControllersOnly() {
        assertEquals(
            mapOf("externaldns|dns-2/legacy" to DATA_CRITICAL),
            dataIssuesOf(services),
        )
    }

    @Test
    fun hintsIncludeExternalDns() {
        val inventory = Inventory(apps = listOf(InventoryApp(id = "external-dns", name = "ExternalDNS"), InventoryApp(id = "velero", name = "Velero")))
        assertTrue(inventory.dataServiceHints().contains("external-dns"))
    }
}
