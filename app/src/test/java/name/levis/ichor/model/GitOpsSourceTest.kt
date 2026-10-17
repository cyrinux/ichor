package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GitOpsSourceTest {

    private val commit = "1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d"
    private val forge = GitSource(
        repoURL = "https://github.com/example-org/platform.git",
        webURL = "https://github.com/example-org/platform",
        tracked = "main",
        revision = commit,
        path = "apps/podinfo",
        kind = "kustomize",
        browsable = true,
    )

    @Test
    fun aFluxArtifactIsReadFromTheClusterEvenWhenTheHostIsBrowsable() {
        val flux = forge.copy(fluxArtifact = true, fluxSource = FluxRef("GitRepository", "flux-system", "flux-system"))

        assertEquals(SourceReadPath.FLUX, flux.readPath)
    }

    @Test
    fun aFluxArtifactWithoutItsSourceFallsBackToTheForge() {
        assertEquals(SourceReadPath.FORGE, forge.copy(fluxArtifact = true).readPath)
    }

    @Test
    fun aSourceNeitherFluxNorBrowsableCannotBeRead() {
        assertEquals(SourceReadPath.NONE, forge.copy(browsable = false).readPath)
    }

    @Test
    fun theRefIsTheDeployedCommitElseTheTrackedRef() {
        assertEquals(commit, forge.ref)
        assertEquals("main", forge.copy(revision = "").ref)
    }

    @Test
    fun theHostComesFromTheWebPage() {
        assertEquals("github.com", forge.host)
        assertEquals("git.example.org:8443", forge.copy(webURL = "https://Git.Example.org:8443/example-org/platform").host)
        assertEquals("", forge.copy(webURL = "").host)
    }

    @Test
    fun theRowLabelIsThePathAtTheShortCommit() {
        assertEquals("apps/podinfo @ 1a2b3c4", sourceRowLabel(forge))
        assertEquals("/ @ main", sourceRowLabel(forge.copy(path = "", revision = "")))
        assertEquals("apps", sourceRowLabel(forge.copy(path = "apps", revision = "", tracked = "")))
    }

    @Test
    fun theBreadcrumbListsEachDirectoryWithItsFullPath() {
        assertEquals(listOf("apps" to "apps", "podinfo" to "apps/podinfo"), sourceBreadcrumbs("/apps/podinfo/"))
        assertEquals(emptyList<Pair<String, String>>(), sourceBreadcrumbs(""))
    }

    @Test
    fun valuesAndChartSourcesAreTold() {
        assertTrue(forge.copy(kind = "values").isValuesOnly)
        assertTrue(forge.copy(kind = "chart").isChart)
        assertFalse(forge.isChart)
    }

    @Test
    fun anEntryIsNamedByItsLastSegment() {
        val entry = GitEntry("apps/podinfo/deployment.yaml", "file", 512)

        assertEquals("deployment.yaml", entry.name)
        assertFalse(entry.isDir)
        assertTrue(GitEntry("apps", "dir").isDir)
    }

    @Test
    fun aFileLinkIsBuiltOnlyForKnownForges() {
        assertEquals(
            "https://github.com/example-org/platform/blob/$commit/apps/podinfo/deployment.yaml",
            forgeFileUrl(forge.webURL, commit, "apps/podinfo/deployment.yaml"),
        )
        assertEquals(
            "https://gitlab.example.org/example-org/platform/-/blob/main/values.yaml",
            forgeFileUrl("https://gitlab.example.org/example-org/platform", "main", "/values.yaml"),
        )
        assertEquals("https://git.example.org/example-org/platform", forgeFileUrl("https://git.example.org/example-org/platform", "main", "a.yaml"))
        assertEquals("", forgeFileUrl("", "main", "a.yaml"))
    }

    @Test
    fun aHostArgumentWithoutATokenNeedsOneExceptInTheDemo() {
        val host = GitHost("github.com")

        assertTrue(gitHostJsonLacksToken(gitHostJson(host, "", demo = false)))
        assertTrue(gitHostJsonLacksToken(gitHostJson(host, "  ", demo = false)))
        assertFalse(gitHostJsonLacksToken(gitHostJson(host, "ghp-test", demo = false)))
        assertFalse(gitHostJsonLacksToken(gitHostJson(host, "", demo = true)))
        assertTrue(gitHostJsonLacksToken("not json"))
    }

    @Test
    fun yamlFilesAreTold() {
        assertTrue(isYamlPath("apps/podinfo/kustomization.YAML"))
        assertTrue(isYamlPath("values.yml"))
        assertFalse(isYamlPath("README.md"))
    }
}
