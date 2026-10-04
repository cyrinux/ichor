package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NetPoliciesTest {

    // KubeNetworkPolicies of the demo cluster (go/ichorgo/kube_netpol_demo.go).
    private val report = TalosJson.decodeFromString(
        NetPolicyReport.serializer(),
        """
            {
             "cilium":true,
             "policies":[
             {"kind":"NetworkPolicy","namespace":"media","name":"immich-postgres-ingress","created":1789301677162,"subject":"app=immich-postgres","ingress":true,"egress":false,"ingressRules":[{"peers":[{"kind":"pods","selector":"app=immich-server"},{"kind":"pods","selector":"app=immich-machine-learning"}],"ports":[{"protocol":"TCP","port":"5432"}]}],"egressRules":[],"pods":["media/immich-postgres-1"],"podCount":1},
             {"kind":"NetworkPolicy","namespace":"home","name":"default-deny-egress","created":1789305277162,"subject":"","ingress":false,"egress":true,"ingressRules":[],"egressRules":[],"pods":["home/home-assistant-0","home/zigbee2mqtt-0","home/mosquitto-0"],"podCount":3},
             {"kind":"NetworkPolicy","namespace":"media","name":"allow-prometheus-scrape","created":1789308877162,"subject":"","ingress":true,"egress":false,"ingressRules":[{"peers":[{"kind":"pods","namespaceSelector":"kubernetes.io/metadata.name=monitoring","selector":"app=prometheus-kube-prometheus"}],"ports":[{"protocol":"TCP","port":"metrics"}]}],"egressRules":[],"pods":["media/jellyfin-5f8c-xq2wz","media/immich-server-7d9f-k2m8p","media/immich-machine-learning-6c4-p8x2n","media/immich-postgres-1","media/sonarr-0","media/radarr-0"],"podCount":6},
             {"kind":"CiliumNetworkPolicy","namespace":"home","name":"mosquitto","created":1789312477162,"subject":"app=mosquitto","description":"MQTT only from the home automation apps","ingress":true,"egress":false,"ingressRules":[{"peers":[{"kind":"pods","selector":"app=zigbee2mqtt"},{"kind":"pods","selector":"app=home-assistant"}],"ports":[{"protocol":"TCP","port":"1883"}]}],"egressRules":[],"pods":["home/mosquitto-0"],"podCount":1},
             {"kind":"CiliumNetworkPolicy","namespace":"home","name":"home-egress","created":1789316077162,"subject":"app=home-assistant","ingress":false,"egress":true,"ingressRules":[],"egressRules":[{"peers":[{"kind":"pods","selector":"app=mosquitto"}],"ports":[{"protocol":"TCP","port":"1883"}]},{"peers":[{"kind":"fqdn","value":"*.home-assistant.io"}],"ports":[{"protocol":"TCP","port":"443"}]},{"deny":true,"peers":[{"kind":"entity","value":"world"}],"ports":[{"protocol":"TCP","port":"25"}]}],"pods":["home/home-assistant-0"],"podCount":1},
             {"kind":"CiliumNetworkPolicy","namespace":"default","name":"vaultwarden-web","created":1789319677162,"subject":"app=vaultwarden","ingress":true,"egress":false,"ingressRules":[{"peers":[{"kind":"pods","namespace":"networking","selector":"app=traefik"}],"ports":[{"protocol":"TCP","port":"8080"}],"l7":["HTTP GET","HTTP POST /api/.*"]}],"egressRules":[],"pods":["default/vaultwarden-0"],"podCount":1},
             {"kind":"CiliumClusterwideNetworkPolicy","name":"allow-dns","created":1789323277162,"subject":"","description":"Every pod may resolve names","ingress":false,"egress":false,"ingressRules":[],"egressRules":[{"peers":[{"kind":"pods","namespace":"kube-system","selector":"k8s-app=kube-dns"}],"ports":[{"protocol":"ANY","port":"53"}],"l7":["DNS *"]}],"pods":["cert-manager/cert-manager-7b5d-9kq2w","cert-manager/cert-manager-cainjector-5c9-tt7x2","longhorn-system/longhorn-manager-r6k2p","longhorn-system/longhorn-manager-v3m8d","longhorn-system/longhorn-csi-plugin-b8w4k","monitoring/prometheus-kube-prometheus-0","monitoring/grafana-6d8b-9fz2t","monitoring/loki-0","argocd/argocd-application-controller-0","argocd/argocd-server-6b9d-k2v8n","flux-system/source-controller-7f9c-2xk8p","flux-system/kustomize-controller-5b8d-n4q9z","networking/traefik-8c6d-w7r2m","home/home-assistant-0","home/zigbee2mqtt-0","home/mosquitto-0","media/jellyfin-5f8c-xq2wz","media/immich-server-7d9f-k2m8p","media/immich-machine-learning-6c4-p8x2n","media/immich-postgres-1","media/sonarr-0","media/radarr-0","default/vaultwarden-0","default/paperless-0","default/uptime-kuma-0","demo/hello-ichor","tools/it-backup-5c8d-rx2k9"],"podCount":27},
             {"kind":"CiliumClusterwideNetworkPolicy","name":"control-plane-host","created":1789326877162,"subject":"node-role.kubernetes.io/control-plane=","nodes":true,"ingress":true,"egress":false,"ingressRules":[{"peers":[{"kind":"entity","value":"cluster"}],"ports":[]},{"peers":[{"kind":"entity","value":"world"}],"ports":[{"protocol":"TCP","port":"6443"},{"protocol":"TCP","port":"50000"}]}],"egressRules":[],"pods":[],"podCount":0}
             ],
             "namespaces":[
             {"namespace":"argocd","pods":2,"ingressIsolated":0,"egressIsolated":0,"policies":0},
             {"namespace":"cert-manager","pods":2,"ingressIsolated":0,"egressIsolated":0,"policies":0},
             {"namespace":"default","pods":3,"ingressIsolated":1,"egressIsolated":0,"policies":1},
             {"namespace":"demo","pods":1,"ingressIsolated":0,"egressIsolated":0,"policies":0},
             {"namespace":"flux-system","pods":2,"ingressIsolated":0,"egressIsolated":0,"policies":0},
             {"namespace":"home","pods":3,"ingressIsolated":1,"egressIsolated":3,"policies":3},
             {"namespace":"longhorn-system","pods":3,"ingressIsolated":0,"egressIsolated":0,"policies":0},
             {"namespace":"media","pods":6,"ingressIsolated":6,"egressIsolated":0,"policies":2},
             {"namespace":"monitoring","pods":3,"ingressIsolated":0,"egressIsolated":0,"policies":0},
             {"namespace":"networking","pods":1,"ingressIsolated":0,"egressIsolated":0,"policies":0},
             {"namespace":"tools","pods":1,"ingressIsolated":0,"egressIsolated":0,"policies":0}
             ]
            }
        """,
    )

    @Test
    fun decodesTheGoJson() {
        assertTrue(report.cilium)
        assertEquals(8, report.policies.size)
        val pg = report.policies.first { it.name == "immich-postgres-ingress" }
        assertEquals("NP", pg.kindShort)
        assertEquals("app=immich-postgres", pg.subject)
        assertTrue(pg.ingress)
        assertEquals(listOf("app=immich-server", "app=immich-machine-learning"), pg.ingressRules.single().peers.map { it.selector })
        assertEquals("TCP 5432", pg.ingressRules.single().ports.single().label)

        val egress = report.policies.first { it.name == "home-egress" }
        val deny = egress.egressRules.single { it.deny }
        assertEquals(NETPEER_ENTITY, deny.peers.single().kind)
        assertEquals(NETPEER_FQDN, egress.egressRules[1].peers.single().kind)

        val web = report.policies.first { it.name == "vaultwarden-web" }
        assertEquals(listOf("HTTP GET", "HTTP POST /api/.*"), web.ingressRules.single().l7)

        val dns = report.policies.first { it.name == "allow-dns" }
        assertEquals("CCNP", dns.kindShort)
        assertTrue(dns.clusterWide)
        assertEquals("53", dns.egressRules.single().ports.single().label)

        val media = report.namespaces.first { it.namespace == "media" }
        assertEquals(6, media.pods)
        assertEquals(Isolation.FULL, isolation(media.ingressIsolated, media.pods))
        assertEquals(Isolation.NONE, isolation(media.egressIsolated, media.pods))
        val home = report.namespaces.first { it.namespace == "home" }
        assertEquals(Isolation.PARTIAL, isolation(home.ingressIsolated, home.pods))
    }

    @Test
    fun groupsByNamespaceWithClusterWideLast() {
        val groups = report.grouped(null, "")
        assertEquals(listOf("default", "home", "media", ""), groups.map { it.first })
        assertEquals(listOf("default-deny-egress", "home-egress", "mosquitto"), groups[1].second.map { it.name })
        assertEquals(listOf("home"), report.grouped("home", "").map { it.first })
        assertEquals(listOf("mosquitto"), report.grouped(null, "MQTT").flatMap { g -> g.second.map { it.name } })
        assertTrue(report.grouped(null, "nothing-like-this").isEmpty())
        assertTrue("monitoring" in report.policyNamespaces)
    }

    @Test
    fun findsAPolicyByItsRef() {
        val ref = PolicyRef("CiliumNetworkPolicy", "home", "home-egress")
        assertEquals("home-egress", report.find(ref)?.name)
        assertNull(report.find(ref.copy(kind = "NetworkPolicy")))
        assertEquals("allow-dns", report.find(PolicyRef("CiliumClusterwideNetworkPolicy", "", "allow-dns"))?.name)
    }

    @Test
    fun labelsPorts() {
        assertEquals("TCP 5432", NetPort("TCP", "5432").label)
        assertEquals("UDP 8000–8100", NetPort("UDP", "8000", 8100).label)
        assertEquals("TCP", NetPort("TCP", "").label)
        assertEquals("http", NetPort("ANY", "http").label)
        assertNull(NetPort("ANY", "").label)
    }
}
