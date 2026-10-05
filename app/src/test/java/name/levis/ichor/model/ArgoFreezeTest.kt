package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.ZoneId
import java.time.ZonedDateTime

class ArgoFreezeTest {
    // Shaped like KubeArgoCD's answer for the demo cluster (kube_argocd_demo.go), trimmed: the
    // demo namespace frozen by Ichor, an ended freeze, and a nightly window from Git.
    private val json = """
        {"installed":true,"apps":[
          {"namespace":"argocd","name":"demo-worker","project":"apps","destination":{"namespace":"demo"},
           "resources":[{"group":"apps","kind":"Deployment","namespace":"demo","name":"worker","sync":"OutOfSync"},
                        {"kind":"Service","namespace":"demo","name":"worker","sync":"Synced"}],
           "freeze":{"project":"apps","until":5000,"manualSync":true,"byIchor":true,"windows":["a1"]}},
          {"namespace":"argocd","name":"hello-ichor","project":"apps","destination":{"namespace":"demo"},
           "freeze":{"project":"apps","until":5000,"manualSync":true,"byIchor":true,"windows":["a1"]}},
          {"namespace":"argocd","name":"nightly-reports","project":"apps","destination":{"namespace":"reports"}},
          {"namespace":"argocd","name":"cilium","project":"infra","destination":{"namespace":"kube-system"}},
          {"namespace":"argocd","name":"crds","project":"infra","destination":{}}],
         "projects":[
          {"namespace":"argocd","name":"apps","syncWindows":2,"managedBy":"projects","managedServerSide":true,"windows":[
            {"id":"a1","kind":"deny","schedule":"8 14 5 10 *","duration":"61m","timeZone":"UTC","namespaces":["demo"],"manualSync":true,
             "active":true,"start":1000,"end":5000,"apps":2,"ichor":{"reason":"hotfix","createdAt":1000,"expiresAt":5000}},
            {"id":"b2","kind":"deny","schedule":"30 10 5 10 *","duration":"31m","applications":["nightly-reports"],"manualSync":true,
             "active":false,"start":9000,"end":9500,"apps":1,"ichor":{"createdAt":100,"expiresAt":200,"expired":true}}]},
          {"namespace":"argocd","name":"infra","syncWindows":1,"windows":[
            {"id":"c3","kind":"deny","schedule":"0 22 * * *","duration":"8h","applications":["*"],"active":false,"start":7000,"end":8000,"apps":2},
            {"id":"d4","kind":"allow","schedule":"bad","duration":"1h","error":"bad schedule"}]}]}
    """.trimIndent()

    private val status = TalosJson.decodeFromString(ArgoStatus.serializer(), json)
    private fun app(name: String) = status.apps.first { it.name == name }

    @Test
    fun decodesWindowsAndFreezes() {
        val apps = status.projects.first { it.name == "apps" }
        assertEquals("projects", apps.managedBy)
        assertTrue(apps.managedServerSide)
        assertEquals(2, apps.windows.size)
        val freeze = apps.windows[0]
        assertTrue(freeze.isDeny && freeze.active)
        assertEquals("hotfix", freeze.ichor?.reason)
        assertEquals(5000L, freeze.endsAt)
        // An ended Ichor freeze ends at its expiry, not at next year's occurrence.
        assertEquals(200L, apps.windows[1].endsAt)
        assertEquals(5000L, app("demo-worker").freeze?.until)
        assertNull(app("cilium").freeze)
    }

    @Test
    fun freezeTargetsByScope() {
        val worker = app("demo-worker")
        assertEquals(listOf("demo-worker"), status.freezeTargets(worker, FreezeScope.APP).map { it.name })
        assertEquals(listOf("demo-worker", "hello-ichor"), status.freezeTargets(worker, FreezeScope.NAMESPACE).map { it.name })
        assertEquals(listOf("demo-worker", "hello-ichor", "nightly-reports"), status.freezeTargets(worker, FreezeScope.PROJECT).map { it.name })
        assertFalse(FreezeScope.NAMESPACE.availableFor(app("crds")))
        assertTrue(FreezeScope.PROJECT.availableFor(app("crds")))
    }

    @Test
    fun freezeOptionsByScope() {
        val worker = app("demo-worker")
        assertEquals(ArgoFreezeOptions(applications = listOf("demo-worker"), minutes = 60, manualSync = true, reason = "fix"), freezeOptions(worker, FreezeScope.APP, 60, true, " fix "))
        assertEquals(ArgoFreezeOptions(namespaces = listOf("demo"), minutes = 15), freezeOptions(worker, FreezeScope.NAMESPACE, 15, false, ""))
        assertEquals(listOf("*"), freezeOptions(worker, FreezeScope.PROJECT, 15, false, "").applications)
    }

    @Test
    fun windowSectionsAndFreezes() {
        val sections = status.windowSections()
        assertEquals(listOf("a1"), sections.getValue(WindowSection.ACTIVE).map { it.window.id })
        // By next start, the unreadable one last.
        assertEquals(listOf("c3", "d4"), sections.getValue(WindowSection.UPCOMING).map { it.window.id })
        assertEquals(listOf("b2"), sections.getValue(WindowSection.EXPIRED).map { it.window.id })
        assertEquals(listOf("a1"), status.activeFreezes.map { it.window.id })
        assertEquals(listOf("a1"), status.runningIchorFreezes.map { it.window.id })
        assertEquals(listOf("apps"), status.projectsToClear.map { it.name })
        assertEquals(listOf("argocd/apps/a1"), status.freezeWindowsOf(app("demo-worker")).map { it.key })
        assertTrue(status.freezeWindowsOf(app("cilium")).isEmpty())
        assertEquals(listOf("worker"), app("demo-worker").drifted.map { it.name })
        assertTrue(ArgoFilter.FROZEN.matches(app("hello-ichor")))
        assertFalse(ArgoFilter.FROZEN.matches(app("cilium")))
    }

    @Test
    fun minutesUntilMorning() {
        val paris = ZoneId.of("Europe/Paris")
        val at = { s: String -> ZonedDateTime.parse(s).toInstant().toEpochMilli() }
        assertEquals(60, minutesUntil(9, at("2026-10-05T08:00:00+02:00[Europe/Paris]"), paris))
        // Past 09:00 (or within 5 minutes of it): tomorrow's.
        assertEquals(23 * 60, minutesUntil(9, at("2026-10-05T10:00:00+02:00[Europe/Paris]"), paris))
        assertEquals(24 * 60 + 2, minutesUntil(9, at("2026-10-05T08:58:00+02:00[Europe/Paris]"), paris))
        // Rounded up to the minute.
        assertEquals(60, minutesUntil(9, at("2026-10-05T08:00:30+02:00[Europe/Paris]"), paris))
    }

    @Test
    fun selfHealingOwnerOfAWorkload() {
        val worker = ArgoResource(group = "apps", kind = "Deployment", namespace = "demo", name = "worker")
        val healing = ArgoApp(name = "web", project = "apps", autoSync = ArgoAutoSync(enabled = true, selfHeal = true), resources = listOf(worker))
        val status = ArgoStatus(apps = listOf(healing))
        assertEquals("web", status.selfHealingOwner("Deployment", "demo", "worker")?.name)
        assertNull(status.selfHealingOwner("StatefulSet", "demo", "worker"))
        // No self-heal, or frozen already: a scale by hand stays.
        assertNull(ArgoStatus(apps = listOf(healing.copy(autoSync = ArgoAutoSync(enabled = true)))).selfHealingOwner("Deployment", "demo", "worker"))
        assertNull(ArgoStatus(apps = listOf(healing.copy(freeze = ArgoFreeze(until = 1)))).selfHealingOwner("Deployment", "demo", "worker"))
    }
}
