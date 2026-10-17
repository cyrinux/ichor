package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ArgoRolloutsTest {
    // A KubeArgoRollouts answer (go/ichorgo/kube_argo_rollouts.go), cut down, with a field from the future.
    private val json = """
        {"installed":true,"rollouts":[
          {"namespace":"web","name":"checkout","strategy":"canary","phase":"Degraded","message":"RolloutAborted","paused":false,
           "pauseReasons":[],"step":0,"steps":[{"kind":"setWeight","label":"20 %"},{"kind":"pause","label":"∞"}],"weight":0,
           "aborted":true,"stableHash":"5d8f7b9c4","currentHash":"7c9d6f8b5","replicas":3,"updated":0,"ready":3,"available":3,
           "level":"critical"},
          {"namespace":"web","name":"shop","strategy":"canary","phase":"Paused","message":"CanaryPauseStep","paused":true,
           "pauseReasons":["CanaryPauseStep"],"pausedSince":1760000000000,"step":2,
           "steps":[{"kind":"setWeight","label":"20 %"},{"kind":"pause","label":"1h"},{"kind":"setWeight","label":"50 %"},
             {"kind":"analysis","label":"success-rate"},{"kind":"setWeight","label":"100 %"}],
           "weight":20,"aborted":false,"promoteFull":false,"stableHash":"6f8b9c7d5","currentHash":"8a7c6d5e4",
           "replicas":5,"updated":1,"ready":5,"available":5,
           "analysis":{"name":"shop-8a7c6d5e4-2","status":"Running","message":""},
           "autoPromotion":false,"workloadRef":"Deployment/shop","images":["registry.example/shop:2.1"],
           "level":"warning","updatedAt":1760000100000,"future":1},
          {"namespace":"api","name":"api","strategy":"blueGreen","phase":"Paused","paused":true,
           "pauseReasons":["BlueGreenPause"],"step":-1,"steps":[],"stableHash":"1a2b3c","currentHash":"4d5e6f","level":"warning"},
          {"namespace":"api","name":"rollouts-demo","strategy":"canary","phase":"Healthy","step":3,
           "steps":[{"kind":"setWeight","label":"50 %"},{"kind":"pause","label":"30s"},{"kind":"setWeight","label":"100 %"}],
           "weight":100,"stableHash":"9f8e7d","currentHash":"9f8e7d","level":"ok"},
          {"namespace":"api","name":"bare"}]}
    """.trimIndent()

    private val status = TalosJson.decodeFromString(ArgoRolloutsStatus.serializer(), json)
    private fun rollout(name: String) = status.rollouts.first { it.name == name }

    @Test
    fun decodesTheReport() {
        assertTrue(status.installed)
        assertEquals(5, status.rollouts.size)
        val shop = rollout("shop")
        assertEquals("web/shop", shop.key)
        assertEquals(5, shop.steps.size)
        assertEquals("analysis", shop.steps[3].kind)
        assertEquals(20, shop.weight)
        assertEquals(1_760_000_000_000L, shop.pausedSince)
        assertEquals("Running", shop.analysis?.status)
        assertEquals("Deployment/shop", shop.workloadRef)
        assertEquals(ServiceHealth.WARNING, shop.serviceHealth)
        assertEquals(ServiceHealth.CRITICAL, rollout("checkout").serviceHealth)
        assertTrue(rollout("api").isBlueGreen)
    }

    @Test
    fun missingFieldsTakeTheirDefaults() {
        val bare = rollout("bare")
        assertEquals(-1, bare.step)
        assertTrue(bare.steps.isEmpty())
        assertNull(bare.analysis)
        assertEquals(0L, bare.pausedSince)
        assertEquals(ServiceHealth.UNKNOWN, bare.serviceHealth)
        assertFalse(TalosJson.decodeFromString(ArgoRolloutsStatus.serializer(), "{}").installed)
    }

    @Test
    fun aPausedCanaryPromotesPromotesFullyAndAborts() {
        val shop = rollout("shop")
        assertTrue(shop.canPromote)
        assertTrue(shop.canPromoteFull)
        assertTrue(shop.canAbort)
        assertFalse(shop.canRetry)
        assertFalse(shop.canPause)
    }

    @Test
    fun anAbortedRolloutOnlyRetries() {
        val checkout = rollout("checkout")
        assertFalse(checkout.canPromote)
        assertFalse(checkout.canPromoteFull)
        assertFalse(checkout.canAbort)
        assertTrue(checkout.canRetry)
        assertFalse(checkout.canPause)
        assertEquals(listOf(ArgoRolloutAction.RETRY), ArgoRolloutAction.entries.filter(checkout::allows))
    }

    @Test
    fun aBlueGreenWaitingForTheSwitchPromotes() {
        val api = rollout("api")
        assertTrue(api.canPromote)
        assertTrue(api.canAbort)
        assertFalse(api.canPause)
        assertTrue(api.awaitsPromotion)
        // Not paused (the preview still scaling, or auto-promotion on its way): nothing to promote yet, pausable.
        val running = api.copy(paused = false)
        assertFalse(running.canPromote)
        assertFalse(running.awaitsPromotion)
        assertTrue(running.canPause)
        // Nothing new: nothing to promote.
        assertFalse(api.copy(paused = false, currentHash = api.stableHash).canPromote)
    }

    @Test
    fun aHealthyRolloutOffersNothing() {
        val demo = rollout("rollouts-demo")
        assertTrue(ArgoRolloutAction.entries.none(demo::allows))
    }

    @Test
    fun aProgressingCanaryPausesAndAborts() {
        val progressing = rollout("shop").copy(phase = "Progressing", paused = false, pauseReasons = emptyList())
        assertFalse(progressing.canPromote)
        assertTrue(progressing.canPause)
        assertTrue(progressing.canAbort)
        assertTrue(progressing.needsPolling)
    }

    @Test
    fun noCurrentHashBlocksTheVersionActions() {
        val unknown = rollout("shop").copy(currentHash = "")
        assertFalse(unknown.canPromoteFull)
        assertFalse(unknown.canAbort)
        assertTrue(unknown.canPromote)
    }

    @Test
    fun progressIsTheShareOfStepsDone() {
        assertEquals(0.4f, rollout("shop").progress, 0.0001f)
        assertEquals(1f, rollout("rollouts-demo").progress, 0.0001f)
        assertEquals(0f, rollout("api").progress, 0.0001f)
        assertEquals(0f, rollout("checkout").copy(step = -1).progress, 0.0001f)
        assertEquals(1f, rollout("checkout").copy(step = 9).progress, 0.0001f)
    }

    @Test
    fun pollsWhileSomethingMoves() {
        // Paused with an analysis running: the analysis may resume it.
        assertTrue(rollout("shop").needsPolling)
        assertFalse(rollout("api").needsPolling)
        assertFalse(rollout("rollouts-demo").needsPolling)
        assertTrue(status.needsPolling)
        assertFalse(status.copy(rollouts = listOf(rollout("rollouts-demo"))).needsPolling)
    }

    @Test
    fun groupsByNamespace() {
        assertEquals(listOf("api", "web"), status.byNamespace.keys.toList())
        assertEquals(listOf("checkout", "shop"), status.byNamespace.getValue("web").map { it.name })
    }

    @Test
    fun wireNamesMatchTheCore() {
        assertEquals(listOf("promote", "promoteFull", "abort", "retry", "pause"), ArgoRolloutAction.entries.map { it.wire })
        assertEquals("argoRollout", KubeAction.ARGO_ROLLOUT.wire)
    }
}
