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

    private val repo = "https://git.example.org/example-org/cluster-config"

    private fun rec(number: Int, state: String, forge: String = "gitea") =
        GitProposalRecord(repoURL = repo, number = number, title = "Change $number", state = state, forge = forge)

    @Test
    fun oldRecordsWithoutStateStillDecode() {
        val old = """[{"repoURL":"$repo","number":3,"title":"Bump","createdAt":1}]"""
        val records = decodeProposals(old)
        assertEquals("", records?.single()?.state)
        assertEquals(0L, records?.single()?.stateAt)
        assertEquals(records, decodeProposals(encodeProposals(records.orEmpty())))
    }

    @Test
    fun aRequestOpenedFromThePhoneStartsOpen() {
        assertEquals(GitRequestState.STATE_OPEN, openedProposal(rec(1, ""), 42).state)
        assertEquals(42L, openedProposal(rec(1, ""), 42).stateAt)
        assertEquals("merged", openedProposal(rec(1, "merged"), 42).state)
    }

    @Test
    fun onlyOpenOrUnreadRequestsAreAsked() {
        val asked = proposalsToCheck(listOf(rec(1, "open"), rec(2, ""), rec(3, "merged"), rec(4, "closed"), rec(5, "open", forge = "demo")))
        assertEquals(listOf(1, 2), asked.map { it.number })
    }

    @Test
    fun aChangeFromOpenIsToldOnce() {
        val records = listOf(rec(1, "open"), rec(2, "open"), rec(3, ""), rec(4, "open"), rec(5, "open"))
        val items = listOf(
            GitRequestStateItem(repo, 1, state = "merged"),
            GitRequestStateItem(repo, 2, state = "closed"),
            GitRequestStateItem(repo, 3, state = "merged"),
            GitRequestStateItem(repo, 4, error = "HTTP 429"),
            GitRequestStateItem(repo, 5, state = "open"),
        )
        val (next, changes) = proposalStateChanges(records, items, 1000)
        assertEquals(listOf(1 to "merged", 2 to "closed"), changes.map { it.record.number to it.state })
        assertEquals(listOf("merged", "closed", "merged", "open", "open"), next.map { it.state })
        assertEquals("never read: taken silently", 1000L, next[2].stateAt)
        assertEquals("unreadable: kept as it was", records[3], next[3])

        val (_, again) = proposalStateChanges(next, items, 2000)
        assertTrue("told once", again.isEmpty())
    }
}
