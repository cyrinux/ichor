package name.levis.ichor.model

import name.levis.ichor.R
import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.checkupIssuesWithGaps
import name.levis.ichor.ui.checkup.checkupFix
import name.levis.ichor.ui.checkup.detail
import org.junit.Assert.assertEquals
import org.junit.Test

class SopsCheckupTest {
    // The demo checkup's SOPS finding (go/ichorgo/kube_checkup_demo.go).
    private val raw = """
        {"status":"critical","sections":[{"id":"secrets","status":"critical","checked":12,"findings":[
          {"kind":"sopsDecryptionFailed","severity":"critical","namespace":"flux-system","name":"apps","extra":"sops-age","reason":"BuildFailed","message":"error decrypting sources: failed to decrypt: no key could decrypt the data"}]}]}
    """.trimIndent()

    private val report get() = TalosJson.decodeFromString(CheckupReport.serializer(), raw)
    private val issue = mapOf("secrets|sopsDecryptionFailed|flux-system/apps" to "critical")

    @Test fun theDemoFindingDecodes() {
        val f = report.shownSections.single().findings.single()
        assertEquals(CheckupKind.SOPS_DECRYPTION_FAILED, f.kind)
        assertEquals("sops-age", f.extra)
        assertEquals("BuildFailed: error decrypting sources: failed to decrypt: no key could decrypt the data", f.detail)
    }

    @Test fun theFixFollowsTheReasonAndTheKey() {
        val kind = CheckupKind.SOPS_DECRYPTION_FAILED
        assertEquals(R.string.checkup_sopsDecryptionFailed_fix, checkupFix(kind, "BuildFailed", "sops-age"))
        assertEquals(R.string.checkup_sopsDecryptionFailedNoKey_fix, checkupFix(kind, "BuildFailed", ""))
        assertEquals(R.string.checkup_sopsDecryptionNotConfigured_fix, checkupFix(kind, "ReconciliationFailed", ""))
    }

    @Test fun theMonitorAlertsOnItWithoutGitOpsWatching() {
        assertEquals(issue, report.alertIssues())
        assertEquals(issue, checkupIssuesWithGaps(report, known = emptyMap(), gitopsWatched = false))
    }

    @Test fun theGitOpsAlertRaisesItWhenWatched() {
        assertEquals(emptyMap<String, String>(), report.alertIssues(gitopsWatched = true))
        assertEquals(emptyMap<String, String>(), checkupIssuesWithGaps(report, known = emptyMap(), gitopsWatched = true))
        assertEquals(setOf("sopsDecryptionFailed"), GITOPS_ALERTED_KINDS)
    }
}
