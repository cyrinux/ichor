package name.levis.ichor.ui.flux

import name.levis.ichor.R
import name.levis.ichor.model.DiffChange
import name.levis.ichor.model.FluxRef
import name.levis.ichor.model.GitOpsSource
import name.levis.ichor.model.GitOpsSourceFind
import name.levis.ichor.model.GitSource
import name.levis.ichor.model.KubeDiffResource
import name.levis.ichor.model.ManifestMatch
import name.levis.ichor.model.findableSourceIndex
import name.levis.ichor.model.isFindableChange
import name.levis.ichor.ui.UiText
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DiffSourceFindTest {

    private val deployment = KubeDiffResource(group = "apps", version = "v1", kind = "Deployment", namespace = "podinfo", name = "podinfo", change = DiffChange.CHANGED)
    private val forge = GitSource(
        repoURL = "https://github.com/example-org/platform.git",
        webURL = "https://github.com/example-org/platform",
        tracked = "main",
        path = "apps/podinfo",
        kind = "kustomize",
        browsable = true,
    )

    @Test
    fun oneMatchOpensTheFileAtItsLine() {
        val result = GitOpsSourceFind(listOf(ManifestMatch("apps/podinfo/deployment.yaml", 4)), searched = 3)

        assertEquals(SourceFindOutcome.Open(0, "apps/podinfo/deployment.yaml", 4), sourceFindOutcome(0, result, deployment))
    }

    @Test
    fun severalMatchesAreOfferedToChoose() {
        val matches = listOf(ManifestMatch("apps/podinfo/a.yaml", 4), ManifestMatch("apps/podinfo/b.yaml", 12, 1))

        assertEquals(SourceFindOutcome.Choose(1, matches), sourceFindOutcome(1, GitOpsSourceFind(matches), deployment))
    }

    @Test
    fun noMatchShowsTheCoreNote() {
        val outcome = sourceFindOutcome(0, GitOpsSourceFind(note = "rendered by Helm; open the chart's templates"), deployment)

        assertEquals(SourceFindOutcome.Message(UiText.Raw("rendered by Helm; open the chart's templates")), outcome)
    }

    @Test
    fun noMatchWithoutNoteNamesTheObject() {
        val outcome = sourceFindOutcome(0, GitOpsSourceFind(searched = 17), deployment) as SourceFindOutcome.Message
        val text = outcome.text as UiText.Res

        assertEquals(R.string.gitops_find_none, text.id)
        assertEquals(listOf<Any>("Deployment", "podinfo"), text.args.toList())
        assertFalse(outcome.addToken)
    }

    @Test
    fun aMatchWithoutPathIsIgnored() {
        val result = GitOpsSourceFind(listOf(ManifestMatch(""), ManifestMatch("apps/podinfo/deployment.yaml", 7)))

        assertEquals(SourceFindOutcome.Open(2, "apps/podinfo/deployment.yaml", 7), sourceFindOutcome(2, result, deployment))
    }

    @Test
    fun theFirstReadableGitSourceIsSearched() {
        val values = forge.copy(kind = GitSource.KIND_VALUES, path = "")
        val chart = GitSource(repoURL = "https://charts.example.org", kind = GitSource.KIND_CHART, chart = "podinfo")

        assertEquals(2, findableSourceIndex(GitOpsSource(listOf(chart, values, forge))))
    }

    @Test
    fun aFluxArtifactIsSearchedWithoutAForge() {
        val flux = GitSource(path = "apps/podinfo", kind = "kustomize", fluxArtifact = true, fluxSource = FluxRef("GitRepository", "flux-system", "flux-system"))

        assertEquals(0, findableSourceIndex(GitOpsSource(listOf(flux))))
    }

    @Test
    fun noReadableSourceOffersNothing() {
        val unreadable = forge.copy(browsable = false)
        val chart = GitSource(kind = GitSource.KIND_CHART, chart = "podinfo", browsable = true)

        assertNull(findableSourceIndex(GitOpsSource(listOf(unreadable, chart))))
        assertNull(findableSourceIndex(GitOpsSource()))
    }

    @Test
    fun anObjectBeingDeletedIsNotLookedUp() {
        assertFalse(isFindableChange(DiffChange.DELETED))
        assertTrue(isFindableChange(DiffChange.CHANGED))
        assertTrue(isFindableChange(DiffChange.CREATED))
    }

    @Test
    fun theToolNamesMatchGitOpsSource() {
        assertEquals("flux", DiffTool.FLUX.sourceTool)
        assertEquals("argocd", DiffTool.ARGO.sourceTool)
    }
}
