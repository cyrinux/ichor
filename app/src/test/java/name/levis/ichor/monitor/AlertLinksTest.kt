package name.levis.ichor.monitor

import name.levis.ichor.model.ShareTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AlertLinksTest {
    @Test
    fun nodeAlertsOpenTheNodeByItsAddress() {
        // The detail of a node that went down is its reason, not its address: the key holds that.
        val down = Alert("node:10.0.0.2", AlertKind.NODE_NOT_READY, true, "cp-1", "kubelet stopped")
        assertEquals(ShareTarget(target = ShareTarget.NODE, addr = "10.0.0.2", host = "cp-1"), down.shareTarget())
        assertEquals(AlertChannel.NODES, down.channel)
    }

    @Test
    fun etcdAndCheckupOpenTheirScreens() {
        assertEquals(ShareTarget.screen(ShareTarget.ETCD), Alert("etcd:m1:NOSPACE", AlertKind.ETCD_ALARM, true).shareTarget())
        val finding = Alert("checkup:volumes|pvc|shop/data", AlertKind.CHECKUP_PROBLEM, true, "shop/data", "volumes|pvc|critical")
        assertEquals(ShareTarget.screen(ShareTarget.CHECKUP), finding.shareTarget())
        assertEquals(AlertChannel.CLUSTER, finding.channel)
    }

    @Test
    fun dataAlertsOpenTheTabOfTheirSystem() {
        val pg = Alert("data:cnpg|shop/pg", AlertKind.DATA_PROBLEM, true, "shop/pg", "cnpg|critical")
        assertEquals(ShareTarget.dataServices("cloudnative-pg"), pg.shareTarget())
        assertEquals(AlertChannel.DATA, pg.channel)
        // A system this version does not know: the screen's first tab.
        assertEquals(ShareTarget.dataServices(), Alert("data:x|y", AlertKind.DATA_OK, false, "y", "newdb|").shareTarget())
    }

    @Test
    fun gitopsAlertsOpenTheirApp() {
        val argo = Alert("gitops:argocd|argocd/guestbook", AlertKind.GITOPS_PROBLEM, true, "argocd/guestbook", "argocd|critical|failed")
        assertEquals(ShareTarget.argoApp("argocd", "guestbook"), argo.shareTarget())
        val flux = Alert("gitops:flux|x", AlertKind.GITOPS_OK, false, "HelmRelease flux-system/podinfo", "flux||")
        assertEquals(ShareTarget.fluxApp("HelmRelease", "flux-system", "podinfo"), flux.shareTarget())
        assertEquals(AlertChannel.GITOPS, flux.channel)
    }

    @Test
    fun credentialsAlerts() {
        // A talosconfig certificate opens the renewal screen instead (no share target for it).
        val cert = Alert("cert", AlertKind.CERT_EXPIRING, true, days = 3)
        assertNull(cert.shareTarget())
        assertEquals(AlertChannel.CERTS, cert.channel)
        // A kubeconfig's credentials: that cluster, where a new kubeconfig is imported.
        val kube = Alert("cert", AlertKind.KUBECONFIG_EXPIRED, true, days = 1)
        assertEquals(ShareTarget.screen(ShareTarget.CLUSTER), kube.shareTarget())
        assertEquals(AlertChannel.CERTS, kube.channel)
    }
}
