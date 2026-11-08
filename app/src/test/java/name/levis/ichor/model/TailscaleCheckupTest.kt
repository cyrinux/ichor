package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class TailscaleCheckupTest {
    // The demo checkup's Tailscale section (go/ichorgo/kube_checkup_demo.go), plus a proxy group.
    private val raw = """
        {"status":"critical","sections":[{"id":"tailscale","status":"critical","checked":6,"findings":[
          {"kind":"tailscaleConnectorNotReady","severity":"critical","name":"lab-router","reason":"ConnectorCreationFailed","message":"failed to create the subnet router","extra":"subnet-router"},
          {"kind":"tailscaleProxyGroupDown","severity":"warning","name":"egress-proxies","reason":"ProxyGroupNotReady","extra":"egress"}]}]}
    """.trimIndent()

    @Test fun tailscaleSectionDecodesAndAlerts() {
        val report = TalosJson.decodeFromString(CheckupReport.serializer(), raw)
        val section = report.shownSections.single()
        assertEquals(CheckupSectionId.TAILSCALE, section.id)
        assertEquals(listOf(CheckupKind.TAILSCALE_CONNECTOR_NOT_READY, CheckupKind.TAILSCALE_PROXY_GROUP_DOWN), section.findings.map { it.kind })
        assertEquals(
            mapOf(
                "tailscale|tailscaleConnectorNotReady|lab-router" to "critical",
                "tailscale|tailscaleProxyGroupDown|egress-proxies" to "warning",
            ),
            report.alertIssues(),
        )
    }
}
