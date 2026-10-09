package name.levis.ichor.monitor

import name.levis.ichor.model.ShareTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertActionsTest {
    private val down = Alert("node:10.0.0.2", AlertKind.NODE_UNREACHABLE, true, "worker-1", "no route")
    private val argo = Alert("gitops:argocd|apps/shop", AlertKind.GITOPS_PROBLEM, true, "apps/shop", "argocd|critical|failed")
    private val flux = Alert("gitops:flux|HelmRelease infra/ingress", AlertKind.GITOPS_PROBLEM, true, "HelmRelease infra/ingress", "flux|critical|notReady")
    private val firing = Alert("am:abc123", AlertKind.AM_FIRING, true, "HighLatency", "warning|HighLatency|shop")

    @Test
    fun aDownNodeOffersWakeOnlyWithATargetAndRebootOnlyWhenAllowed() {
        assertEquals(listOf(AlertAction.WAKE, AlertAction.REBOOT, AlertAction.SNOOZE), down.actions(canWake = true, canReboot = true))
        assertEquals(listOf(AlertAction.REBOOT, AlertAction.SNOOZE), down.actions(canWake = false, canReboot = true))
        assertEquals(listOf(AlertAction.SNOOZE), down.actions(canWake = false, canReboot = false))
        val notReady = down.copy(kind = AlertKind.NODE_NOT_READY)
        assertEquals(listOf(AlertAction.WAKE, AlertAction.SNOOZE), notReady.actions(canWake = true, canReboot = false))
    }

    @Test
    fun gitOpsAndAlertmanagerAlertsGetTheirAction() {
        assertEquals(listOf(AlertAction.SYNC, AlertAction.SNOOZE), argo.actions(canWake = true, canReboot = true))
        assertEquals(listOf(AlertAction.RECONCILE, AlertAction.SNOOZE), flux.actions(canWake = true, canReboot = true))
        assertEquals(listOf(AlertAction.SILENCE, AlertAction.SNOOZE), firing.actions(canWake = true, canReboot = true))
    }

    @Test
    fun theOtherProblemsOnlySnooze() {
        listOf(
            Alert("etcd:m1:NOSPACE", AlertKind.ETCD_ALARM, true, "NOSPACE", "m1"),
            Alert("data:cnpg|shop/pg", AlertKind.DATA_PROBLEM, true, "shop/pg", "cnpg|critical"),
            Alert("checkup:volumes|pvc|shop/data", AlertKind.CHECKUP_PROBLEM, true, "shop/data", "volumes|pvc|critical"),
            Alert("cert", AlertKind.CERT_EXPIRING, true, days = 3),
            Alert("cert", AlertKind.KUBECONFIG_EXPIRED, true, days = 1),
        ).forEach { assertEquals(it.kind.name, listOf(AlertAction.SNOOZE), it.actions(canWake = true, canReboot = true)) }
    }

    @Test
    fun resolvedAlertsGetNoButtons() {
        listOf(
            Alert("node:10.0.0.2", AlertKind.NODE_READY, false, "worker-1", "10.0.0.2"),
            argo.copy(kind = AlertKind.GITOPS_OK, problem = false),
            firing.copy(kind = AlertKind.AM_RESOLVED, problem = false),
            Alert("data:cnpg|shop/pg", AlertKind.DATA_OK, false, "shop/pg", "cnpg|ok"),
        ).forEach { assertTrue(it.kind.name, it.actions(canWake = true, canReboot = true).isEmpty()) }
    }

    @Test
    fun onlyTheClusterChangingActionsRunInTheApp() {
        assertEquals(setOf(AlertAction.REBOOT, AlertAction.SYNC, AlertAction.RECONCILE, AlertAction.SILENCE), AlertAction.entries.filter { it.inApp }.toSet())
    }

    @Test
    fun aRequestNamesItsTargetAndMatchesTheAlertsOwnLink() {
        val reboot = down.actionRequest(AlertAction.REBOOT)!!
        assertEquals(AlertActionRequest(AlertAction.REBOOT, "10.0.0.2"), reboot)
        assertTrue(reboot.matches(down.shareTarget()!!))
        assertEquals(AlertActionRequest(AlertAction.SYNC, "apps/shop"), argo.actionRequest(AlertAction.SYNC))
        assertTrue(argo.actionRequest(AlertAction.SYNC)!!.matches(argo.shareTarget()!!))
        assertEquals(AlertActionRequest(AlertAction.RECONCILE, "HelmRelease infra/ingress"), flux.actionRequest(AlertAction.RECONCILE))
        assertTrue(flux.actionRequest(AlertAction.RECONCILE)!!.matches(flux.shareTarget()!!))
        assertEquals(AlertActionRequest(AlertAction.SILENCE, "abc123"), firing.actionRequest(AlertAction.SILENCE))
        assertTrue(firing.actionRequest(AlertAction.SILENCE)!!.matches(firing.shareTarget()!!))
        assertNull(down.actionRequest(AlertAction.SNOOZE))
        assertNull(down.actionRequest(AlertAction.WAKE))
    }

    @Test
    fun aRequestForAnotherScreenOrObjectIsIgnored() {
        val reboot = AlertActionRequest(AlertAction.REBOOT, "10.0.0.2")
        assertFalse(reboot.matches(ShareTarget(target = ShareTarget.NODE, addr = "10.0.0.3")))
        assertFalse(reboot.matches(ShareTarget.argoApp("apps", "shop")))
        assertFalse(AlertActionRequest(AlertAction.SYNC, "apps/shop").matches(ShareTarget.argoApp("apps", "cart")))
        assertFalse(AlertActionRequest(AlertAction.RECONCILE, "Kustomization infra/ingress").matches(ShareTarget.fluxApp("HelmRelease", "infra", "ingress")))
        assertFalse(AlertActionRequest(AlertAction.SILENCE, "abc123").matches(ShareTarget.screen(ShareTarget.CHECKUP)))
        assertFalse(AlertActionRequest(AlertAction.SILENCE, "").matches(ShareTarget.screen(ShareTarget.ALERTS)))
    }

    @Test
    fun onlyInAppActionsWithATargetParse() {
        assertEquals(AlertActionRequest(AlertAction.SYNC, "apps/shop"), AlertActionRequest.parse("SYNC", "apps/shop"))
        assertNull(AlertActionRequest.parse("SNOOZE", "node:10.0.0.2"))
        assertNull(AlertActionRequest.parse("WAKE", "10.0.0.2"))
        assertNull(AlertActionRequest.parse("DELETE", "apps/shop"))
        assertNull(AlertActionRequest.parse("REBOOT", ""))
        assertNull(AlertActionRequest.parse(null, "10.0.0.2"))
        assertNull(AlertActionRequest.parse("REBOOT", null))
    }
}
