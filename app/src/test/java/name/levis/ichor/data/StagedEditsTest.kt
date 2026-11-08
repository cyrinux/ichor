package name.levis.ichor.data

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import name.levis.ichor.model.GitProposal
import name.levis.ichor.model.GitSource
import name.levis.ichor.model.gitProposalJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class StagedEditsTest {
    private val key = StagedKey("https://git.example.org/example-org/platform.git", "main")
    private fun file(path: String, draft: String = "b: 2\n") = StagedFile(path, "a: 1\n", "sha-$path", draft)

    @Test
    fun stagingAgainReplacesTheFileInPlace() {
        val staged = StagedEdits()
        staged.stage(key, file("apps/a.yaml"))
        staged.stage(key, file("apps/b.yaml"))
        staged.stage(key, file("apps/a.yaml", draft = "c: 3\n"))

        assertEquals(listOf("apps/a.yaml", "apps/b.yaml"), staged.of(key).map { it.path })
        assertEquals("c: 3\n", staged.of(key).first().draft)
    }

    @Test
    fun eachRepositoryAndBranchHasItsOwnFiles() {
        val staged = StagedEdits()
        val other = key.copy(baseRef = "release")
        staged.stage(key, file("apps/a.yaml"))
        staged.stage(other, file("apps/b.yaml"))

        assertEquals(listOf("apps/a.yaml"), staged.of(key).map { it.path })
        staged.clear(key)
        assertTrue(staged.of(key).isEmpty())
        assertEquals(1, staged.of(other).size)
    }

    @Test
    fun removingTheLastFileDropsTheKey() {
        val staged = StagedEdits()
        staged.stage(key, file("apps/a.yaml"))
        staged.remove(key, "apps/a.yaml")

        assertFalse(key in staged.files.value)
    }

    @Test
    fun aFullPullRequestTakesNoNewFile() {
        val staged = StagedEdits()
        repeat(MAX_STAGED_FILES) { staged.stage(key, file("apps/f$it.yaml")) }

        assertFalse(staged.canStage(key, "apps/new.yaml"))
        assertTrue(staged.canStage(key, "apps/f0.yaml"))
    }

    @Test
    fun theKeyIsTheBranchTheFilesAreProposedTo() {
        val src = GitSource(repoURL = key.repoURL, tracked = "main", revision = "0123456789abcdef0123456789abcdef01234567")
        assertEquals(key, stagedKeyOf(src))
    }

    @Test
    fun severalFilesAreTitledAfterTheirCommonDirectory() {
        assertEquals("apps/podinfo", stagedDefaultsPath(listOf("apps/podinfo/deployment.yaml", "apps/podinfo/kustomization.yaml")))
        assertEquals("apps", stagedDefaultsPath(listOf("apps/podinfo/deployment.yaml", "apps/grafana/values.yaml")))
        assertEquals("talconfig.yaml", stagedDefaultsPath(listOf("talconfig.yaml", "clusters/lab/flux.yaml")))
        assertEquals("apps/a.yaml", stagedDefaultsPath(listOf("apps/a.yaml")))
    }

    @Test
    fun theOptionsCarryTheFiles() {
        val p = GitProposal(
            path = "",
            content = "",
            fileSha = "",
            baseRef = "main",
            message = "Update apps",
            title = "Update apps",
            body = "",
            branch = "",
            files = listOf(file("apps/a.yaml").proposal, StagedFile("apps/new.yaml", "", "", "c: 3\n").proposal),
        )
        val files = Json.parseToJsonElement(gitProposalJson(key.repoURL, p)).jsonObject.getValue("files").jsonArray

        assertEquals(2, files.size)
        val first = files[0].jsonObject
        assertEquals("apps/a.yaml", first.getValue("path").jsonPrimitive.content)
        assertEquals("b: 2\n", first.getValue("content").jsonPrimitive.content)
        assertEquals("sha-apps/a.yaml", first.getValue("fileSha").jsonPrimitive.content)
        assertEquals("", files[1].jsonObject.getValue("fileSha").jsonPrimitive.content)
    }
}
