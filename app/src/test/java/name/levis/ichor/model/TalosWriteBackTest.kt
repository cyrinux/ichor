package name.levis.ichor.model

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TalosWriteBackTest {
    @Test
    fun decodesThePatchAndItsChanges() {
        val patch = TalosJson.decodeFromString(
            TalosConfigPatch.serializer(),
            """{"patch":"machine:\n  install:\n    disk: /dev/nvme0n1\n","changes":[
              {"doc":0,"kind":"v1alpha1","path":"machine.network.nameservers","from":["192.0.2.1"],"to":null},
              {"doc":0,"kind":"v1alpha1","path":"machine.install.disk","from":"/dev/sda","to":"/dev/nvme0n1"},
              {"doc":0,"kind":"v1alpha1","path":"machine.kubelet.extraArgs","to":{"max-pods":"150"}}],
              "touchesSecrets":false,"notes":["machine.network.nameservers was removed"]}""",
        )
        assertTrue(patch.changes[0].removed)
        assertFalse(patch.changes[1].removed)
        assertEquals("machine.install.disk", patch.firstPath)
        assertEquals("/dev/sda", changeValueText(patch.changes[1].from))
        assertEquals("—", changeValueText(patch.changes[2].from))
        assertEquals("""{"max-pods":"150"}""", changeValueText(patch.changes[2].to))
    }

    @Test
    fun aTalosQuickEditGoesThroughTheRouteAndToTheCore() {
        val plan = TalosWriteBackPlan(
            sha = "abc", file = "talos/nodes/cp-1.yaml", patch = "machine:\n  install:\n    disk: /dev/nvme0n1\n",
            skipped = listOf(TalosRepoSkip("machine.time.servers", "set in talos/patches/time.yaml")),
        )
        val edit = QuickEdit.talos("cp-1", plan, "machine.install.disk")

        // The route keeps it whole.
        val back = decodeQuickEdit(encodeQuickEdit(edit))
        assertEquals(edit, back)
        assertEquals("Keep cp-1's config change in Git: machine.install.disk", quickEditTitle(edit))

        // The core gets only the keys of its op.
        val json = Json.parseToJsonElement(quickEditJson(edit)).jsonObject
        assertEquals(setOf("op", "documentIndex", "patch", "hostname", "create", "skipped"), json.keys)
        assertEquals("talos", json["op"]?.jsonPrimitive?.content)
        assertEquals("machine.time.servers: set in talos/patches/time.yaml", json["skipped"]?.jsonArray?.single()?.jsonPrimitive?.content)

        // Without a patch it is no quick edit.
        assertNotNull(back)
        assertEquals(null, decodeQuickEdit(encodeQuickEdit(edit.copy(patch = ""))))
    }
}
