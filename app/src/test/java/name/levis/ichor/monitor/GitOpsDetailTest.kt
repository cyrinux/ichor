package name.levis.ichor.monitor

import org.junit.Assert.assertEquals
import org.junit.Test

class GitOpsDetailTest {

    @Test
    fun splitsToolSeverityAndReason() {
        assertEquals(GitOpsDetail("argocd", "critical", "failed"), GitOpsDetail.parse("argocd|critical|failed"))
        assertEquals(GitOpsDetail("flux", "critical", "notReady"), GitOpsDetail.parse("$GITOPS_FLUX|${gitopsValue(DATA_CRITICAL, GITOPS_FLUX_NOT_READY)}"))
    }

    @Test
    fun missingPartsAreEmpty() {
        assertEquals(GitOpsDetail("argocd", "", ""), GitOpsDetail.parse("argocd"))
        assertEquals(GitOpsDetail("", "", ""), GitOpsDetail.parse(""))
    }
}
