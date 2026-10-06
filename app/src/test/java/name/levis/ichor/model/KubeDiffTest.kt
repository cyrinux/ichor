package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeDiffTest {
    // Shaped like KubeFluxDiff's answer (kube_flux_diff.go), already sorted by the Go core.
    private val json = """
        {"kind":"Kustomization","namespace":"flux-system","name":"apps","futureField":1,
         "revision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d","applied":"main@sha1:91c0a7e6d5b4c3a29180f7e6d5c4b3a291807f6e",
         "warnings":null,"resources":[
          {"group":"batch","version":"v1","kind":"Job","namespace":"web","name":"migrate","change":"error","diff":"","truncated":false,
           "error":"Kubernetes API (422 Invalid): spec.template: field is immutable"},
          {"group":"","version":"v1","kind":"ConfigMap","namespace":"web","name":"settings","change":"created",
           "diff":"--- live\n+++ wanted\n@@ -0,0 +1,2 @@\n+apiVersion: v1\n+kind: ConfigMap\n"},
          {"group":"apps","version":"v1","kind":"Deployment","namespace":"web","name":"web","change":"changed",
           "diff":"--- live\n+++ wanted\n@@ -40,3 +40,3 @@\n     spec:\n-      image: web:1\n+      image: web:2\n","truncated":true},
          {"group":"","version":"v1","kind":"Secret","namespace":"web","name":"creds","change":"encrypted"},
          {"group":"","version":"v1","kind":"Namespace","namespace":"","name":"web","change":"unchanged"},
          {"group":"","version":"v1","kind":"Service","namespace":"web","name":"web","change":"unchanged"}]}
    """.trimIndent()

    private val diff = TalosJson.decodeFromString(FluxDiff.serializer(), json)

    @Test
    fun decodesTheGoAnswer() {
        assertEquals("apps", diff.name)
        assertEquals(6, diff.resources.size)
        assertEquals(emptyList<String>(), diff.warnings) // null on the wire
        assertEquals(DiffChange.ERROR, diff.resources[0].change)
        assertTrue(diff.resources[0].error.contains("immutable"))
        assertTrue(diff.resources[2].truncated)
        assertEquals("apps/Deployment/web/web", diff.resources[2].key)
    }

    @Test
    fun countsAndFolds() {
        assertEquals(
            listOf(DiffChange.ERROR to 1, DiffChange.CREATED to 1, DiffChange.CHANGED to 1, DiffChange.ENCRYPTED to 1, DiffChange.UNCHANGED to 2),
            diff.counts,
        )
        assertEquals(listOf("migrate", "settings", "web", "creds"), diff.changed.map { it.name })
        assertEquals(listOf("Namespace", "Service"), diff.unchanged.map { it.kind })
        assertTrue(diff.newRevision)
        assertFalse(diff.inSync)
    }

    @Test
    fun inSyncWhenNothingWouldBeWritten() {
        val quiet = diff.copy(
            resources = listOf(
                KubeDiffResource(kind = "Secret", name = "s", change = DiffChange.ENCRYPTED),
                KubeDiffResource(kind = "ConfigMap", name = "c", change = DiffChange.IGNORED),
                KubeDiffResource(kind = "Service", name = "w", change = DiffChange.UNCHANGED),
            ),
            applied = diff.revision,
        )
        assertTrue(quiet.inSync)
        assertFalse(quiet.newRevision)
        // An error is not "in sync": the reconcile would fail.
        assertFalse(quiet.copy(resources = quiet.resources + KubeDiffResource(name = "j", change = DiffChange.ERROR)).inSync)
    }

    @Test
    fun parsesDiffLines() {
        val lines = diff.resources[2].lines
        assertEquals(
            listOf(
                DiffLine(DiffLine.Kind.HUNK, "@@ -40,3 +40,3 @@"),
                DiffLine(DiffLine.Kind.CONTEXT, "    spec:"),
                DiffLine(DiffLine.Kind.REMOVED, "      image: web:1"),
                DiffLine(DiffLine.Kind.ADDED, "      image: web:2"),
            ),
            lines,
        )
        assertEquals(emptyList<DiffLine>(), parseDiffLines(""))
    }

    @Test
    fun keepsARemovedLineThatLooksLikeAHeader() {
        // A removed "-- note" line reads "--- note": only the first two lines are the header.
        val lines = parseDiffLines("--- live\n+++ wanted\n@@ -1,1 +0,0 @@\n--- note\n")
        assertEquals(DiffLine(DiffLine.Kind.REMOVED, "-- note"), lines.last())
        assertEquals(2, lines.size)
    }

    @Test
    fun unknownChangeFallsBackToUnchanged() {
        val r = TalosJson.decodeFromString(KubeDiffResource.serializer(), """{"kind":"X","name":"x","change":"teleported"}""")
        assertEquals(DiffChange.UNCHANGED, r.change)
    }
}
