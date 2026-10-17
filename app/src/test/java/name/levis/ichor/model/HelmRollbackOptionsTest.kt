package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.data.helmRollbackOptionsJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class HelmRollbackOptionsTest {
    @Test
    fun optionsJsonAlwaysCarriesForceAndHistoryMaxButNotWait() {
        assertEquals("""{"force":false,"historyMax":10}""", helmRollbackOptionsJson(HelmRollbackOptions()))
        assertEquals("""{"force":true,"historyMax":2}""", helmRollbackOptionsJson(HelmRollbackOptions(force = true, historyMax = 2, wait = false)))
        assertEquals("""{"force":false,"historyMax":0}""", helmRollbackOptionsJson(HelmRollbackOptions(historyMax = 0)))
        // Never a negative or oversized history-max, which the Go core refuses.
        assertEquals("""{"force":false,"historyMax":0}""", helmRollbackOptionsJson(HelmRollbackOptions(historyMax = -3)))
        assertEquals("""{"force":false,"historyMax":50}""", helmRollbackOptionsJson(HelmRollbackOptions(historyMax = 99)))
    }

    @Test
    fun defaultsAndWhichChangesReloadThePlan() {
        val d = HelmRollbackOptions()
        assertFalse(d.force)
        assertEquals(HELM_HISTORY_MAX_DEFAULT, d.historyMax)
        assertTrue(d.wait)
        assertTrue(d.changesPlan(d.copy(force = true)))
        assertTrue(d.changesPlan(d.copy(historyMax = 3)))
        assertFalse(d.changesPlan(d.copy(wait = false)))
        assertEquals(0, d.withHistoryMax(-1).historyMax)
        assertEquals(HELM_HISTORY_MAX_LIMIT, d.withHistoryMax(51).historyMax)
        assertEquals(7, d.withHistoryMax(7).historyMax)
    }

    @Test
    fun planDecodesTheNewFieldsAndDefaultsWithoutThem() {
        val json = """{"namespace":"web","name":"site","from":2,"to":1,
            "changes":[{"action":"replace","kind":"Deployment","namespace":"web","name":"site"}],
            "force":true,"historyMax":2,"prune":[1],"workloads":3}"""
        val plan = TalosJson.decodeFromString(HelmRollbackPlan.serializer(), json)
        assertTrue(plan.force)
        assertEquals(2, plan.historyMax)
        assertEquals(listOf(1), plan.prune)
        assertEquals(3, plan.workloads)
        assertEquals("replace", plan.changes.single().action)

        val old = TalosJson.decodeFromString(HelmRollbackPlan.serializer(), """{"from":2,"to":1,"prune":null}""")
        assertFalse(old.force)
        assertEquals(0, old.historyMax)
        assertEquals(emptyList<Int>(), old.prune)
        assertEquals(0, old.workloads)
    }

    @Test
    fun followProgressDecodes() {
        val json = """{"phase":"Failed","done":1,"total":2,"at":1700000000000,"workloads":[
            {"kind":"Deployment","namespace":"web","name":"site","ready":2,"desired":2,"done":true},
            {"kind":"StatefulSet","namespace":"web","name":"db","ready":0,"desired":1,"failed":true,"manual":false}]}"""
        val p = TalosJson.decodeFromString(HelmRollbackFollowProgress.serializer(), json)
        assertEquals(HelmRollbackPhase.FAILED, p.phase)
        assertEquals(0.5f, p.fraction, 0.0001f)
        assertEquals(1700000000000, p.at)
        assertEquals("StatefulSet db", p.firstFailed?.label)
        assertTrue(p.workloads[0].done)
        assertFalse(p.workloads[0].manual)

        val empty = TalosJson.decodeFromString(HelmRollbackFollowProgress.serializer(), """{"phase":"Succeeded","workloads":null}""")
        assertEquals(1f, empty.fraction, 0f)
        assertNull(empty.firstFailed)
        assertEquals(emptyList<HelmRollbackWorkload>(), empty.workloads)
    }

    @Test
    fun manualWorkloadDecodes() {
        val w = TalosJson.decodeFromString(
            HelmRollbackWorkload.serializer(),
            """{"kind":"DaemonSet","namespace":"kube-system","name":"agent","ready":1,"desired":3,"done":true,"manual":true}""",
        )
        assertTrue(w.manual)
        assertTrue(w.done)
        assertEquals("DaemonSet agent", w.label)
    }

    @Test
    fun onlyAPruneErrorCountsAsADoneRollback() {
        assertTrue(isHelmHistoryNotPruned("history not pruned: secrets \"sh.helm.release.v1.site.v1\" is forbidden"))
        assertFalse(isHelmHistoryNotPruned("rollback failed: conflict"))
        assertFalse(isHelmHistoryNotPruned(null))
    }

    @Test
    fun planDecodesHooksInOrder() {
        val json = """{"from":3,"to":2,"hooks":[
            {"name":"site-migrate","kind":"Job","event":"pre-rollback","weight":-5,"deletePolicies":["before-hook-creation","hook-succeeded"],"error":""},
            {"name":"site-notify","kind":"Pod","event":"post-rollback","weight":0,"deletePolicies":null}]}"""
        val plan = TalosJson.decodeFromString(HelmRollbackPlan.serializer(), json)
        assertEquals(2, plan.hooks.size)
        val pre = plan.hooks[0]
        assertEquals("site-migrate", pre.name)
        assertEquals("Job", pre.kind)
        assertEquals(-5, pre.weight)
        assertTrue(pre.isPre)
        assertEquals(listOf("before-hook-creation", "hook-succeeded"), pre.deletePolicies)
        val post = plan.hooks[1]
        assertFalse(post.isPre)
        assertEquals(emptyList<String>(), post.deletePolicies)
        assertEquals("", post.error)
        assertTrue(plan.canRun)
    }

    @Test
    fun planWithoutHooksDecodesToNone() {
        assertEquals(emptyList<HelmRollbackHook>(), TalosJson.decodeFromString(HelmRollbackPlan.serializer(), """{"from":2,"to":1}""").hooks)
        assertEquals(emptyList<HelmRollbackHook>(), TalosJson.decodeFromString(HelmRollbackPlan.serializer(), """{"from":2,"to":1,"hooks":null}""").hooks)
    }

    @Test
    fun aRefusedHookBlocksTheRun() {
        val ok = HelmRollbackHook(name = "site-migrate", kind = "Job", event = HELM_HOOK_PRE_ROLLBACK)
        assertTrue(HelmRollbackPlan(hooks = listOf(ok)).canRun)
        val refused = ok.copy(error = "admission webhook denied the request")
        assertFalse(HelmRollbackPlan(hooks = listOf(ok, refused)).canRun)
        assertFalse(HelmRollbackPlan(blockers = listOf("another operation is in progress"), hooks = listOf(ok)).canRun)
    }
}
