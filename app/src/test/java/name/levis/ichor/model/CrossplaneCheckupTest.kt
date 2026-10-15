package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class CrossplaneCheckupTest {
    // The demo checkup's Crossplane section (go/ichorgo/kube_checkup_demo.go).
    private val raw = """
        {"status":"critical","sections":[{"id":"crossplane","status":"critical","checked":12,"findings":[
          {"kind":"crossplaneSyncFailed","severity":"critical","namespace":"apps","name":"Database/app-db-primary","reason":"ReconcileError","message":"cannot create database instance: quota exceeded","extra":"default"},
          {"kind":"crossplaneProviderUnhealthy","severity":"critical","name":"Provider/provider-storage","reason":"UnhealthyPackageRevision","message":"cannot pull package","extra":"xpkg.example.org/demo/provider-storage:v1.4.0"},
          {"kind":"crossplaneNotReady","severity":"warning","name":"Bucket/assets-bucket","reason":"Creating","extra":"default","count":42,"since":1}]}]}
    """.trimIndent()

    @Test fun crossplaneSectionDecodesAndAlerts() {
        val report = TalosJson.decodeFromString(CheckupReport.serializer(), raw)
        val section = report.shownSections.single()
        assertEquals(CheckupSectionId.CROSSPLANE, section.id)
        assertEquals(
            listOf(CheckupKind.CROSSPLANE_SYNC_FAILED, CheckupKind.CROSSPLANE_PROVIDER_UNHEALTHY, CheckupKind.CROSSPLANE_NOT_READY),
            section.findings.map { it.kind },
        )
        assertEquals("apps/Database/app-db-primary", section.findings[0].subject)
        assertEquals(
            mapOf(
                "crossplane|crossplaneSyncFailed|apps/Database/app-db-primary" to "critical",
                "crossplane|crossplaneProviderUnhealthy|Provider/provider-storage" to "critical",
                "crossplane|crossplaneNotReady|Bucket/assets-bucket" to "warning",
            ),
            report.alertIssues(),
        )
    }
}
