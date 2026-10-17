package name.levis.ichor.model

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeNodeMetaTest {
    private val read = TalosJson.decodeFromString(
        KubeNodeMeta.serializer(),
        """
        {"node":"worker-1","resourceVersion":"4242",
         "taints":[
          {"key":"maintenance","value":"true","effect":"NoSchedule","timeAdded":1760000000,"protected":false},
          {"key":"node.kubernetes.io/unschedulable","effect":"NoSchedule","timeAdded":0,"protected":true}],
         "labels":[
          {"key":"example.com/pool","value":"blue","protected":false},
          {"key":"kubernetes.io/hostname","value":"worker-1","protected":true},
          {"key":"topology.kubernetes.io/zone","value":"zone-a","protected":true}]}
        """,
    )

    private fun parse(json: String): JsonObject = Json.parseToJsonElement(json).jsonObject

    @Test fun readDecodesTaintsAndLabels() {
        assertEquals("worker-1", read.node)
        assertEquals("4242", read.resourceVersion)
        assertEquals(1760000000L, read.taints.first().timeAdded)
        assertEquals("", read.taints[1].value)
        assertTrue(read.taints[1].protected)
        assertEquals(mapOf("example.com/pool" to "blue", "kubernetes.io/hostname" to "worker-1", "topology.kubernetes.io/zone" to "zone-a"), read.labelMap)
        assertTrue(read.labelProtected("topology.kubernetes.io/zone"))
        assertFalse(read.labelProtected("example.com/pool"))
        // A key added locally under a well-known prefix is locked too.
        assertTrue(read.labelProtected("node-role.kubernetes.io/worker"))
        assertTrue(read.taintProtected(UNSCHEDULABLE_TAINT))
    }

    @Test fun readToleratesGoNulls() {
        val empty = TalosJson.decodeFromString(KubeNodeMeta.serializer(), """{"node":"n","taints":null,"labels":null}""")
        assertTrue(empty.taints.isEmpty())
        assertTrue(empty.labels.isEmpty())
    }

    @Test fun protectedPrefixes() {
        assertTrue(protectedNodeKey("kubernetes.io/arch"))
        assertTrue(protectedNodeKey("karpenter.sh/nodepool"))
        assertTrue(protectedNodeKey("talos.dev/owned"))
        assertFalse(protectedNodeKey("example.com/kubernetes.io"))
        assertFalse(protectedNodeKey("workload"))
    }

    @Test fun editsCarryLabelDeltasAndTheWholeTaintList() {
        val taints = read.taints.map { it.edit }.filter { it.key != "maintenance" } + NodeTaintEdit("dedicated", "gpu", "NoExecute")
        val labels = read.labelMap - "topology.kubernetes.io/zone" + ("example.com/pool" to "green") + ("workload" to "batch")
        val edits = NodeMetaEdits.between(read, taints, labels)
        val json = parse(edits.toJson())

        assertEquals("4242", json["resourceVersion"]!!.jsonPrimitive.content)
        assertEquals("false", json["force"]!!.jsonPrimitive.content)
        // "Show protected" reaches the plan through the edits, not only the apply's argument.
        assertEquals("true", parse(NodeMetaEdits.between(read, taints, labels, force = true).toJson())["force"]!!.jsonPrimitive.content)
        assertEquals(mapOf("example.com/pool" to "green", "workload" to "batch"), json["setLabels"]!!.jsonObject.mapValues { it.value.jsonPrimitive.content })
        assertEquals(listOf("topology.kubernetes.io/zone"), json["removeLabels"]!!.jsonArray.map { it.jsonPrimitive.content })
        // The cordon's taint is never sent: the core keeps the server's copy.
        val sent = json["taints"]!!.jsonArray.map { it.jsonObject }
        assertEquals(listOf("dedicated"), sent.map { it["key"]!!.jsonPrimitive.content })
        assertEquals("gpu", sent.single()["value"]!!.jsonPrimitive.content)
        assertEquals("NoExecute", sent.single()["effect"]!!.jsonPrimitive.content)
        assertTrue(edits.hasTaintChanges(read))
    }

    @Test fun labelsOnlyEditsKeepTheTaintsAndWriteEveryField() {
        val edits = NodeMetaEdits.between(read, read.taints.map { it.edit }, read.labelMap + ("workload" to "batch"))
        assertFalse(edits.hasTaintChanges(read))
        assertFalse(edits.isEmpty(read))
        val json = parse(edits.toJson())
        assertEquals(listOf("maintenance"), json["taints"]!!.jsonArray.map { it.jsonObject["key"]!!.jsonPrimitive.content })
        assertTrue(json["removeLabels"]!!.jsonArray.isEmpty())
        assertTrue(NodeMetaEdits.between(read, read.taints.map { it.edit }.reversed(), read.labelMap).isEmpty(read))
    }

    @Test fun removingTheLastTaintSendsAnEmptyList() {
        val edits = NodeMetaEdits.between(read, emptyList(), read.labelMap)
        assertTrue(edits.hasTaintChanges(read))
        assertTrue(parse(edits.toJson())["taints"]!!.jsonArray.isEmpty())
    }

    @Test fun planDecodes() {
        val plan = TalosJson.decodeFromString(
            NodeMetaPlan.serializer(),
            """
            {"node":"worker-1",
             "labels":{"added":[{"key":"workload","value":"batch"}],"removed":[{"key":"topology.kubernetes.io/zone","value":"zone-a"}],
                       "changed":[{"key":"example.com/pool","from":"blue","to":"green"}]},
             "taints":{"added":[{"key":"dedicated","value":"gpu","effect":"NoExecute"}],"removed":[{"key":"maintenance","value":"true","effect":"NoSchedule"}],"changed":null},
             "evicts":[{"namespace":"shop","name":"web-0","owner":"StatefulSet/web"}],
             "blockers":[],"warnings":["1 pods will be evicted at once"],"noChange":false}
            """,
        )
        assertEquals("batch", plan.labels.added.single().value)
        assertEquals("green", plan.labels.changed.single().to)
        assertEquals("dedicated=gpu:NoExecute", plan.taints.added.single().display)
        assertTrue(plan.taints.changed.isEmpty())
        assertEquals("StatefulSet/web", plan.evicts.single().owner)
        assertTrue(plan.hasTaintChanges)
        assertTrue(plan.canApply)
        assertFalse(plan.stale)
    }

    @Test fun planWithBlockersOrNothingCannotApply() {
        val blocked = TalosJson.decodeFromString(
            NodeMetaPlan.serializer(),
            """{"node":"n","labels":{"added":[{"key":"bad key","value":"x"}]},"blockers":["metadata.labels: Invalid value"],"warnings":null}""",
        )
        assertFalse(blocked.canApply)
        assertFalse(blocked.hasTaintChanges)
        val none = TalosJson.decodeFromString(NodeMetaPlan.serializer(), """{"node":"n","noChange":true}""")
        assertFalse(none.canApply)
        val stale = TalosJson.decodeFromString(NodeMetaPlan.serializer(), """{"node":"n","blockers":["the node changed, reload"]}""")
        assertTrue(stale.stale)
    }

    @Test fun taintChangeReadsAValueOrAWholeTaint() {
        val plan = TalosJson.decodeFromString(
            NodeMetaPlan.serializer(),
            """{"taints":{"changed":[
              {"key":"dedicated","effect":"NoSchedule","from":"cpu","to":"gpu"},
              {"key":"spot","from":{"value":"true","effect":"NoSchedule"},"to":{"value":"true","effect":"NoExecute"}}]}}""",
        )
        assertTrue(plan.hasTaintChanges)
        assertEquals("cpu" to "gpu", plan.taints.changed[0].fromText to plan.taints.changed[0].toText)
        assertEquals("true:NoSchedule" to "true:NoExecute", plan.taints.changed[1].fromText to plan.taints.changed[1].toText)
    }
}
