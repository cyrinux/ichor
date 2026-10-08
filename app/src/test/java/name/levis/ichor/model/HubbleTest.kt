package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HubbleTest {

    @Test
    fun decodesTheCiliumStatus() {
        val status = TalosJson.decodeFromString(
            CiliumStatus.serializer(),
            """
                {
                 "installed":true,
                 "namespace":"kube-system",
                 "version":"v1.18.2",
                 "hubble":true,
                 "buffer":4095,
                 "agents":[
                 {"node":"demo-cp-1","pod":"cilium-ah2kd","ready":true},
                 {"node":"demo-cp-2","pod":"cilium-bi2kd","ready":true},
                 {"node":"demo-cp-3","pod":"cilium-cj2kd","ready":true},
                 {"node":"demo-worker-1","pod":"cilium-dk2kd","ready":true},
                 {"node":"demo-worker-2","pod":"cilium-el2kd","ready":true}
                 ]
                }
            """,
        )
        assertTrue(status.installed)
        assertTrue(status.hubble)
        assertEquals("v1.18.2", status.version)
        assertEquals(4095, status.buffer)
        assertEquals(5, status.agents.size)
        assertEquals(CiliumStatus(), TalosJson.decodeFromString(CiliumStatus.serializer(), """{"installed":false,"hubble":false,"agents":null}"""))
    }

    @Test
    fun decodesASnapshot() {
        // A snapshot of the demo cluster's stream (go/ichorgo/kube_hubble_demo.go).
        val snapshot = TalosJson.decodeFromString(
            HubbleSnapshot.serializer(),
            """
                {
                 "namespace":"kube-system",
                 "version":"v1.18.2",
                 "buffer":4095,
                 "nodes":[
                 {"node":"demo-cp-1","pod":"cilium-ah2kd","state":"connecting","flows":0},
                 {"node":"demo-cp-2","pod":"cilium-bi2kd","state":"connecting","flows":0},
                 {"node":"demo-cp-3","pod":"cilium-cj2kd","state":"connecting","flows":0},
                 {"node":"demo-worker-1","pod":"cilium-dk2kd","state":"live","flows":70},
                 {"node":"demo-worker-2","pod":"cilium-el2kd","state":"live","flows":50}
                 ],
                 "flows":[
                 {"time":1791116075956,"node":"demo-worker-1","verdict":"FORWARDED","direction":"INGRESS","protocol":"TCP","port":5432,"flags":"SYN","type":"L3_L4","source":{"namespace":"media","pod":"immich-server-7d9f-k2m8p","workload":"immich-server-7d9f","identity":11811,"ip":"10.244.5.82"},"destination":{"namespace":"media","pod":"immich-postgres-1","workload":"immich-postgres","identity":11520,"ip":"10.244.5.61"}},
                 {"time":1791116075090,"node":"demo-worker-1","verdict":"FORWARDED","direction":"INGRESS","protocol":"TCP","port":8080,"flags":"SYN","type":"L7","l7":"HTTP GET /api/sync → 200","source":{"namespace":"networking","pod":"traefik-8c6d-w7r2m","workload":"traefik-8c6d","identity":11294,"ip":"10.244.2.64"},"destination":{"namespace":"default","pod":"vaultwarden-0","workload":"vaultwarden","identity":11158,"ip":"10.244.7.49"}},
                 {"time":1791116074987,"node":"demo-worker-1","verdict":"FORWARDED","direction":"INGRESS","protocol":"TCP","port":8096,"flags":"SYN","type":"L7","l7":"HTTP GET /metrics → 200","source":{"namespace":"monitoring","pod":"prometheus-kube-prometheus-0","workload":"prometheus-kube-prometheus","identity":12652,"ip":"10.244.2.94"},"destination":{"namespace":"media","pod":"jellyfin-5f8c-xq2wz","workload":"jellyfin-5f8c","identity":11326,"ip":"10.244.5.67"}}
                 ],
                 "drops":[
                 {"source":{"namespace":"home","pod":"home-assistant-0","workload":"home-assistant","identity":11410,"ip":"10.244.4.58"},"destination":{"identity":2,"ip":"198.51.100.25","names":["smtp.example.net"],"reserved":"world"},"protocol":"TCP","port":25,"direction":"EGRESS","verdict":"DROPPED","reason":"POLICY_DENY","count":9,"firstSeen":1791116032689,"lastSeen":1791116073859,"nodes":["demo-worker-2"],"deniedBy":[{"kind":"CiliumNetworkPolicy","namespace":"home","name":"home-egress"}],"isolating":[],"sample":{"time":1791116073859,"node":"demo-worker-2","verdict":"DROPPED","reason":"POLICY_DENY","direction":"EGRESS","protocol":"TCP","port":25,"flags":"SYN","type":"L3_L4","source":{"namespace":"home","pod":"home-assistant-0","workload":"home-assistant","identity":11410,"ip":"10.244.4.58"},"destination":{"identity":2,"ip":"198.51.100.25","names":["smtp.example.net"],"reserved":"world"},"deniedBy":[{"kind":"CiliumNetworkPolicy","namespace":"home","name":"home-egress"}]}},
                 {"source":{"namespace":"media","pod":"jellyfin-5f8c-xq2wz","workload":"jellyfin-5f8c","identity":11326,"ip":"10.244.5.67"},"destination":{"namespace":"media","pod":"immich-postgres-1","workload":"immich-postgres","identity":11520,"ip":"10.244.5.61"},"protocol":"TCP","port":5432,"direction":"INGRESS","verdict":"DROPPED","reason":"POLICY_DENIED","count":10,"firstSeen":1791116033921,"lastSeen":1791116068781,"nodes":["demo-worker-1"],"deniedBy":[],"isolating":[{"kind":"NetworkPolicy","namespace":"media","name":"immich-postgres-ingress"},{"kind":"NetworkPolicy","namespace":"media","name":"allow-prometheus-scrape"}],"sample":{"time":1791116068781,"node":"demo-worker-1","verdict":"DROPPED","reason":"POLICY_DENIED","direction":"INGRESS","protocol":"TCP","port":5432,"flags":"SYN","type":"L3_L4","source":{"namespace":"media","pod":"jellyfin-5f8c-xq2wz","workload":"jellyfin-5f8c","identity":11326,"ip":"10.244.5.67"},"destination":{"namespace":"media","pod":"immich-postgres-1","workload":"immich-postgres","identity":11520,"ip":"10.244.5.61"}}},
                 {"source":{"namespace":"home","pod":"zigbee2mqtt-0","workload":"zigbee2mqtt","identity":11119,"ip":"10.244.4.49"},"destination":{"identity":2,"ip":"198.51.100.80","names":["ota.example.org"],"reserved":"world"},"protocol":"TCP","port":443,"direction":"EGRESS","verdict":"DROPPED","reason":"POLICY_DENIED","count":6,"firstSeen":1791116025871,"lastSeen":1791116065830,"nodes":["demo-worker-1"],"deniedBy":[],"isolating":[{"kind":"NetworkPolicy","namespace":"home","name":"default-deny-egress"}],"sample":{"time":1791116065830,"node":"demo-worker-1","verdict":"DROPPED","reason":"POLICY_DENIED","direction":"EGRESS","protocol":"TCP","port":443,"flags":"SYN","type":"L3_L4","source":{"namespace":"home","pod":"zigbee2mqtt-0","workload":"zigbee2mqtt","identity":11119,"ip":"10.244.4.49"},"destination":{"identity":2,"ip":"198.51.100.80","names":["ota.example.org"],"reserved":"world"}}},
                 {"source":{"namespace":"media","pod":"radarr-0","workload":"radarr","identity":10647,"ip":"10.244.5.34"},"destination":{"namespace":"default","pod":"vaultwarden-0","workload":"vaultwarden","identity":11158,"ip":"10.244.7.49"},"protocol":"TCP","port":8080,"direction":"INGRESS","verdict":"AUDIT","reason":"POLICY_DENIED","count":4,"firstSeen":1791116044962,"lastSeen":1791116058981,"nodes":["demo-worker-1"],"deniedBy":[],"isolating":[{"kind":"CiliumNetworkPolicy","namespace":"default","name":"vaultwarden-web"}],"sample":{"time":1791116058981,"node":"demo-worker-1","verdict":"AUDIT","reason":"POLICY_DENIED","direction":"INGRESS","protocol":"TCP","port":8080,"flags":"SYN","type":"L3_L4","source":{"namespace":"media","pod":"radarr-0","workload":"radarr","identity":10647,"ip":"10.244.5.34"},"destination":{"namespace":"default","pod":"vaultwarden-0","workload":"vaultwarden","identity":11158,"ip":"10.244.7.49"}}}
                 ],
                 "seen":120,
                 "dropped":29,
                 "lost":0
                }
            """,
        )
        assertEquals(5, snapshot.nodes.size)
        assertEquals(HUBBLE_NODE_CONNECTING, snapshot.nodes.first().state)
        assertTrue(snapshot.seen >= snapshot.dropped)
        assertEquals(4, snapshot.drops.size)

        val deny = snapshot.drops.first { it.reason == "POLICY_DENY" }
        assertEquals(HUBBLE_EGRESS, deny.direction)
        assertEquals("home/home-assistant-0", deny.source.label)
        assertEquals("world smtp.example.net", deny.destination.label)
        assertEquals(listOf(PolicyRef("CiliumNetworkPolicy", "home", "home-egress")), deny.deniedBy)
        assertEquals("TCP 25", portLabel(deny.protocol, deny.port))
        assertEquals(deny.deniedBy, deny.sample.deniedBy)

        val audit = snapshot.drops.single { it.verdict == HUBBLE_AUDIT }
        assertEquals("default/vaultwarden-web", audit.isolating.single().label)
        assertEquals(setOf("home", "media", "default"), snapshot.drops.flatMap { listOf(it.source.namespace, it.destination.namespace) }.filter { it.isNotEmpty() }.toSet())
        assertTrue(snapshot.namespaces().containsAll(listOf("home", "media", "default")))
        assertEquals(snapshot.drops.size, snapshot.drops.map { it.key }.toSet().size)
    }

    @Test
    fun labelsPeers() {
        assertEquals("media/sonarr-0", HubblePeer(namespace = "media", pod = "sonarr-0", ip = "10.0.0.1").label)
        assertEquals("host", HubblePeer(reserved = "host").label)
        assertEquals("world 198.51.100.7", HubblePeer(reserved = "world", ip = "198.51.100.7").label)
        assertEquals("10.0.0.9", HubblePeer(ip = "10.0.0.9").label)
        assertEquals("?", HubblePeer().label)
        assertEquals("ICMPv4", portLabel("ICMPv4", 0))
    }

    @Test
    fun humanizesDropReasons() {
        assertEquals("Stale or unroutable IP", humanizeReason("STALE_OR_UNROUTABLE_IP"))
        assertEquals("CT map insertion failed", humanizeReason("CT_MAP_INSERTION_FAILED"))
        assertEquals("Unsupported L3 protocol", humanizeReason("UNSUPPORTED_L3_PROTOCOL"))
        assertEquals("", humanizeReason(""))
    }

    // KubeCilium on a Calico cluster with Whisker (go/ichorgo/kube_calico.go).
    @Test
    fun decodesACalicoStatus() {
        val status = TalosJson.decodeFromString(
            CiliumStatus.serializer(),
            """{"installed":true,"cni":"calico","namespace":"calico-system","version":"v3.33.0","hubble":true,
                "agents":[{"node":"minikube","pod":"calico-node-b68qd","ready":true}],
                "whisker":{"namespace":"calico-system","pod":"whisker-58dd87647c-zxzcq","port":8443,"tls":true}}""",
        )
        assertTrue(status.installed)
        assertTrue(status.calico)
        assertTrue(status.hubble)
        assertEquals(0, status.buffer)
        assertEquals("v3.33.0", status.version)

        val snapshot = TalosJson.decodeFromString(
            HubbleSnapshot.serializer(),
            """{"cni":"calico","namespace":"calico-system","version":"v3.33.0","buffer":0,"nodes":[{"node":"whisker","pod":"whisker-1","state":"live","flows":15}],
                "flows":[{"time":1791446895000,"node":"whisker","verdict":"DROPPED","reason":"POLICY_DENIED","direction":"INGRESS","protocol":"TCP","port":80,"type":"L3_L4",
                          "source":{"namespace":"flows","workload":"curl"},"destination":{"namespace":"flows","workload":"nginx"},"isolating":[{"kind":"NetworkPolicy","namespace":"flows","name":"nginx-ingress"}],"packets":6}],
                "drops":[],"seen":15,"dropped":6,"lost":0}""",
        )
        assertEquals(FLOW_CNI_CALICO, snapshot.cni)
        // A Calico peer is an aggregate without a pod: named by its workload.
        assertEquals("flows/curl", snapshot.flows.single().source.label)
    }
}
