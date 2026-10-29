package name.levis.ichor.model

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ClusterUpdatesTest {
    // The shape of demoClusterUpdates() (go/ichorgo/updates_demo.go), cut down.
    private val json = """
        {"at":1767323045000,
         "images":[
          {"repo":"ghcr.io/stefanprodan/podinfo","tag":"6.7.0","digest":"","workloads":[{"kind":"Deployment","namespace":"demo","name":"podinfo","containers":["podinfo"]}],
           "owner":{"tool":"flux","kind":"Kustomization","namespace":"flux-system","name":"podinfo"},
           "current":"6.7.0","patch":"6.7.1","minor":"6.9.2","shape":"x.y.z","source":"ghcr.io",
           "edit":{"op":"image","kind":"Deployment","name":"podinfo","container":"podinfo","image":"ghcr.io/stefanprodan/podinfo:6.7.1","path":""}},
          {"repo":"ghcr.io/example-org/worker","tag":"2.3.0","digest":"","workloads":[],"owner":{"tool":"argocd","kind":"Application","namespace":"argocd","name":"hello-ichor"},
           "current":"2.3.0","patch":"2.3.1","automatedBy":"argocd-image-updater","edit":null},
          {"repo":"busybox","tag":"latest","digest":"","workloads":[],"owner":null,"current":"latest","reason":"not a version","edit":null},
          {"repo":"nginx","tag":"1.27.0","digest":"","workloads":[],"owner":null,"current":"1.27.0","edit":null}
         ],
         "charts":[
          {"name":"ingress-nginx","version":"4.12.1","repoURL":"https://kubernetes.github.io/ingress-nginx",
           "owner":{"tool":"flux","kind":"HelmRelease","namespace":"ingress-nginx","name":"ingress-nginx"},
           "current":"4.12.1","patch":"4.12.4","minor":"4.13.3","source":"kubernetes.github.io",
           "edit":{"op":"path","path":"spec.chart.spec.version","value":"4.12.4"}}
         ],
         "skipped":[],"lookups":{"queried":8,"failed":1}}
    """.trimIndent()

    private val updates = TalosJson.decodeFromString(ClusterUpdates.serializer(), json)

    @Test
    fun sectionsSplitWhatToBumpFromWhatCannotBeCompared() {
        val s = updateSections(updates)
        assertEquals(listOf("ghcr.io/stefanprodan/podinfo", "ghcr.io/example-org/worker"), s.images.map { it.name })
        assertEquals(listOf("ingress-nginx"), s.charts.map { it.name })
        // busybox:latest says why; nginx is up to date and is not listed.
        assertEquals(listOf("busybox"), s.notComparable.map { it.name })
        assertEquals(2, s.outdated)
        assertEquals(8, updates.lookups.queried)
    }

    @Test
    fun anAutomatedImageShowsItsBadgeAndNoChip() {
        val worker = updateSections(updates).images[1]
        assertEquals(UpdateAutomation.ARGO_IMAGE_UPDATER, worker.automation)
        assertTrue(worker.candidates.isEmpty())
        assertFalse(worker.editable)
    }

    @Test
    fun chipsAreTheCandidatesInStepOrder() {
        val podinfo = updateSections(updates).images[0]
        assertEquals(listOf(UpdateStep.PATCH to "6.7.1", UpdateStep.MINOR to "6.9.2"), podinfo.candidates.map { it.step to it.version })
        assertEquals("6.9.2", podinfo.newest)
        assertTrue(podinfo.editable)
    }

    @Test
    fun theItemSentBackCarriesTheVersionPicked() {
        val podinfo = updateSections(updates).images[0]
        val item = Json.parseToJsonElement(updateItemJson(podinfo, "6.9.2")).jsonObject
        assertEquals("6.9.2", item["version"]?.jsonPrimitive?.content)
        assertEquals("flux", item["owner"]?.jsonObject?.get("tool")?.jsonPrimitive?.content)
        assertEquals("image", item["edit"]?.jsonObject?.get("op")?.jsonPrimitive?.content)
    }

    @Test
    fun anImageTargetBecomesAPrefilledImageEditNamedAfterTheUpdate() {
        val podinfo = updateSections(updates).images[0]
        val target = UpdateEditTarget(
            app = UpdateEditApp("flux", "Kustomization", "flux-system", "podinfo"),
            path = "apps/podinfo/deployment.yaml",
            line = 17,
            edit = UpdateQuickEdit(op = "image", kind = "Deployment", name = "podinfo", container = "podinfo", image = "ghcr.io/stefanprodan/podinfo:6.7.1"),
        )
        val edit = updateQuickEdit(target, podinfo, "6.7.1")!!
        assertEquals(QuickEdit.OP_IMAGE, edit.op)
        assertEquals("podinfo", edit.container)
        assertEquals("Update podinfo to 6.7.1", quickEditTitle(edit))
        assertTrue(edit.body.contains("`6.7.0` to `6.7.1`"))
        assertTrue(edit.body.contains("Source: ghcr.io"))
    }

    @Test
    fun aChartTargetIsAPathEdit() {
        val chart = updateSections(updates).charts[0]
        val target = UpdateEditTarget(
            app = UpdateEditApp("flux", "Kustomization", "flux-system", "apps"),
            path = "apps/ingress.yaml",
            edit = UpdateQuickEdit(op = "path", kind = "HelmRelease", name = "ingress-nginx", path = "spec.chart.spec.version", value = "4.13.3"),
        )
        val edit = updateQuickEdit(target, chart, "4.13.3")!!
        assertEquals("""{"op":"path","documentIndex":0,"kind":"HelmRelease","name":"ingress-nginx","path":"spec.chart.spec.version","value":"4.13.3"}""", quickEditJson(edit))
        assertEquals(edit.copy(), decodeQuickEdit(encodeQuickEdit(edit)))
        assertEquals("Update ingress-nginx to 4.13.3", quickEditTitle(edit))
    }

    @Test
    fun noEditNoQuickEdit() {
        val chart = updateSections(updates).charts[0]
        assertNull(updateQuickEdit(UpdateEditTarget(note = "no Argo CD or Flux app declares HelmRelease x in Git"), chart, "4.12.4"))
    }

    @Test
    fun aKustomizePrefillPicksItsEntryAndBareTag() {
        val targets = QuickEditTargets(
            documents = listOf(QuickEditDocument(0, "Deployment", "api", containers = listOf(QuickEditContainer("api", "ghcr.io/example-org/api:2.0.0")))),
            kustomizeImages = listOf(QuickEditKustomizeImage(1, "ghcr.io/example-org/api", newTag = "2.0.0")),
        )
        val choices = quickImageChoices(targets)
        val kust = QuickEdit.kustomize(1, "ghcr.io/example-org/api", "2.1.0")
        assertEquals(0, quickPrefillIndex(choices, kust))
        assertEquals("2.1.0", quickPrefillTag(kust))
        val plain = QuickEdit.image(0, "api", "ghcr.io/example-org/api:2.1.0", "Deployment", "api")
        assertEquals(choices.indexOfFirst { it.container == "api" }, quickPrefillIndex(choices, plain))
        assertEquals("2.1.0", quickPrefillTag(plain))
    }
}
