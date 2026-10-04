package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.DATA_WARNING
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.assertEquals
import org.junit.Test

class CertManagerTest {
    private val json = """
        {"certManager":{"version":"v1","error":"","certificates":[
          {"namespace":"app","name":"old","secretName":"old-tls","dnsNames":["old.example.com"],"dnsNameCount":1,"issuer":"ClusterIssuer/letsencrypt",
           "health":"critical","reasons":["expired"],"ready":false,"message":"Certificate expired","notAfter":1759276800000,"renewalTime":1756684800000,"failedAttempts":5},
          {"namespace":"app","name":"stuck","secretName":"stuck-tls","dnsNames":["stuck.example.com"],"dnsNameCount":1,"issuer":"ClusterIssuer/letsencrypt",
           "health":"warning","reasons":["expiring","renewalOverdue"],"ready":true,"notAfter":1760227200000,"renewalTime":1757635200000},
          {"namespace":"app","name":"internal","secretName":"internal-tls","dnsNames":["a.example.com","b.example.com"],"dnsNameCount":6,"issuer":"Issuer/internal-ca",
           "health":"warning","reasons":["issuer"],"ready":true,"notAfter":1798761600000,"renewalTime":1796083200000},
          {"namespace":"app","name":"web","secretName":"web-tls","dnsNames":["web.example.com"],"dnsNameCount":1,"issuer":"ClusterIssuer/letsencrypt",
           "health":"ok","reasons":[],"ready":true,"notAfter":1796083200000,"renewalTime":1793491200000}],
         "issuers":[
          {"kind":"Issuer","namespace":"app","name":"internal-ca","type":"ca","ready":false,"message":"secret not found","health":"warning"},
          {"kind":"ClusterIssuer","name":"letsencrypt","type":"acme","server":"acme-v02.api.letsencrypt.org","ready":true,"health":"ok"}]}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesCertificatesAndIssuers() {
        val cm = services.certManager!!
        assertEquals(4, cm.certificates.size)
        assertEquals(listOf(CertReason.EXPIRING, CertReason.RENEWAL_OVERDUE), cm.certificates[1].reasonList)
        assertEquals(6, cm.certificates[2].dnsNameCount)
        assertEquals(1759276800000, cm.certificates[0].notAfter)
        assertEquals(listOf("Issuer/app/internal-ca", "ClusterIssuer/letsencrypt"), cm.issuers.map { it.label })
        assertEquals("acme-v02.api.letsencrypt.org", cm.issuers[1].server)
        assertEquals(listOf(DataServiceKind.CERT_MANAGER), services.detected)
    }

    @Test
    fun summaryCountsIssuersToo() {
        // Three certificates and the CA issuer need a look.
        assertEquals(ServiceSummary(total = 4, attention = 4, health = ServiceHealth.CRITICAL), services.summary(DataServiceKind.CERT_MANAGER))
    }

    @Test
    fun noLikelyCause() {
        assertEquals(emptyList<LikelyCause>(), services.likelyCauses(setOf("node-1")))
    }

    @Test
    fun alertsLeaveTheIssuerReasonToTheIssuer() {
        assertEquals(
            mapOf(
                "certmanager|app/old" to DATA_CRITICAL,
                "certmanager|app/stuck" to DATA_WARNING,
                "certmanager|Issuer/app/internal-ca" to DATA_WARNING,
            ),
            dataIssuesOf(services),
        )
    }

    @Test
    fun hintsIncludeCertManager() {
        val inventory = Inventory(apps = listOf(InventoryApp(id = "cert-manager", name = "cert-manager"), InventoryApp(id = "longhorn", name = "Longhorn")))
        assertEquals("longhorn,cert-manager", inventory.dataServiceHints())
    }
}
