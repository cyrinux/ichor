package name.levis.ichor.data

import name.levis.ichor.model.DiffChange
import name.levis.ichor.model.DiffLine
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.model.FluxRef
import name.levis.ichor.model.SourceReadPath
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GitOpsSourceDecodeTest {

    @Test
    fun aFluxSourceDecodesWithItsArtifactRef() {
        val json = """
            {"sources":[{"repoURL":"https://git.example.org/example-org/platform.git","webURL":"https://git.example.org/example-org/platform",
            "tracked":"main","revision":"1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d","path":"apps/podinfo","kind":"kustomize","chart":"",
            "valueFiles":null,"browsable":true,"fluxArtifact":true,"fluxSource":{"kind":"GitRepository","namespace":"flux-system","name":"flux-system"}}],"note":""}
        """.trimIndent()

        val source = decodeGitOpsSource(json).sources.single()

        assertEquals(FluxRef("GitRepository", "flux-system", "flux-system"), source.fluxSource)
        assertEquals(SourceReadPath.FLUX, source.readPath)
        assertEquals(emptyList<String>(), source.valueFiles)
    }

    @Test
    fun anArgoSourceWithoutFluxSourceDecodes() {
        val json = """{"sources":[{"repoURL":"git@github.com:example-org/platform.git","webURL":"https://github.com/example-org/platform","tracked":"HEAD","revision":"","path":"","kind":"directory","browsable":true,"fluxArtifact":false}],"note":"x"}"""

        val decoded = decodeGitOpsSource(json)

        assertNull(decoded.sources.single().fluxSource)
        assertEquals(SourceReadPath.FORGE, decoded.sources.single().readPath)
        assertEquals("x", decoded.note)
    }

    @Test
    fun aTreeAndAFileDecode() {
        val tree = decodeGitTree("""{"ref":"main","sha":"abc","entries":[{"path":"apps/podinfo","type":"dir","size":0},{"path":"README.md","type":"file","size":42}]}""")
        val file = decodeGitFile("""{"path":"README.md","sha":"abc","size":2000000,"content":"","truncated":true}""")

        assertEquals(listOf("podinfo", "README.md"), tree.entries.map { it.name })
        assertTrue(tree.entries.first().isDir)
        assertTrue(file.truncated)
        assertEquals(2_000_000L, file.size)
    }

    @Test
    fun aTreeWithoutEntriesIsEmpty() {
        assertEquals(emptyList<Any>(), decodeGitTree("""{"ref":"main","entries":null}""").entries)
    }

    @Test
    fun aProposalResultDecodes() {
        val result = decodeGitProposalResult(
            """{"url":"https://github.com/example-org/platform/pull/7","number":7,"branch":"ichor/podinfo-deployment-1a2b","commit":"9f8e7d6","forge":"github"}""",
        )

        assertEquals("https://github.com/example-org/platform/pull/7", result.url)
        assertEquals(7, result.number)
        assertEquals("ichor/podinfo-deployment-1a2b", result.branch)
        assertEquals("github", result.forge)
    }

    @Test
    fun theProposalDefaultsDecode() {
        val defaults = decodeGitProposalDefaults(
            """{"title":"Update apps/podinfo/deployment.yaml","message":"Update apps/podinfo/deployment.yaml","branch":"ichor/podinfo-deployment-1a2b","body":"Proposed from Ichor for Argo CD Application argocd/podinfo on lab."}""",
        )

        assertEquals("Update apps/podinfo/deployment.yaml", defaults.title)
        assertEquals("ichor/podinfo-deployment-1a2b", defaults.branch)
        assertTrue(defaults.body.startsWith("Proposed from Ichor"))
    }

    @Test
    fun aTextDiffDecodesAsAOneFileDiff() {
        val json = """{"kind":"file","name":"apps/podinfo/deployment.yaml","resources":[{"kind":"file","name":"apps/podinfo/deployment.yaml","change":"changed","diff":"--- a/x\n+++ b/x\n@@ -1 +1 @@\n-replicas: 1\n+replicas: 2\n"}],"warnings":[]}"""

        val diff = TalosJson.decodeFromString(FluxDiff.serializer(), json)

        val file = diff.resources.single()
        assertEquals(DiffChange.CHANGED, file.change)
        assertEquals(listOf(DiffLine.Kind.HUNK, DiffLine.Kind.REMOVED, DiffLine.Kind.ADDED), file.lines.map { it.kind })
    }
}
