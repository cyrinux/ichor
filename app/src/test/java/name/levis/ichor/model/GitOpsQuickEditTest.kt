package name.levis.ichor.model

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GitOpsQuickEditTest {

    @Test
    fun titlesNameTheContainerOrTheWorkload() {
        assertEquals("Bump podinfo to 6.7.0", quickEditTitle(QuickEdit.image(0, "podinfo", "ghcr.io/example-org/podinfo:6.7.0")))
        assertEquals("Scale podinfo to 3", quickEditTitle(QuickEdit.replicas(0, "podinfo", 3)))
        assertEquals("Bump app to 1.2.0", quickEditTitle(QuickEdit.kustomize(0, "ghcr.io/example-org/app", "1.2.0")))
        assertEquals("Scale web to 0", quickEditTitle(QuickEdit.replicas(0, "web", 0, kustomizeReplica = "web")))
    }

    @Test
    fun aKeepEditSendsTheLiveChangesOfItsObject() {
        val change = DiffLiveChange(listOf("spec", "replicas"), "spec.replicas", "2", "5", "int")
        val resource = KubeDiffResource(kind = "Deployment", name = "web", change = DiffChange.CHANGED, liveChanges = listOf(change))
        val edit = QuickEdit.keep(1, resource)
        assertEquals("Keep live changes of Deployment web", quickEditTitle(edit))

        val json = Json.parseToJsonElement(quickEditJson(edit)).jsonObject
        assertEquals(setOf("op", "documentIndex", "kind", "name", "changes"), json.keys)
        assertEquals("keep", json.getValue("op").jsonPrimitive.content)
        assertEquals(1, json.getValue("documentIndex").jsonPrimitive.int)
        assertEquals("Deployment", json.getValue("kind").jsonPrimitive.content)
        val sent = json.getValue("changes").jsonArray.single().jsonObject
        assertEquals(setOf("path", "label", "wanted", "live", "type"), sent.keys)

        // Through a route and back; a keep edit with nothing to keep is no edit.
        assertEquals(edit, decodeQuickEdit(encodeQuickEdit(edit)))
        assertNull(decodeQuickEdit(encodeQuickEdit(edit.copy(changes = emptyList()))))
    }

    @Test
    fun theQuickTitleHoldsWhileTheDraftIsTheQuickEdits() {
        val defaults = GitProposalDefaults(title = "Update deploy.yaml", message = "Update deploy.yaml", branch = "ichor/x", body = "b")
        val titled = quickProposalDefaults(defaults, "a: 2\n", "Scale web to 2", "a: 2\n")
        assertEquals(GitProposalDefaults("Scale web to 2", "Scale web to 2", "ichor/x", "b"), titled)
        assertEquals(defaults, quickProposalDefaults(defaults, "a: 2\n", "Scale web to 2", "a: 3\n"))
        assertEquals(defaults, quickProposalDefaults(defaults, null, null, "a: 2\n"))
    }

    @Test
    fun imagesSplitIntoRepositoryAndTag() {
        assertEquals("ghcr.io/example-org/app", imageRepository("ghcr.io/example-org/app:1.2"))
        assertEquals("1.2", imageTag("ghcr.io/example-org/app:1.2"))
        assertEquals("registry.example.com:5000/app", imageRepository("registry.example.com:5000/app"))
        assertEquals("", imageTag("registry.example.com:5000/app"))
        assertEquals("ghcr.io/example-org/app", imageRepository("ghcr.io/example-org/app:1.2@sha256:abc"))
        assertEquals("1.2", imageTag("ghcr.io/example-org/app:1.2@sha256:abc"))
        assertEquals("", imageTag("ghcr.io/example-org/app@sha256:abc"))
    }

    @Test
    fun aTagReplacesTheOldTagAndDigest() {
        assertEquals("ghcr.io/example-org/app:2.0", imageWithTag("ghcr.io/example-org/app:1.2@sha256:abc", "2.0"))
        assertEquals("registry.example.com:5000/app:2.0", imageWithTag("registry.example.com:5000/app", " 2.0 "))
    }

    @Test
    fun tagsAreValidated() {
        assertTrue(isValidImageTag("6.7.0"))
        assertTrue(isValidImageTag("v1.2.3-rc.1_build"))
        assertFalse(isValidImageTag(""))
        assertFalse(isValidImageTag("a/b"))
        assertFalse(isValidImageTag("1 2"))
        assertFalse(isValidImageTag(".hidden"))
        assertFalse(isValidImageTag("sha256:" + "0".repeat(64)))
    }

    @Test
    fun theEditJsonCarriesOnlyItsOpKeys() {
        val image = Json.parseToJsonElement(quickEditJson(QuickEdit.image(1, "app", "ghcr.io/example-org/app:2", "Deployment", "web"))).jsonObject
        assertEquals(setOf("op", "documentIndex", "kind", "name", "container", "image"), image.keys)
        assertEquals(1, image.getValue("documentIndex").jsonPrimitive.int)
        assertEquals("Deployment", image.getValue("kind").jsonPrimitive.content)
        assertEquals("web", image.getValue("name").jsonPrimitive.content)
        val replicas = Json.parseToJsonElement(quickEditJson(QuickEdit.replicas(0, "app", 4, kind = "StatefulSet"))).jsonObject
        assertEquals(setOf("op", "documentIndex", "kind", "name", "replicas"), replicas.keys)
        assertEquals("app", replicas.getValue("name").jsonPrimitive.content)
        val bare = Json.parseToJsonElement(quickEditJson(QuickEdit.replicas(0, "app", 4))).jsonObject
        assertEquals(setOf("op", "documentIndex", "replicas"), bare.keys)
        val kustomize = Json.parseToJsonElement(quickEditJson(QuickEdit.kustomize(0, "ghcr.io/example-org/app", "2"))).jsonObject
        assertEquals(setOf("op", "documentIndex", "kustomizeImage", "image"), kustomize.keys)
        assertEquals("2", kustomize.getValue("image").jsonPrimitive.content)
        val kReplicas = Json.parseToJsonElement(quickEditJson(QuickEdit.replicas(0, "web", 2, kustomizeReplica = "web"))).jsonObject
        assertEquals(setOf("op", "documentIndex", "kustomizeReplica", "replicas"), kReplicas.keys)
        // A kustomization entry never carries a workload's kind and name.
        val kReplicasKind = Json.parseToJsonElement(quickEditJson(QuickEdit.replicas(0, "web", 2, kustomizeReplica = "web", kind = "Deployment"))).jsonObject
        assertEquals(setOf("op", "documentIndex", "kustomizeReplica", "replicas"), kReplicasKind.keys)
    }

    @Test
    fun aRouteEditRoundTrips() {
        val edit = QuickEdit.replicas(2, "web", 5, kind = "Deployment")
        assertEquals(edit, decodeQuickEdit(encodeQuickEdit(edit)))
        val kustomize = QuickEdit.kustomize(0, "ghcr.io/example-org/app", "3")
        assertEquals(kustomize, decodeQuickEdit(encodeQuickEdit(kustomize)))
        assertNull(decodeQuickEdit(""))
        assertNull(decodeQuickEdit("not json"))
        assertNull(decodeQuickEdit("""{"op":"delete"}"""))
    }

    @Test
    fun targetsAndResultsDecodeLeniently() {
        val targets = decodeQuickEditTargets(
            """{"documents":[{"index":0,"kind":"Deployment","name":"web","replicas":2,"scalable":true,"containers":[{"name":"web","image":"ghcr.io/example-org/web:1","line":9}],"extra":1},
               {"index":1,"kind":"CronJob","name":"job","scalable":false,"containers":null}],"kustomizeImages":null,"kustomizeReplicas":null}""",
        )
        assertEquals(2, targets.documents.size)
        assertEquals(listOf("web"), quickReplicaChoices(targets).map { it.name })
        assertEquals(2, quickReplicaChoices(targets).single().current)
        assertEquals(listOf("web"), targets.withContainers.map { it.name })
        assertTrue(targets.canEditImage && targets.canEditReplicas)
        assertTrue(decodeQuickEditTargets("{}").isEmpty)
        val result = decodeQuickEditResult("""{"content":"a: 1\n","changed":true,"line":3}""")
        assertEquals(QuickEditResult("a: 1\n", true, 3), result)
        assertFalse(decodeQuickEditResult("{}").changed)
    }

    @Test
    fun choicesPutKustomizeEntriesFirstAndLabelSeveralWorkloads() {
        val targets = QuickEditTargets(
            documents = listOf(
                QuickEditDocument(0, "Deployment", "web", 1, true, listOf(QuickEditContainer("app", "ghcr.io/example-org/web:1"))),
                QuickEditDocument(1, "Deployment", "api", 1, true, listOf(QuickEditContainer("app", "ghcr.io/example-org/api:1"))),
            ),
            kustomizeImages = listOf(QuickEditKustomizeImage(2, "ghcr.io/example-org/web", newTag = "1")),
        )
        val choices = quickImageChoices(targets)
        assertEquals(listOf("ghcr.io/example-org/web", "web/app", "api/app"), choices.map { it.label })
        assertTrue(choices[0].isKustomize)
        assertEquals("ghcr.io/example-org/web:1", choices[0].image)
        assertEquals(QuickEdit.kustomize(2, "ghcr.io/example-org/web", "2"), choices[0].edit(" 2"))
        assertEquals(QuickEdit.image(1, "app", "ghcr.io/example-org/api:2", "Deployment", "api"), choices[2].edit("2"))
        val json = Json.parseToJsonElement(quickEditJson(choices[2].edit("2"))).jsonObject
        assertEquals(setOf("op", "documentIndex", "kind", "name", "container", "image"), json.keys)
        assertEquals("api", json.getValue("name").jsonPrimitive.content)
        assertEquals(setOf("op", "documentIndex", "kind", "name", "replicas"), Json.parseToJsonElement(quickEditJson(quickReplicaChoices(targets)[1].edit(3))).jsonObject.keys)
    }

    @Test
    fun anOverriddenContainerOrADigestEntryIsNotAChoice() {
        val targets = QuickEditTargets(
            documents = listOf(
                QuickEditDocument(0, "Deployment", "web", null, true, listOf(QuickEditContainer("app", "ghcr.io/example-org/web:1", overriddenBy = "ghcr.io/example-org/web"))),
            ),
            kustomizeImages = listOf(QuickEditKustomizeImage(name = "ghcr.io/example-org/pinned", digest = "sha256:abc")),
        )
        assertTrue(quickImageChoices(targets).isEmpty())
        assertFalse(targets.canEditImage)
        assertEquals(1, quickReplicaChoices(targets).single().current)
    }

    @Test
    fun kustomizeReplicasAreChoices() {
        val targets = QuickEditTargets(kustomizeReplicas = listOf(QuickEditKustomizeReplica(0, "web", 3)))
        val choice = quickReplicaChoices(targets).single()
        assertEquals(3, choice.current)
        assertEquals(QuickEdit.replicas(0, "web", 5, kustomizeReplica = "web"), choice.edit(5))
    }

    @Test
    fun aKustomizeEntryMakesItsNewName() {
        assertEquals("ghcr.io/example-org/fork:2", QuickEditKustomizeImage(name = "app", newName = "ghcr.io/example-org/fork", newTag = "2").image)
        assertEquals("app", QuickEditKustomizeImage(name = "app").image)
    }

    @Test
    fun chipsOfferThePreviousAndTheRunningTag() {
        val image = "ghcr.io/example-org/web:1.3"
        val revisions = listOf(
            KubeRevision(5, images = listOf("ghcr.io/example-org/web:1.2", "ghcr.io/example-org/sidecar:9"), current = true),
            KubeRevision(4, images = listOf("ghcr.io/example-org/web:1.2")),
            KubeRevision(3, images = listOf("ghcr.io/example-org/web:1.1")),
        )
        // The file says 1.3 but the cluster still runs 1.2: running 1.2, previous 1.1.
        assertEquals(QuickTagChips(previous = "1.1", running = "1.2"), quickTagChips(image, emptyList(), revisions))
        // In sync: no running chip, the previous is the newest other tag.
        val synced = listOf(KubeRevision(2, images = listOf(image), current = true), KubeRevision(1, images = listOf("ghcr.io/example-org/web:1.0")))
        assertEquals(QuickTagChips(previous = "1.0"), quickTagChips(image, listOf(image), synced))
        // Flux, or no history: the app's live images alone.
        assertEquals(QuickTagChips(running = "0.9"), quickTagChips(image, listOf("ghcr.io/example-org/other:5", "ghcr.io/example-org/web:0.9"), emptyList()))
    }
}
