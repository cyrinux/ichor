package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class YamlCursorTest {
    private val yaml = """
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: web
          labels:
            app.kubernetes.io/name: web
        spec:
          replicas: 2
          template:
            spec:
              containers:
              - name: app
                image: example/app:1
                ports:
                  - containerPort: 80
                    protocol: TCP
                env:
                - name: MODE
                  value: "fast"
              # a comment: not a key
              volumes: []
        ---
        kind: ConfigMap
    """.trimIndent()

    /** The cursor just after the first [marker] on the line holding it. */
    private fun at(marker: String, text: String = yaml): YamlCursor = yamlCursorAt(text, text.indexOf(marker) + marker.length)

    @Test
    fun readsNestedMaps() {
        assertEquals("metadata.name", at("name: we").fieldPath)
        assertEquals("spec.replicas", at("replicas").fieldPath)
        assertEquals("apiVersion", at("apiVersion").fieldPath)
        assertEquals("metadata.labels.app.kubernetes.io/name", at("io/name").fieldPath)
    }

    @Test
    fun walksListItems() {
        assertEquals("spec.template.spec.containers.name", at("- name: ap").fieldPath)
        assertEquals("spec.template.spec.containers.image", at("image").fieldPath)
        assertEquals("spec.template.spec.containers.ports.containerPort", at("containerPort").fieldPath)
        assertEquals("spec.template.spec.containers.ports.protocol", at("protocol").fieldPath)
        assertEquals("spec.template.spec.containers.env.value", at("value").fieldPath)
        assertEquals("spec.template.spec.volumes", at("volumes").fieldPath)
    }

    @Test
    fun aKeyOpeningABlockAddsInsideIt() {
        val template = at("template")
        assertTrue(template.opensBlock)
        assertEquals("spec.template", template.addPath)
        assertEquals(4, template.addColumn)

        val image = at("image")
        assertFalse(image.opensBlock)
        assertEquals("spec.template.spec.containers", image.addPath)
        assertEquals(8, image.addColumn)
    }

    @Test
    fun aBlankLineCountsFromTheCursor() {
        val text = "spec:\n  template:\n    \n"
        val cursor = yamlCursorAt(text, text.indexOf("    \n") + 4)
        assertNull(cursor.key)
        assertEquals("spec.template", cursor.fieldPath)
        assertEquals("spec.template", cursor.addPath)

        val shallow = yamlCursorAt(text, text.indexOf("    \n") + 2)
        assertEquals("spec", shallow.addPath)
    }

    @Test
    fun stopsAtTheDocumentStart() {
        assertEquals("kind", at("kind: C").fieldPath)
    }

    @Test
    fun skipsComments() {
        val cursor = at("# a comm")
        assertEquals("spec.template.spec", cursor.fieldPath)
    }

    @Test
    fun quotedKeysAndCrlf() {
        val text = "data:\r\n  \"a.key\": x\r\n"
        assertEquals("data.a.key", yamlCursorAt(text, text.indexOf("x")).fieldPath)
    }

    @Test
    fun insertsBelowAKeyOrOnABlankLine() {
        val text = "spec:\n  replicas: 2\n"
        val (below, cursor) = insertYamlField(text, text.indexOf("2"), "paused", listItem = false)
        assertEquals("spec:\n  replicas: 2\n  paused: \n", below)
        assertEquals(below.indexOf("paused: ") + "paused: ".length, cursor)

        val (inside, _) = insertYamlField(text, 2, "selector", listItem = false)
        assertEquals("spec:\n  selector: \n  replicas: 2\n", inside)

        val blank = "spec:\n  containers:\n    \n"
        val (item, end) = insertYamlField(blank, blank.indexOf("    \n") + 4, "name", listItem = true)
        assertEquals("spec:\n  containers:\n    - name: \n", item)
        assertEquals(item.indexOf("name: ") + "name: ".length, end)
    }

    @Test
    fun decodesTheExplainJson() {
        val json = """{"path":"spec.strategy","type":"DeploymentStrategy","description":"How.","required":false,
            "children":[{"name":"type","type":"string","description":"Type.","required":false,"enum":["Recreate","RollingUpdate"]}]}"""
        val explain = TalosJson.decodeFromString(KubeExplain.serializer(), json)
        assertEquals(listOf("Recreate", "RollingUpdate"), explain.children.single().enum)
        assertTrue(explain.enum.isEmpty())
        assertFalse(explain.isList)
    }
}
