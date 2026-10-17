package name.levis.ichor.data

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
}
