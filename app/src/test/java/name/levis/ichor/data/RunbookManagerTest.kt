package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.Channel
import name.levis.ichor.model.RunbookParam
import name.levis.ichor.model.RunbookPlan
import name.levis.ichor.model.RunbookPlanStep
import name.levis.ichor.model.RunbookResult
import name.levis.ichor.model.RunbookStepState
import name.levis.ichor.model.RunbookTarget
import name.levis.ichor.model.confirmsByHostname
import name.levis.ichor.model.initialRunbookValues
import name.levis.ichor.model.missingRunbookValues
import name.levis.ichor.model.runbookValuesLabel
import name.levis.ichor.ui.operations.OperationText
import name.levis.ichor.ui.operations.RunbookPresenter
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RunbookManagerTest {
    private val plan = RunbookPlan(
        name = "Restart ingestion",
        steps = listOf(
            RunbookPlanStep("scale-down", "scale", RunbookTarget("Deployment", "demo-shop", "ingest-api"), destructive = true),
            RunbookPlanStep("snapshot", "etcdSnapshot", RunbookTarget(node = "10.5.0.2")),
        ),
    )

    private class FakeRun {
        val events = Channel<RunbookEvent>(Channel.UNLIMITED)
        val confirmed = mutableListOf<Int>()
        var stops = 0
        var continueIds = emptySet<String>()
        var values = emptyMap<String, String>()

        fun step(index: Int, id: String, state: String) = events.trySend(RunbookEvent.Step(RunbookStepState(index, id, state)))
    }

    private fun manager(fake: FakeRun) = RunbookManager(
        startRun = { _, ids, values ->
            fake.continueIds = ids
            fake.values = values
            RunbookHandle(fake.events, { fake.stops++ }, { fake.confirmed += it })
        },
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined),
    )

    @Test
    fun theValuesReachTheRunAndItsHeader() {
        val fake = FakeRun()
        val m = manager(fake)
        val values = mapOf("ns" to "demo-shop", "count" to "2")
        assertTrue(m.start("yaml", plan, emptySet(), values))
        assertEquals(values, fake.values)
        assertEquals(values, m.current.value?.values)
    }

    @Test
    fun whatTheFormStartsFromAndWhatItStillNeeds() {
        val params = listOf(RunbookParam("ns"), RunbookParam("app", default = "ingest-api"), RunbookParam("old", default = "x"))
        val start = initialRunbookValues(params, mapOf("ns" to "demo-shop", "gone" to "y", "old" to " "))
        assertEquals(mapOf("ns" to "demo-shop", "app" to "ingest-api", "old" to "x"), start)
        assertEquals(emptyList<String>(), missingRunbookValues(params, start))
        assertEquals(listOf("ns"), missingRunbookValues(params, mapOf("ns" to " ")))
        assertEquals("ns=demo-shop · app=api", runbookValuesLabel(mapOf("ns" to "demo-shop", "app" to "api")))
    }

    @Test
    fun aWaitingStepIsConfirmedOnlyWhileItWaits() {
        val fake = FakeRun()
        val m = manager(fake)
        assertTrue(m.start("yaml", plan, setOf("snapshot")))
        assertEquals(setOf("snapshot"), fake.continueIds)
        assertFalse("one run at a time", m.start("yaml", plan, emptySet()))

        m.confirm(0)
        assertTrue("nothing waits yet", fake.confirmed.isEmpty())

        fake.step(0, "scale-down", RunbookStepState.WAITING)
        assertEquals(0, m.current.value?.waiting?.index)
        m.confirm(1)
        m.confirm(0)
        assertEquals(listOf(0), fake.confirmed)

        fake.step(0, "scale-down", RunbookStepState.OK)
        fake.step(1, "snapshot", RunbookStepState.RUNNING)
        assertNull(m.current.value?.waiting)
        assertEquals(1, m.current.value?.current)
    }

    @Test
    fun theRunEndsOkStoppedOrUnstarted() {
        val fake = FakeRun()
        val m = manager(fake)
        m.start("yaml", plan, emptySet())
        fake.events.trySend(RunbookEvent.Done(RunbookResult(ok = true, steps = listOf(RunbookStepState(0, "scale-down", "ok"), RunbookStepState(1, "snapshot", "ok")))))
        val done = requireNotNull(m.current.value)
        assertTrue(done.finished && done.ok)
        assertEquals(2, done.current)
        assertEquals(OperationText.Res(name.levis.ichor.R.string.runbook_notification_done, listOf("Restart ingestion")), RunbookPresenter.result(done).title)

        val stopped = FakeRun()
        val m2 = manager(stopped)
        m2.start("yaml", plan, emptySet())
        m2.stop()
        assertEquals(1, stopped.stops)
        stopped.events.trySend(RunbookEvent.Done(RunbookResult(ok = false)))
        assertEquals(name.levis.ichor.R.string.runbook_notification_stopped, (RunbookPresenter.result(requireNotNull(m2.current.value)).title as OperationText.Res).id)

        val demo = FakeRun()
        val m3 = manager(demo)
        m3.start("yaml", plan, emptySet())
        demo.events.trySend(RunbookEvent.Failed("this action is unavailable in the demo cluster"))
        val refused = requireNotNull(m3.current.value)
        assertTrue(refused.finished)
        assertEquals("this action is unavailable in the demo cluster", refused.error)
        m3.clear()
        assertNull(m3.current.value)
    }

    @Test
    fun aRefusedStepBlocksTheRunUntilLetPast() {
        val denied = plan.copy(steps = plan.steps.mapIndexed { i, s -> if (i == 1) s.copy(allowed = false, denied = "needs the os:operator role") else s })
        assertFalse(denied.canRun(emptySet()))
        assertTrue(denied.canRun(setOf("snapshot")))
        assertTrue(denied.copy(steps = denied.steps.map { it.copy(continueOnError = true) }).canRun(emptySet()))
        assertFalse(RunbookPlan().canRun(emptySet()))
        assertTrue(plan.steps[1].confirmsByHostname())
        assertFalse(plan.steps[0].confirmsByHostname())
    }
}
