package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GitOpsEditCheckTest {
    @Test
    fun `decodes a build that changes one object`() {
        val json = """{"sha":"abc","kind":"kustomize","dir":"apps/web","ok":true,"errors":[],
            "diff":{"name":"apps/web/deployment.yaml","resources":[{"group":"apps","version":"v1","kind":"Deployment",
            "namespace":"web","name":"web","change":"changed","diff":"--- a\n+++ b\n"}],"warnings":[]},"unchanged":2}"""
        val c = TalosJson.decodeFromString(GitOpsEditCheck.serializer(), json)
        assertTrue(c.built)
        assertEquals(DiffChange.CHANGED, c.diff.resources.single().change)
        assertEquals(2, c.unchanged)
    }

    @Test
    fun `a failed build and a chart are not built`() {
        val failed = TalosJson.decodeFromString(
            GitOpsEditCheck.serializer(),
            """{"kind":"kustomize","ok":false,"errors":[{"message":"no matches","path":"apps/web/patch.yaml","line":3}]}""",
        )
        assertFalse(failed.built)
        assertEquals(3, failed.errors.single().line)
        assertFalse(TalosJson.decodeFromString(GitOpsEditCheck.serializer(), """{"kind":"helm","ok":true}""").built)
    }

    @Test
    fun `the Flux spec quotes its path`() {
        assertEquals("""{"path":"./apps/\"web\""}""", fluxCheckSpec("./apps/\"web\""))
    }
}
