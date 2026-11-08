package name.levis.ichor.model

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GitProposalTest {

    @Test
    fun aBranchIsTheBase() {
        assertEquals("main", proposalBaseRef("main"))
        assertEquals("release/2.x", proposalBaseRef("release/2.x"))
        assertEquals("feature-1", proposalBaseRef(" feature-1 "))
        assertEquals("main", proposalBaseRef("refs/heads/main"))
    }

    @Test
    fun aCommitATagOrARangeIsNotABranch() {
        assertNull(proposalBaseRef("1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d"))
        assertNull(proposalBaseRef("v1.2.3"))
        assertNull(proposalBaseRef("1.2.3"))
        assertNull(proposalBaseRef("v2.0.0-rc.1"))
        assertNull(proposalBaseRef("refs/tags/v1.2.3"))
        assertNull(proposalBaseRef(">=1.0.0 <2.0.0"))
        assertNull(proposalBaseRef("*"))
        assertNull(proposalBaseRef("HEAD"))
        assertNull(proposalBaseRef("1.x"))
        assertNull(proposalBaseRef("a..b"))
        assertNull(proposalBaseRef("-main"))
        assertNull(proposalBaseRef(""))
    }

    @Test
    fun theKindComesFromTheFirstTopLevelKindLine() {
        val yaml = """
            # a comment
            apiVersion: apps/v1
            kind: Deployment # the app
            metadata:
              name: podinfo
              labels:
                kind: not-this
        """.trimIndent()

        assertEquals(ManifestType("apps", "v1", "Deployment"), manifestType("apps/podinfo/deployment.yaml", yaml))
        assertEquals(ManifestType("", "v1", "ConfigMap"), manifestType("cm.yml", "apiVersion: v1\nkind: \"ConfigMap\"\n"))
    }

    @Test
    fun aKustomizationOrAValuesFileHasNoSchemaHelp() {
        val kustomization = "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: [deployment.yaml]\n"

        assertNull(manifestType("apps/podinfo/kustomization.yaml", kustomization))
        assertNull(manifestType("apps/podinfo/base.yaml", kustomization))
        assertNull(manifestType("charts/podinfo/values.yaml", "replicaCount: 2\nimage:\n  tag: 6.7.1\n"))
        assertNull(manifestType("README.md", "apiVersion: v1\nkind: ConfigMap\n"))
    }

    @Test
    fun onlyTheFirstDocumentCounts() {
        val yaml = "apiVersion: v1\nmetadata: {}\n---\nkind: Service\n"

        assertNull(manifestType("svc.yaml", yaml))
    }

    @Test
    fun theOptionsCarryEveryKey() {
        val json = gitProposalJson(
            "https://github.com/example-org/platform.git",
            GitProposal(
                path = "apps/podinfo/deployment.yaml",
                content = "kind: Deployment\n",
                fileSha = "abc123",
                baseRef = "main",
                message = "Update apps/podinfo/deployment.yaml",
                title = "Update apps/podinfo/deployment.yaml",
                body = "",
                branch = "",
                draft = true,
            ),
        )

        val o = Json.parseToJsonElement(json).jsonObject
        assertEquals(
            setOf("repoURL", "baseRef", "path", "content", "fileSha", "message", "branch", "title", "body", "draft"),
            o.keys,
        )
        assertEquals("https://github.com/example-org/platform.git", o.getValue("repoURL").jsonPrimitive.content)
        assertEquals("abc123", o.getValue("fileSha").jsonPrimitive.content)
        assertEquals(JsonPrimitive(""), o.getValue("branch"))
        assertTrue(o.getValue("draft").jsonPrimitive.boolean)
    }

    @Test
    fun aStaleFileIsToldByTheCoresMessage() {
        assertTrue(isStaleFileError("the file changed on main since you opened it: reload and try again"))
        assertFalse(isStaleFileError("turn off screenshot mode to propose a change: the file was read with masked names"))
        assertFalse(isStaleFileError(null))
    }

    @Test
    fun proposingNeedsAForgeAndAToken() {
        val src = GitSource(repoURL = "https://github.com/example-org/platform.git", webURL = "https://github.com/example-org/platform", browsable = true)

        assertNull(proposeBlock(src, lacksToken = false))
        assertEquals(ProposeBlock.NoToken("github.com"), proposeBlock(src, lacksToken = true))
        assertEquals(ProposeBlock.NotEditable, proposeBlock(src.copy(webURL = "", browsable = false), lacksToken = false))
        assertEquals(ProposeBlock.NotEditable, proposeBlock(src.copy(kind = GitSource.KIND_CHART), lacksToken = false))
    }

    @Test
    fun theToolIsNamedAsTheAppsDo() {
        assertEquals("Argo CD", gitOpsToolLabel("argocd"))
        assertEquals("Flux", gitOpsToolLabel("flux"))
    }
}
