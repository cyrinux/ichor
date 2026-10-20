package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GitProposalRecordTest {
    private fun record(n: Int, tool: String = "argocd") = GitProposalRecord(
        tool = tool, kind = "Application", namespace = "argocd", name = "web",
        repoURL = "https://github.com/example-org/cluster-config", number = n, title = "Change $n",
    )

    @Test
    fun keepsTheNewestFirstOnceAndAtMostFifty() {
        var list = emptyList<GitProposalRecord>()
        repeat(MAX_PROPOSALS + 5) { list = withProposal(list, record(it + 1)) }
        assertEquals(MAX_PROPOSALS, list.size)
        assertEquals(MAX_PROPOSALS + 5, list.first().number)

        // The same request again moves to the top, once.
        list = withProposal(list, record(30).copy(title = "again"))
        assertEquals("again", list.first().title)
        assertEquals(1, list.count { it.number == 30 })
    }

    @Test
    fun roundTripsAndSaysWhatItIsFor() {
        val list = listOf(record(1), record(2, tool = ""))
        assertEquals(list, decodeProposals(encodeProposals(list)))
        assertEquals(null, decodeProposals("not json"))
        assertTrue(record(1).isFor("argocd", "Application", "argocd", "web"))
        assertFalse(record(1).isFor("flux", "Application", "argocd", "web"))
        assertEquals("github.com", record(1).host)
        assertEquals("https://github.com/example-org/cluster-config#1", record(1).key)
    }

    @Test
    fun statusFromTheForgeAndTheApp() {
        val open = GitRequestState(state = GitRequestState.STATE_OPEN)
        val merged = GitRequestState(state = GitRequestState.STATE_MERGED, mergeSha = "abc1234")
        assertEquals(ProposalStatus.UNKNOWN, proposalStatus(null, null))
        assertEquals(ProposalStatus.OPEN, proposalStatus(open, null))
        assertEquals(ProposalStatus.DRAFT, proposalStatus(open.copy(draft = true), null))
        assertEquals(ProposalStatus.CLOSED, proposalStatus(GitRequestState(state = GitRequestState.STATE_CLOSED), null))
        assertEquals(ProposalStatus.MERGED, proposalStatus(merged, null))
        assertEquals(ProposalStatus.DEPLOYED, proposalStatus(merged, RevisionRuns(runs = true, revision = "abc1234")))
        assertEquals(ProposalStatus.OTHER_REVISION, proposalStatus(merged, RevisionRuns(runs = false, revision = "def5678")))
        assertEquals(ProposalStatus.MERGED, proposalStatus(merged, RevisionRuns()))
    }

    @Test
    fun theDemoHasOneOpenAndOneMerged() {
        assertEquals(listOf(1, 2), DEMO_PROPOSALS.map { it.number })
        assertTrue(DEMO_PROPOSALS.all { it.isFor("argocd", "Application", "argocd", "hello-ichor") })
    }
}
