package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TalosRepoTest {
    @Test
    fun decodesTheFilesAndPicksThePatches() {
        val files = TalosJson.decodeFromString(
            TalosRepoFiles.serializer(),
            """{"sha":"abc","generator":"talhelper","files":[
              {"path":"talos/talconfig.yaml","kind":"talconfig","why":"talconfig.yaml lists cp-1","line":7,"patch":false},
              {"path":"talos/patches/network.yaml","kind":"shared","why":"talconfig.yaml patches","patch":true},
              {"path":"talos/talconfig.yaml#L12","kind":"talconfig","why":"inline patch","line":12,"patch":true},
              {"path":"talos/nodes/cp-1.yaml","kind":"node","why":"talconfig.yaml nodes[cp-1].patches","patch":true},
              {"path":"talos/talsecret.sops.yaml","kind":"secrets","why":"encrypted with SOPS","patch":false}],"note":"","future":1}""",
        )
        assertEquals(TalosRepoFileKind.SECRETS, files.files.last().kind)
        assertEquals(setOf("talos/patches/network.yaml", "talos/talconfig.yaml#L12", "talos/nodes/cp-1.yaml"), files.defaultSelection)
        assertEquals("talos/talconfig.yaml", files.files[2].file)

        // The order is the files', never the order they were picked in; secrets never go.
        val picked = setOf("talos/nodes/cp-1.yaml", "talos/talsecret.sops.yaml", "talos/patches/network.yaml")
        assertEquals(listOf("talos/patches/network.yaml", "talos/nodes/cp-1.yaml"), orderedSelection(files.files, picked))
    }

    @Test
    fun decodesTheComparison() {
        val compare = TalosJson.decodeFromString(
            TalosRepoCompare.serializer(),
            """{"sha":"abc","files":["a.yaml"],"diff":{"kind":"file","name":"machine config","resources":[{"kind":"file","name":"machine config","change":"changed","diff":"-a\n+b\n"}],"warnings":[]},
              "same":false,"needsReboot":true,"skipped":[{"path":"b.yaml","why":"is not YAML"}],"draft":"version: v1alpha1\n"}""",
        )
        assertFalse(compare.same)
        assertTrue(compare.needsReboot)
        assertEquals("-a\n+b\n", compare.diff.resources.single().diff)
        assertEquals("b.yaml", compare.skipped.single().path)
    }

    @Test
    fun readsTheRoleFromMachineType() {
        assertEquals("controlplane", configRole("version: v1alpha1\nmachine:\n  network:\n    hostname: cp-1\n  type: controlplane\ncluster:\n  type: other\n"))
        assertEquals("controlplane", configRole("machine:\n  type: init\n"))
        assertEquals("worker", configRole("machine:\n  type: \"worker\"\n"))
        // A nested type is not the machine's.
        assertEquals("", configRole("machine:\n  install:\n    type: disk\ncluster:\n  type: x\n"))
    }
}
