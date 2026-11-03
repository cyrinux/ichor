package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class NetMapTest {
    // Shaped like KubeArgoNetwork's answer for the demo app "demo-worker" (kube_argocd_network_demo.go),
    // a NetMap whose caller draws three layers in front, plus a host and an Ingress so every kind
    // of hop is there.
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

    private val network = TalosJson.decodeFromString(NetMap.serializer(), json)

    private companion object {
        // The Argo map's layout: host, Gateway and route in front.
        const val SERVICE_LAYER = 3
        const val POD_LAYER = 4
        val MORE_PODS = NetMap.moreId(POD_LAYER)
    }

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
        val empty = TalosJson.decodeFromString(NetMap.serializer(), """{"nodes":[],"edges":[],"problem":null}""")
        assertTrue(empty.takesNoTraffic)
        assertTrue(empty.layers.isEmpty())
        assertNull(empty.problem)
        assertTrue(TalosJson.decodeFromString(NetMap.serializer(), "{}").takesNoTraffic)
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
        val pods = (1..11).map { NetMapNode(id = "pod/ns/p$it", layer = POD_LAYER, kind = NetMapNode.POD, name = "p$it") }
        val big = NetMap(nodes = pods)
        val folded = big.column(POD_LAYER, expanded = false)
        assertEquals(8, folded.nodes.size)
        assertEquals(3, folded.hidden)
        assertEquals("p1", folded.nodes.first().name)
        val open = big.column(POD_LAYER, expanded = true)
        assertEquals(11, open.nodes.size)
        assertEquals(0, open.hidden)
        assertEquals(0, network.column(POD_LAYER, expanded = false).hidden)
    }

    @Test
    fun findsThePodsNode() {
        val pod = network.node("pod/demo/worker-6f4b8-pqrst")!!
        assertEquals("demo-worker-2", network.nodeOf(pod)?.name)
        assertNull(network.nodeOf(NetMapNode(id = "pod/demo/pending", kind = NetMapNode.POD)))
    }

    @Test
    fun marksTheNodesTalosReportsDown() {
        val healthy = network.copy(problem = null)
        val joined = healthy.withDownNodes(setOf("demo-worker-2", "elsewhere"))
        assertEquals(ServiceHealth.CRITICAL, joined.node("node/demo-worker-2")?.healthState)
        assertEquals(ServiceHealth.CRITICAL, joined.edges.first { it.to == "node/demo-worker-2" }.healthState)
        assertEquals("NotReady", joined.node("node/demo-worker-2")?.detail)
        assertEquals(NetMapProblem(kind = "Node", name = "demo-worker-2", detail = "NotReady"), joined.problem)
        // A problem the core found stays the one shown.
        assertEquals("demo-worker-3", network.withDownNodes(setOf("demo-worker-2")).problem?.name)
        // Nothing down among its nodes: the same graph.
        assertSame(network, network.withDownNodes(setOf("elsewhere")))
    }

    @Test
    fun aDownNodeOutranksACordonedOneAndAPodWarning() {
        val cordoned = NetMap(
            nodes = listOf(
                NetMapNode(id = "pod/ns/p", layer = 4, kind = "Pod", namespace = "ns", name = "p", detail = "ContainerCreating", health = "warning"),
                NetMapNode(id = "node/w3", layer = 5, kind = "Node", name = "w3", detail = "SchedulingDisabled", health = "warning"),
            ),
            edges = listOf(NetMapEdge("pod/ns/p", "node/w3", "warning")),
            problem = NetMapProblem(kind = "Pod", namespace = "ns", name = "p", detail = "ContainerCreating"),
        )
        val joined = cordoned.withDownNodes(setOf("w3"))
        assertEquals(NetMapProblem(kind = "Node", name = "w3", detail = "NotReady"), joined.problem)
        assertEquals(ArgoNetWording.NODE_NOT_READY, joined.problem?.wording)
        assertEquals(NetMapNode(id = "node/w3", layer = 5, kind = "Node", name = "w3", detail = "NotReady", health = "critical"), joined.node("node/w3"))
        // The core named the cordon: Talos saying the node is down replaces it too.
        val named = cordoned.copy(problem = NetMapProblem(kind = "Node", name = "w3", detail = "SchedulingDisabled"))
        assertEquals("NotReady", named.withDownNodes(setOf("w3")).problem?.detail)
    }

    @Test
    fun anyLongColumnFolds() {
        val services = (1..10).map { NetMapNode(id = "svc/ns/s$it", layer = SERVICE_LAYER, kind = NetMapNode.SERVICE, name = "s$it") }
        val map = NetMap(nodes = services)
        val column = map.column(SERVICE_LAYER, expanded = false)
        assertEquals(8, column.nodes.size)
        assertEquals(2, column.hidden)
        assertTrue(map.folds(SERVICE_LAYER))
        assertFalse(network.folds(POD_LAYER))
        // Each column folds behind its own box.
        assertEquals(setOf("svc/ns/s9", "svc/ns/s10"), map.hiddenIds(expanded = false))
        assertEquals("more/$SERVICE_LAYER", NetMap.moreId(SERVICE_LAYER))
    }

    /** One Service in front of 10 pods on 2 nodes: p9 and p10 fold, one critical, one fine. */
    private val crowded: NetMap = run {
        val pods = (1..10).map {
            NetMapNode(id = "pod/ns/p$it", layer = 4, kind = "Pod", namespace = "ns", name = "p$it", health = if (it == 9) "critical" else "ok")
        }
        val nodes = listOf(
            NetMapNode(id = "svc/ns/s", layer = 3, kind = "Service", namespace = "ns", name = "s", health = "warning"),
        ) + pods + listOf(
            NetMapNode(id = "node/a", layer = 5, kind = "Node", name = "a", health = "ok"),
            NetMapNode(id = "node/b", layer = 5, kind = "Node", name = "b", health = "ok"),
        )
        val edges = pods.flatMap { p ->
            val n = if (p.name == "p10") "node/b" else "node/a"
            listOf(NetMapEdge("svc/ns/s", p.id, p.health), NetMapEdge(p.id, n, "ok"))
        }
        NetMap(nodes = nodes, edges = edges)
    }

    @Test
    fun foldedPodsHopsGoToTheMoreBoxOncePerPairInTheWorstHealth() {
        val hidden = crowded.hiddenIds(expanded = false)
        assertEquals(setOf("pod/ns/p9", "pod/ns/p10"), hidden)
        assertTrue(crowded.hiddenIds(expanded = true).isEmpty())
        val hops = crowded.edgesHiding(hidden)
        val intoMore = hops.filter { it.to == MORE_PODS }
        assertEquals(listOf(NetMapEdge("svc/ns/s", MORE_PODS, "critical")), intoMore)
        assertEquals(
            setOf(NetMapEdge(MORE_PODS, "node/a", "ok"), NetMapEdge(MORE_PODS, "node/b", "ok")),
            hops.filter { it.from == MORE_PODS }.toSet(),
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
        assertTrue(viaNodeB.contains(MORE_PODS))
        assertTrue(viaNodeB.contains(NetMapEdge(MORE_PODS, "node/b")))
        assertTrue(viaNodeB.contains(NetMapEdge("svc/ns/s", MORE_PODS)))
        assertFalse(viaNodeB.contains(NetMapEdge(MORE_PODS, "node/a")))
        // A shown pod's path stays clear of the box.
        val viaP1 = crowded.pathThrough("pod/ns/p1", hidden)
        assertFalse(viaP1.contains(MORE_PODS))
        assertTrue(viaP1.contains(NetMapEdge("svc/ns/s", "pod/ns/p1")))
    }

    @Test
    fun wordsTheProblemByKind() {
        assertEquals(ArgoNetWording.POD_STATUS, NetMapProblem(kind = "Pod").wording)
        assertEquals(ArgoNetWording.NO_READY_PODS, NetMapProblem(kind = "Service").wording)
        assertEquals(ArgoNetWording.NODE_CORDONED, NetMapProblem(kind = "Node", detail = "SchedulingDisabled").wording)
        assertEquals(ArgoNetWording.NO_HEALTHY_BACKEND, NetMapProblem(kind = "Ingress").wording)
        assertEquals(ArgoNetWording.NO_HEALTHY_BACKEND, NetMapProblem(kind = "Gateway").wording)
        assertEquals(ArgoNetWording.NO_HEALTHY_BACKEND, NetMapProblem(kind = "IngressRoute").wording)
    }

    @Test
    fun sharedRoutesAreTheOnesTheAppDoesNotManage() {
        assertTrue(NetMapNode(id = "gw/traefik/homelab", kind = NetMapNode.GATEWAY, managed = false).shared)
        assertTrue(NetMapNode(id = "ir/edge/web", kind = NetMapNode.INGRESS_ROUTE, managed = false).shared)
        assertTrue(NetMapNode(id = "irtcp/edge/web", kind = NetMapNode.INGRESS_ROUTE_TCP, managed = false).shared)
        assertTrue(NetMapNode(id = "gr/edge/api", kind = NetMapNode.GRPC_ROUTE, managed = false).shared)
        assertTrue(NetMapNode(id = "tr/edge/tls", kind = NetMapNode.TLS_ROUTE, managed = false).shared)
        assertFalse(NetMapNode(id = "ir/edge/web", kind = NetMapNode.INGRESS_ROUTE, managed = true).shared)
        assertFalse(network.node("ing/demo/worker")!!.shared)
        assertFalse(network.node("pod/demo/worker-6f4b8-pqrst")!!.shared)
    }
}
