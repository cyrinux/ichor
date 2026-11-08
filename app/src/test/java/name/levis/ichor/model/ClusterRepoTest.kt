package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ClusterRepoTest {

    @Test
    fun normalizeTrimsAndCleansDirectories() {
        val repo = normalizeClusterRepo(ClusterRepo(" git@github.com:example-org/cluster-config.git ", " main ", "/talos/", " kubernetes/ "))
        assertEquals(ClusterRepo("git@github.com:example-org/cluster-config.git", "main", "talos", "kubernetes"), repo)
        assertEquals("github.com", repo?.host)
        assertNull(normalizeClusterRepo(ClusterRepo("  ")))
    }

    @Test
    fun webUrlOfEveryAddressForm() {
        assertEquals("https://github.com/example-org/cluster-config", repoWebUrl("git@github.com:example-org/cluster-config.git"))
        assertEquals("https://gitlab.example.org/group/sub/infra", repoWebUrl("ssh://git@gitlab.example.org/group/sub/infra.git"))
        assertEquals("https://git.home.example/example-org/cluster-config", repoWebUrl("https://git.home.example/example-org/cluster-config/"))
        assertEquals("", repoWebUrl("not a repo"))
    }

    @Test
    fun repoSourceIsTheRootAtTheHead() {
        val info = decodeGitRepoInfo(
            """{"defaultBranch":"main","branch":"main","headSha":"0123456789abcdef0123456789abcdef01234567","webURL":"https://github.com/example-org/cluster-config","private":true,"canPush":true,"forge":"github"}""",
        )
        val src = repoSource(ClusterRepo("git@github.com:example-org/cluster-config.git"), info)
        assertEquals("", src.path)
        assertEquals("main", src.tracked)
        assertEquals(info.headSha, src.ref)
        assertEquals(SourceReadPath.FORGE, src.readPath)
        assertEquals("github.com", src.host)
        assertEquals("main", proposalBaseRef(src.tracked))
        assertEquals("main @ 0123456", repoCrumb(info))
    }

    @Test
    fun repoSourceWithoutAWebPageBuildsOne() {
        val src = repoSource(ClusterRepo("git@gitea.home.example:ops/fleet.git", branch = "dev"), GitRepoInfo(defaultBranch = "main"))
        assertEquals("https://gitea.home.example/ops/fleet", src.webURL)
        assertEquals("dev", src.tracked)
    }

    @Test
    fun storedJsonRoundTrips() {
        val repo = ClusterRepo("https://github.com/example-org/cluster-config", talosDir = "talos")
        assertEquals(repo, decodeClusterRepo(encodeClusterRepo(repo)))
        assertNull(decodeClusterRepo("garbage"))
        assertNull(decodeClusterRepo("""{"repoURL":" "}"""))
    }

    @Test
    fun backupRoundTripKeepsTheRepositoryOfKnownClusters() {
        val repo = ClusterRepo("https://github.com/example-org/cluster-config", "main", "talos", "kubernetes")
        val clusters = backupClusters(listOf("fp-lab", "fp-other"), emptyMap(), emptyMap(), emptySet(), emptyMap(), emptyMap(), repos = mapOf("fp-lab" to repo))
        assertEquals(BackupClusterRepo(repo.repoURL, "main", "talos", "kubernetes"), clusters["fp-lab"]?.repository)
        assertNull(clusters["fp-other"]?.repository)
        assertEquals(mapOf("fp-lab" to repo), restoredClusters(clusters, listOf("fp-lab")).repos)
        val blank = mapOf("fp-lab" to BackupCluster(repository = BackupClusterRepo(" ")), "fp-new" to BackupCluster(repository = BackupClusterRepo(repo.repoURL)))
        assertTrue(restoredClusters(blank, listOf("fp-lab")).repos.isEmpty())
    }
}
