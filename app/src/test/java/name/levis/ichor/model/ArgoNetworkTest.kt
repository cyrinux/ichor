package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class ArgoNetworkTest {
    // Shaped like KubeArgoNetwork's answer for the demo app "demo-worker" (kube_argocd_network_demo.go),
    // plus a host and an Ingress so every kind of hop is there.
    private val json = """
        {"nodes":[
          {"id":"host/https://worker.lan","layer":0,"kind":"Host","name":"worker.lan","detail":"https://worker.lan","health":"ok","url":"https://worker.lan","managed":false},
          {"id":"ing/demo/worker","layer":2,"kind":"Ingress","namespace":"demo","name":"worker","detail":"10.0.0.240","health":"ok","managed":true,"futureField":1},
          {"id":"svc/demo/worker","layer":3,"kind":"Service","namespace":"demo","name":"worker","detail":"ClusterIP 10.96.31.7 · 9090→metrics","health":"warning","managed":true},
          {"id":"pod/demo/worker-6f4b8-uvwxy","layer":4,"kind":"Pod","namespace":"demo","name":"worker-6f4b8-uvwxy","detail":"CrashLoopBackOff · 14 restarts","health":"critical"},
          {"id":"pod/demo/worker-6f4b8-pqrst","layer":4,"kind":"Pod","namespace":"demo","name":"worker-6f4b8-pqrst","detail":"Running","health":"ok"},
          {"id":"node/demo-worker-3","layer":5,"kind":"Node","name":"demo-worker-3","detail":"NotReady","health":"critical"},
          {"id":"node/demo-worker-2","layer":5,"kind":"Node","name":"demo-worker-2","health":"ok"}],
         "edges":[
          {"from":"host/https://worker.lan","to":"ing/demo/worker","health":"ok"},
          {"from":"ing/demo/worker","to":"svc/demo/worker","health":"warning"},
          {"from":"svc/demo/worker","to":"pod/demo/worker-6f4b8-uvwxy","health":"critical"},
          {"from":"svc/demo/worker","to":"pod/demo/worker-6f4b8-pqrst","health":"ok"},
          {"from":"pod/demo/worker-6f4b8-uvwxy","to":"node/demo-worker-3","health":"critical"},
          {"from":"pod/demo/worker-6f4b8-pqrst","to":"node/demo-worker-2","health":"ok"}],
         "problem":{"kind":"Node","namespace":"","name":"demo-worker-3","detail":"NotReady"}}
    """.trimIndent()

    private val network = TalosJson.decodeFromString(ArgoNetwork.serializer(), json)

    @Test
    fun decodesTheGoAnswer() {
        assertEquals(7, network.nodes.size)
        assertEquals(6, network.edges.size)
        assertEquals(ServiceHealth.CRITICAL, network.node("node/demo-worker-3")?.healthState)
        assertEquals(ArgoNetWording.NODE_NOT_READY, network.problem?.wording)
        assertFalse(network.takesNoTraffic)
    }

    @Test
    fun anEmptyGraphTakesNoTraffic() {
        val empty = TalosJson.decodeFromString(ArgoNetwork.serializer(), """{"nodes":[],"edges":[],"problem":null}""")
        assertTrue(empty.takesNoTraffic)
        assertTrue(empty.layers.isEmpty())
        assertNull(empty.problem)
        assertTrue(TalosJson.decodeFromString(ArgoNetwork.serializer(), "{}").takesNoTraffic)
    }

    @Test
    fun skipsEmptyLayers() {
        // No Gateway: its column is not drawn.
        assertEquals(listOf(0, 2, 3, 4, 5), network.layers)
        assertEquals(mapOf(0 to 1, 2 to 1, 3 to 1, 4 to 2, 5 to 2), network.layerCounts)
    }

    @Test
    fun pathThroughAPodGoesUpToTheHostAndDownToItsNodeOnly() {
        val path = network.pathThrough("pod/demo/worker-6f4b8-uvwxy")
        assertEquals(
            setOf("host/https://worker.lan", "ing/demo/worker", "svc/demo/worker", "pod/demo/worker-6f4b8-uvwxy", "node/demo-worker-3"),
            path.nodes,
        )
        // Not sideways into the sibling pod sharing the Service.
        assertFalse(path.contains("pod/demo/worker-6f4b8-pqrst"))
        assertFalse(path.contains("node/demo-worker-2"))
        assertEquals(4, path.edges.size)
    }

    @Test
    fun pathThroughAServiceTakesEveryPodAndNode() {
        val path = network.pathThrough("svc/demo/worker")
        assertEquals(network.nodes.map { it.id }.toSet(), path.nodes)
        assertEquals(network.edges.map { it.key }.toSet(), path.edges)
    }

    @Test
    fun pathThroughAnUnknownBoxIsEmpty() {
        assertTrue(network.pathThrough("svc/nope").nodes.isEmpty())
    }

    @Test
    fun foldsLongColumns() {
        val pods = (1..11).map { ArgoNetNode(id = "pod/ns/p$it", layer = ArgoNetNode.LAYER_POD, kind = ArgoNetNode.POD, name = "p$it") }
        val big = ArgoNetwork(nodes = pods)
        val folded = big.column(ArgoNetNode.LAYER_POD, expanded = false)
        assertEquals(8, folded.nodes.size)
        assertEquals(3, folded.hidden)
        assertEquals("p1", folded.nodes.first().name)
        val open = big.column(ArgoNetNode.LAYER_POD, expanded = true)
        assertEquals(11, open.nodes.size)
        assertEquals(0, open.hidden)
        assertEquals(0, network.column(ArgoNetNode.LAYER_POD, expanded = false).hidden)
    }

    @Test
    fun findsThePodsNode() {
        val pod = network.node("pod/demo/worker-6f4b8-pqrst")!!
        assertEquals("demo-worker-2", network.nodeOf(pod)?.name)
        assertNull(network.nodeOf(ArgoNetNode(id = "pod/demo/pending", kind = ArgoNetNode.POD)))
    }

    @Test
    fun marksTheNodesTalosReportsDown() {
        val healthy = network.copy(problem = null)
        val joined = healthy.withDownNodes(setOf("demo-worker-2", "elsewhere"))
        assertEquals(ServiceHealth.CRITICAL, joined.node("node/demo-worker-2")?.healthState)
        assertEquals(ServiceHealth.CRITICAL, joined.edges.first { it.to == "node/demo-worker-2" }.healthState)
        assertEquals("NotReady", joined.node("node/demo-worker-2")?.detail)
        assertEquals(ArgoNetProblem(kind = "Node", name = "demo-worker-2", detail = "NotReady"), joined.problem)
        // A problem the core found stays the one shown.
        assertEquals("demo-worker-3", network.withDownNodes(setOf("demo-worker-2")).problem?.name)
        // Nothing down among its nodes: the same graph.
        assertSame(network, network.withDownNodes(setOf("elsewhere")))
    }

    @Test
    fun aDownNodeOutranksACordonedOneAndAPodWarning() {
        val cordoned = ArgoNetwork(
            nodes = listOf(
                ArgoNetNode(id = "pod/ns/p", layer = 4, kind = "Pod", namespace = "ns", name = "p", detail = "ContainerCreating", health = "warning"),
                ArgoNetNode(id = "node/w3", layer = 5, kind = "Node", name = "w3", detail = "SchedulingDisabled", health = "warning"),
            ),
            edges = listOf(ArgoNetEdge("pod/ns/p", "node/w3", "warning")),
            problem = ArgoNetProblem(kind = "Pod", namespace = "ns", name = "p", detail = "ContainerCreating"),
        )
        val joined = cordoned.withDownNodes(setOf("w3"))
        assertEquals(ArgoNetProblem(kind = "Node", name = "w3", detail = "NotReady"), joined.problem)
        assertEquals(ArgoNetWording.NODE_NOT_READY, joined.problem?.wording)
        assertEquals(ArgoNetNode(id = "node/w3", layer = 5, kind = "Node", name = "w3", detail = "NotReady", health = "critical"), joined.node("node/w3"))
        // The core named the cordon: Talos saying the node is down replaces it too.
        val named = cordoned.copy(problem = ArgoNetProblem(kind = "Node", name = "w3", detail = "SchedulingDisabled"))
        assertEquals("NotReady", named.withDownNodes(setOf("w3")).problem?.detail)
    }

    @Test
    fun onlyPodsFold() {
        val services = (1..10).map { ArgoNetNode(id = "svc/ns/s$it", layer = ArgoNetNode.LAYER_SERVICE, kind = ArgoNetNode.SERVICE, name = "s$it") }
        val column = ArgoNetwork(nodes = services).column(ArgoNetNode.LAYER_SERVICE, expanded = false)
        assertEquals(10, column.nodes.size)
        assertEquals(0, column.hidden)
    }

    /** One Service in front of 10 pods on 2 nodes: p9 and p10 fold, one critical, one fine. */
    private val crowded: ArgoNetwork = run {
        val pods = (1..10).map {
            ArgoNetNode(id = "pod/ns/p$it", layer = 4, kind = "Pod", namespace = "ns", name = "p$it", health = if (it == 9) "critical" else "ok")
        }
        val nodes = listOf(
            ArgoNetNode(id = "svc/ns/s", layer = 3, kind = "Service", namespace = "ns", name = "s", health = "warning"),
        ) + pods + listOf(
            ArgoNetNode(id = "node/a", layer = 5, kind = "Node", name = "a", health = "ok"),
            ArgoNetNode(id = "node/b", layer = 5, kind = "Node", name = "b", health = "ok"),
        )
        val edges = pods.flatMap { p ->
            val n = if (p.name == "p10") "node/b" else "node/a"
            listOf(ArgoNetEdge("svc/ns/s", p.id, p.health), ArgoNetEdge(p.id, n, "ok"))
        }
        ArgoNetwork(nodes = nodes, edges = edges)
    }

    @Test
    fun foldedPodsHopsGoToTheMoreBoxOncePerPairInTheWorstHealth() {
        val hidden = crowded.hiddenIds(expanded = false)
        assertEquals(setOf("pod/ns/p9", "pod/ns/p10"), hidden)
        assertTrue(crowded.hiddenIds(expanded = true).isEmpty())
        val hops = crowded.edgesHiding(hidden)
        val intoMore = hops.filter { it.to == ArgoNetwork.MORE_PODS }
        assertEquals(listOf(ArgoNetEdge("svc/ns/s", ArgoNetwork.MORE_PODS, "critical")), intoMore)
        assertEquals(
            setOf(ArgoNetEdge(ArgoNetwork.MORE_PODS, "node/a", "ok"), ArgoNetEdge(ArgoNetwork.MORE_PODS, "node/b", "ok")),
            hops.filter { it.from == ArgoNetwork.MORE_PODS }.toSet(),
        )
        assertTrue(hops.none { it.from in hidden || it.to in hidden })
        // 8 shown pods × 2 hops, plus 3 through the box.
        assertEquals(19, hops.size)
        assertSame(crowded.edges, crowded.edgesHiding(emptySet()))
    }

    @Test
    fun aPathThroughFoldedPodsLightsTheMoreBox() {
        val hidden = crowded.hiddenIds(expanded = false)
        val viaNodeB = crowded.pathThrough("node/b", hidden)
        assertTrue(viaNodeB.contains(ArgoNetwork.MORE_PODS))
        assertTrue(viaNodeB.contains(ArgoNetEdge(ArgoNetwork.MORE_PODS, "node/b")))
        assertTrue(viaNodeB.contains(ArgoNetEdge("svc/ns/s", ArgoNetwork.MORE_PODS)))
        assertFalse(viaNodeB.contains(ArgoNetEdge(ArgoNetwork.MORE_PODS, "node/a")))
        // A shown pod's path stays clear of the box.
        val viaP1 = crowded.pathThrough("pod/ns/p1", hidden)
        assertFalse(viaP1.contains(ArgoNetwork.MORE_PODS))
        assertTrue(viaP1.contains(ArgoNetEdge("svc/ns/s", "pod/ns/p1")))
    }

    @Test
    fun wordsTheProblemByKind() {
        assertEquals(ArgoNetWording.POD_STATUS, ArgoNetProblem(kind = "Pod").wording)
        assertEquals(ArgoNetWording.NO_READY_PODS, ArgoNetProblem(kind = "Service").wording)
        assertEquals(ArgoNetWording.NODE_CORDONED, ArgoNetProblem(kind = "Node", detail = "SchedulingDisabled").wording)
        assertEquals(ArgoNetWording.NO_HEALTHY_BACKEND, ArgoNetProblem(kind = "Ingress").wording)
        assertEquals(ArgoNetWording.NO_HEALTHY_BACKEND, ArgoNetProblem(kind = "Gateway").wording)
    }

    @Test
    fun sharedRoutesAreTheOnesTheAppDoesNotManage() {
        assertTrue(ArgoNetNode(id = "gw/traefik/homelab", kind = ArgoNetNode.GATEWAY, managed = false).shared)
        assertFalse(network.node("ing/demo/worker")!!.shared)
        assertFalse(network.node("pod/demo/worker-6f4b8-pqrst")!!.shared)
        assertEquals("demo/worker", network.node("svc/demo/worker")!!.qualifiedName)
        assertEquals("demo-worker-3", network.node("node/demo-worker-3")!!.qualifiedName)
    }
}
