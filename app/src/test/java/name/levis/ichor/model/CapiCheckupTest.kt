package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class CapiCheckupTest {
    // The demo checkup's Cluster lifecycle section (go/ichorgo/kube_checkup_demo.go), plus the unaccepted summary.
    private val raw = """
        {"status":"critical","sections":[{"id":"clusterapi","status":"critical","checked":19,"findings":[
          {"kind":"sideroServerOff","severity":"critical","name":"srv-b2","extra":"rack2-u02"},
          {"kind":"capiMachineStuck","severity":"warning","namespace":"fleet","name":"staging-cp-x7k2p","extra":"staging/Provisioning","since":1},
          {"kind":"sideroServerUnaccepted","severity":"warning","name":"","count":3}]}]}
    """.trimIndent()

    @Test fun clusterAPISectionDecodesAndAlerts() {
        val report = TalosJson.decodeFromString(CheckupReport.serializer(), raw)
        val section = report.shownSections.single()
        assertEquals(CheckupSectionId.CLUSTER_API, section.id)
        assertEquals(
            listOf(CheckupKind.SIDERO_SERVER_OFF, CheckupKind.CAPI_MACHINE_STUCK, CheckupKind.SIDERO_SERVER_UNACCEPTED),
            section.findings.map { it.kind },
        )
        assertEquals("fleet/staging-cp-x7k2p", section.findings[1].subject)
        assertEquals(
            mapOf(
                "clusterapi|sideroServerOff|srv-b2" to "critical",
                "clusterapi|capiMachineStuck|fleet/staging-cp-x7k2p" to "warning",
                "clusterapi|sideroServerUnaccepted|" to "warning",
            ),
            report.alertIssues(),
        )
    }
}
