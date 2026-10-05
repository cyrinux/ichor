package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoAutoSync
import name.levis.ichor.model.ArgoCondition
import name.levis.ichor.model.ArgoOperation
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.FluxApp
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.NodeHealth
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GitOpsAlertsTest {
    private val now = 1_800_000_000_000L
    private val farCert = now / 1000 + 365L * 86_400

    private val failed = gitopsValue(DATA_CRITICAL, GITOPS_ARGO_FAILED)
    private val outOfSync = gitopsValue(DATA_WARNING, GITOPS_ARGO_OUT_OF_SYNC)
    private val degraded = gitopsValue(DATA_CRITICAL, GITOPS_ARGO_DEGRADED)

    private fun snap(issues: Map<String, String>?, watched: Boolean = true, context: String = "lab") = ClusterSnapshot(
        context = context,
        takenAt = now,
        nodes = mapOf("a" to NodeState("host-a", NodeHealth.READY)),
        etcdChecked = true,
        certNotAfter = farCert,
        gitopsWatched = watched,
        gitopsChecked = watched && issues != null,
        gitopsIssues = issues.orEmpty(),
    )

    @Test
    fun argoIssues() {
        val argo = ArgoStatus(
            installed = true,
            apps = listOf(
                ArgoApp(namespace = "argocd", name = "grafana", health = "Healthy", sync = "Synced", operation = ArgoOperation(phase = "Failed")),
                ArgoApp(namespace = "argocd", name = "errored", health = "Healthy", sync = "Synced", operation = ArgoOperation(phase = "Error")),
                ArgoApp(namespace = "argocd", name = "worker", health = "Degraded", sync = "Synced"),
                ArgoApp(namespace = "argocd", name = "gone", health = "Missing", sync = "OutOfSync"),
                ArgoApp(namespace = "argocd", name = "cert-manager", health = "Healthy", sync = "OutOfSync", autoSync = ArgoAutoSync(enabled = true)),
                ArgoApp(namespace = "argocd", name = "broken", health = "Healthy", sync = "Unknown", conditions = listOf(ArgoCondition("ComparisonError", "bad chart"))),
                // Not issues: the user's choice, work in progress, paused, a past success, a mere warning condition.
                ArgoApp(namespace = "argocd", name = "manual", health = "Healthy", sync = "OutOfSync"),
                ArgoApp(namespace = "argocd", name = "rolling", health = "Progressing", sync = "Synced"),
                ArgoApp(namespace = "argocd", name = "paused", health = "Suspended", sync = "Synced"),
                ArgoApp(namespace = "argocd", name = "fine", health = "Healthy", sync = "Synced", operation = ArgoOperation(phase = "Succeeded")),
                ArgoApp(namespace = "argocd", name = "noted", health = "Healthy", sync = "Synced", conditions = listOf(ArgoCondition("SharedResourceWarning"))),
            ),
        )
        assertEquals(
            mapOf(
                "argocd|argocd/broken" to gitopsValue(DATA_WARNING, GITOPS_ARGO_ERROR),
                "argocd|argocd/cert-manager" to outOfSync,
                "argocd|argocd/errored" to failed,
                "argocd|argocd/gone" to gitopsValue(DATA_CRITICAL, GITOPS_ARGO_MISSING),
                "argocd|argocd/grafana" to failed,
                "argocd|argocd/worker" to degraded,
            ),
            gitopsIssuesOf(argo, null),
        )
    }

    @Test
    fun fluxIssues() {
        val flux = FluxStatus(
            installed = true,
            apps = listOf(
                FluxApp(kind = "HelmRelease", namespace = "ingress", name = "ingress-nginx", level = "critical", ready = "False"),
                FluxApp(kind = "Kustomization", namespace = "flux-system", name = "apps", level = "critical", ready = "False", stalled = true),
                // Suspended apps never alert, nor does Ready=Unknown while reconciling.
                FluxApp(kind = "HelmRelease", namespace = "db", name = "paused", level = "critical", ready = "False", suspended = true),
                FluxApp(kind = "HelmRelease", namespace = "db", name = "upgrading", level = "critical", ready = "Unknown", reconciling = true),
                FluxApp(kind = "Kustomization", namespace = "flux-system", name = "infra", level = "warning", ready = "Unknown"),
                FluxApp(kind = "Kustomization", namespace = "flux-system", name = "ok", level = "ok", ready = "True"),
            ),
        )
        val notReady = gitopsValue(DATA_CRITICAL, GITOPS_FLUX_NOT_READY)
        assertEquals(
            mapOf(
                "flux|HelmRelease ingress/ingress-nginx" to notReady,
                "flux|Kustomization flux-system/apps" to notReady,
            ),
            gitopsIssuesOf(null, flux),
        )
    }

    @Test
    fun notInstalledHasNoIssues() {
        val argo = ArgoStatus(installed = false, apps = listOf(ArgoApp(name = "x", health = "Degraded")))
        val flux = FluxStatus(installed = false, apps = listOf(FluxApp(kind = "HelmRelease", name = "y", level = "critical")))
        assertTrue(gitopsIssuesOf(argo, flux).isEmpty())
    }

    @Test
    fun firstCheckIsASilentBaseline() {
        val result = evaluate(null, snap(mapOf("argocd|argocd/grafana" to failed)), now)
        assertTrue(result.alerts.isEmpty())
        assertEquals(mapOf("argocd|argocd/grafana" to failed), result.next.gitopsIssues)
    }

    @Test
    fun criticalAlertsAtOnce() {
        val result = evaluate(snap(emptyMap()), snap(mapOf("argocd|argocd/grafana" to failed)), now)
        val alert = result.alerts.single()
        assertEquals("gitops:argocd|argocd/grafana", alert.key)
        assertEquals(AlertKind.GITOPS_PROBLEM, alert.kind)
        assertEquals("argocd/grafana", alert.subject)
        assertEquals("argocd|critical|failed", alert.detail)
        assertTrue(alert.problem)
    }

    @Test
    fun warningNeedsTwoChecksInARow() {
        val key = "argocd|argocd/cert-manager"
        val first = evaluate(snap(emptyMap()), snap(mapOf(key to outOfSync)), now)
        assertTrue(first.alerts.isEmpty())
        assertEquals(listOf(key), first.next.gitopsPending)

        val second = evaluate(first.next, snap(mapOf(key to outOfSync)), now)
        assertEquals(listOf("gitops:$key"), second.alerts.map { it.key })
        assertTrue(second.next.gitopsPending.isEmpty())

        assertTrue(evaluate(second.next, snap(mapOf(key to outOfSync)), now).alerts.isEmpty())
    }

    @Test
    fun aShortOutOfSyncNeverAlerts() {
        val key = "argocd|argocd/cert-manager"
        val first = evaluate(snap(emptyMap()), snap(mapOf(key to outOfSync)), now)
        val synced = evaluate(first.next, snap(emptyMap()), now)
        assertTrue(synced.alerts.isEmpty())
        assertTrue(synced.next.gitopsPending.isEmpty())
    }

    @Test
    fun anotherReasonAtTheSameSeverityStaysQuiet() {
        val key = "argocd|argocd/worker"
        val known = snap(emptyMap()).copy(gitopsIssues = mapOf(key to degraded))
        val result = evaluate(known, snap(mapOf(key to failed)), now)
        assertTrue(result.alerts.isEmpty())
        assertEquals(mapOf(key to failed), result.next.gitopsIssues)
    }

    @Test
    fun escalationAlertsAgain() {
        val key = "argocd|argocd/cert-manager"
        val known = snap(emptyMap()).copy(gitopsIssues = mapOf(key to outOfSync))
        val result = evaluate(known, snap(mapOf(key to degraded)), now)
        assertEquals(listOf(AlertKind.GITOPS_PROBLEM), result.alerts.map { it.kind })
    }

    @Test
    fun recoveryIsNotifiedOnce() {
        val key = "flux|HelmRelease ingress/ingress-nginx"
        val known = snap(emptyMap()).copy(gitopsIssues = mapOf(key to gitopsValue(DATA_CRITICAL, GITOPS_FLUX_NOT_READY)))
        val result = evaluate(known, snap(emptyMap()), now)
        val alert = result.alerts.single()
        assertEquals(AlertKind.GITOPS_OK, alert.kind)
        assertEquals("HelmRelease ingress/ingress-nginx", alert.subject)
        assertEquals("flux|critical|notReady", alert.detail)
        assertFalse(alert.problem)
        assertTrue(result.next.gitopsIssues.isEmpty())
        assertTrue(evaluate(result.next, snap(emptyMap()), now).alerts.isEmpty())
    }

    @Test
    fun anUnreadableCheckKeepsWhatWasKnown() {
        val known = snap(emptyMap()).copy(
            gitopsIssues = mapOf("argocd|argocd/grafana" to failed),
            gitopsPending = listOf("argocd|argocd/cert-manager"),
        )
        val result = evaluate(known, snap(issues = null), now)
        assertTrue(result.alerts.isEmpty())
        assertEquals(known.gitopsIssues, result.next.gitopsIssues)
        assertEquals(known.gitopsPending, result.next.gitopsPending)
        assertTrue(result.next.gitopsChecked)
    }

    @Test
    fun turningWatchingOffForgetsAndOnAgainIsABaseline() {
        val known = snap(emptyMap()).copy(gitopsIssues = mapOf("argocd|argocd/grafana" to failed))
        val off = evaluate(known, snap(issues = null, watched = false), now)
        assertTrue(off.alerts.isEmpty())
        assertTrue(off.next.gitopsIssues.isEmpty())

        val on = evaluate(off.next, snap(mapOf("argocd|argocd/worker" to degraded)), now)
        assertTrue(on.alerts.isEmpty())
    }

    @Test
    fun anotherClusterIsABaseline() {
        val result = evaluate(snap(emptyMap(), context = "lab"), snap(mapOf("argocd|argocd/grafana" to failed), context = "prod"), now)
        assertTrue(result.alerts.isEmpty())
    }

    @Test
    fun independentOfDataServices() {
        val prev = snap(emptyMap()).copy(dataWatched = true, dataChecked = true)
        val cur = snap(mapOf("argocd|argocd/grafana" to failed)).copy(dataWatched = true, dataChecked = true, dataIssues = mapOf("cnpg|db/down" to DATA_CRITICAL))
        val result = evaluate(prev, cur, now)
        assertEquals(listOf("gitops:argocd|argocd/grafana", "data:cnpg|db/down").sorted(), result.alerts.map { it.key }.sorted())
    }

    @Test
    fun anOlderSnapshotDecodesWithoutGitOps() {
        val json = """{"context":"lab","takenAt":1,"nodes":{},"dataWatched":true,"dataChecked":true}"""
        val snapshot = TalosJson.decodeFromString(ClusterSnapshot.serializer(), json)
        assertFalse(snapshot.gitopsWatched)
        assertTrue(snapshot.gitopsIssues.isEmpty())
    }

    @Test
    fun snapshotOfReadsGitOpsOnlyWhenWatched() {
        val overview = ClusterOverview(context = "lab", nodes = emptyList())
        val issues = mapOf("argocd|argocd/grafana" to failed)
        assertTrue(snapshotOf(overview, null, 0, now, gitopsWatched = true, gitopsIssues = issues).gitopsChecked)
        assertFalse(snapshotOf(overview, null, 0, now, gitopsWatched = true, gitopsIssues = null).gitopsChecked)
        assertTrue(snapshotOf(overview, null, 0, now, gitopsWatched = false, gitopsIssues = issues).gitopsIssues.isEmpty())
    }

    @Test
    fun anUnreadablePartKeepsItsIssuesAndTheOtherStillAlerts() {
        val known = mapOf(
            "argocd|argocd/web" to gitopsValue(DATA_CRITICAL, GITOPS_ARGO_DEGRADED),
            "flux|HelmRelease db/pg" to gitopsValue(DATA_CRITICAL, GITOPS_FLUX_NOT_READY),
            "flux|Kustomization flux-system/apps" to gitopsValue(DATA_CRITICAL, GITOPS_FLUX_NOT_READY),
        )
        // Argo CD unreadable, Flux read but its HelmReleases not listed, its Kustomization fixed.
        val flux = FluxStatus(installed = true, helmError = "forbidden")
        val issues = gitopsIssuesWithGaps(argo = null, flux = flux, known = known)!!
        assertEquals(setOf("argocd|argocd/web", "flux|HelmRelease db/pg"), issues.keys)
        // Nothing read at all: an unreadable check.
        assertEquals(null, gitopsIssuesWithGaps(null, null, known))
    }

    @Test
    fun knownIssuesOnlyFromTheSameWatchedCluster() {
        val issues = mapOf("argocd|argocd/web" to gitopsValue(DATA_CRITICAL, GITOPS_ARGO_DEGRADED))
        assertEquals(issues, knownGitOpsIssues(snap(issues), "lab"))
        // Another cluster, a check that could not read, or watching off: nothing carries over.
        assertEquals(emptyMap<String, String>(), knownGitOpsIssues(snap(issues, context = "other"), "lab"))
        assertEquals(emptyMap<String, String>(), knownGitOpsIssues(snap(null), "lab"))
        assertEquals(emptyMap<String, String>(), knownGitOpsIssues(snap(issues, watched = false), "lab"))
        assertEquals(emptyMap<String, String>(), knownGitOpsIssues(null, "lab"))
    }
}
