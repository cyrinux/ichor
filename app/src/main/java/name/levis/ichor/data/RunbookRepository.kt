package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.ReceiveChannel
import kotlinx.coroutines.withContext
import name.levis.ichor.model.RunbookItem
import name.levis.ichor.model.RunbookList
import name.levis.ichor.model.RunbookPlan
import name.levis.ichor.model.RunbookResult
import name.levis.ichor.model.RunbookStepState
import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.Ichorgo
import name.levis.ichorgo.RunbookListener

/** What a running runbook reports. */
sealed interface RunbookEvent {
    data class Step(val state: RunbookStepState) : RunbookEvent
    data class Done(val result: RunbookResult) : RunbookEvent

    /** It did not start (it does not load, the demo). */
    data class Failed(val message: String) : RunbookEvent
}

/** A running runbook: its [events] (closed after Done or Failed), [stop] and [confirm] a waiting step. */
class RunbookHandle(val events: ReceiveChannel<RunbookEvent>, val stop: () -> Unit, val confirm: (index: Int) -> Unit)

/**
 * Runbooks (docs/runbooks.md): each cluster's, kept by the Go core in its sealed data
 * directory under [scope] (see [pinScope]), their plan and their run. Every step of a run is
 * one of the app's own actions, with its permission check, demo refusal and Activity entry.
 */
class RunbookRepository(go: GoCall) : GoRepository(go) {
    suspend fun list(scope: String): List<RunbookItem> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(RunbookList.serializer(), Ichorgo.runbookStoreList(scope)).items
    }

    /** Keeps [yaml] for [scope], after its plan was shown; the same name again replaces it. */
    suspend fun import(scope: String, yaml: String): RunbookItem = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(RunbookItem.serializer(), Ichorgo.runbookStoreImport(scope, yaml))
    }

    suspend fun yaml(scope: String, id: String): String = withContext(Dispatchers.IO) { Ichorgo.runbookStoreGet(scope, id) }

    suspend fun remove(scope: String, id: String) = withContext(Dispatchers.IO) { Ichorgo.runbookStoreRemove(scope, id) }

    /** An https download for the import sheet (at most 1 MiB): the file as it is. */
    suspend fun download(url: String): String = withContext(Dispatchers.IO) { Ichorgo.pluginFetchURL(url) }

    /** What each step would do on the cluster on screen; nothing runs. */
    suspend fun plan(yaml: String): RunbookPlan = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(RunbookPlan.serializer(), Ichorgo.runbookPlan(cfg, ctx, server, yaml))
    }

    /** Starts [yaml] on the cluster on screen, going past the steps [continueIds] names when they fail. */
    fun start(yaml: String, continueIds: Set<String>): RunbookHandle {
        val target = go.kubeTarget()
        val events = Channel<RunbookEvent>(Channel.UNLIMITED) // never drop the final event
        val run = Ichorgo.startRunbook(
            target.yaml,
            target.context,
            target.server,
            yaml,
            continueIds.joinToString(","),
            object : RunbookListener {
                override fun onStep(index: Long, stateJSON: String) {
                    runCatching { TalosJson.decodeFromString(RunbookStepState.serializer(), stateJSON) }
                        .onSuccess { events.trySend(RunbookEvent.Step(it)) }
                }

                override fun onDone(resultJSON: String) {
                    val result = runCatching { TalosJson.decodeFromString(RunbookResult.serializer(), resultJSON) }.getOrElse { RunbookResult() }
                    events.trySend(RunbookEvent.Done(result))
                    events.close()
                }

                override fun onError(message: String) {
                    events.trySend(RunbookEvent.Failed(goErrorText(message)))
                    events.close()
                }
            },
        )
        return RunbookHandle(events, run::stop) { run.confirm(it.toLong()) }
    }
}
