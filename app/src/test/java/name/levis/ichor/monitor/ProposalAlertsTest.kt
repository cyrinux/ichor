package name.levis.ichor.monitor

import name.levis.ichor.model.GitProposalRecord
import name.levis.ichor.model.ProposalChange
import name.levis.ichor.model.ShareTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class ProposalAlertsTest {
    private val repo = "https://git.example.org/example-org/cluster-config"

    @Test
    fun aMergedRequestOpensItsArgoApp() {
        val record = GitProposalRecord(tool = "argocd", kind = "Application", namespace = "argocd", name = "ingest", repoURL = repo, number = 12, title = "Raise ingest replicas")
        val alert = proposalAlert(ProposalChange(record, "merged"))
        assertEquals("proposal:$repo#12", alert.key)
        assertEquals(AlertKind.PROPOSAL_MERGED, alert.kind)
        assertFalse(alert.problem)
        assertEquals("Raise ingest replicas", alert.subject)
        assertEquals(AlertChannel.GITOPS, alert.channel)
        assertEquals(ShareTarget.argoApp("argocd", "ingest"), alert.shareTarget())
        assertEquals("12", ProposalDetail.parse(alert.detail).number)
    }

    @Test
    fun aClosedFluxRequestOpensItsObject() {
        val record = GitProposalRecord(tool = GITOPS_FLUX, kind = "Kustomization", namespace = "flux-system", name = "apps", repoURL = repo, number = 3)
        val alert = proposalAlert(ProposalChange(record, "closed"))
        assertEquals(AlertKind.PROPOSAL_CLOSED, alert.kind)
        assertEquals(ShareTarget.fluxApp("Kustomization", "flux-system", "apps"), alert.shareTarget())
    }

    @Test
    fun theRepositorysOwnRequestOpensTheCluster() {
        val alert = proposalAlert(ProposalChange(GitProposalRecord(repoURL = repo, number = 4, path = "talos/cp-1.yaml"), "merged"))
        assertEquals(ShareTarget.screen(ShareTarget.CLUSTER), alert.shareTarget())
        assertEquals(emptyList<AlertAction>(), alert.actions(canWake = false, canReboot = false))
    }
}
