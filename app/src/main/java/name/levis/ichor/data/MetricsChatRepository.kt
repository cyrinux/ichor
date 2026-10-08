package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.model.PanelSuggestion
import name.levis.ichor.model.PromPanel
import name.levis.ichor.model.PromSource
import name.levis.ichor.model.toGoJson
import name.levis.ichor.model.toGoPanelJson
import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.Ichorgo
import name.levis.ichorgo.PromChat
import name.levis.ichorgo.PromChatListener

/** What the panel assistant sends back: the explanation so far, then how the turn ended. */
sealed interface ChatEvent {
    data class Text(val text: String) : ChatEvent

    /** [panel] is the proposed panel, checked by Go, null for a plain answer; [error] null on success or when stopped. */
    data class Done(val panel: PanelSuggestion?, val error: String?) : ChatEvent
}

/**
 * The panel assistant of the Metrics screen, through the Go core: a conversation with the
 * model the user chose, which checks every proposed PromQL query against the cluster's
 * metrics source before showing it. Uses the AI diagnosis settings and keys.
 */
class MetricsChatRepository(private val configs: ConfigRepository, private val kubeServers: KubeServers) {

    /** The metric names [source] knows, for the model to use metrics that exist. */
    suspend fun metricNames(source: PromSource): List<String> = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(ListSerializer(String.serializer()), Ichorgo.promMetricNames(cfg, ctx, server, source.toGoJson()))
    }

    /** A new conversation about [source]; [current] is the panel being edited, null for a new one. */
    suspend fun newChat(source: PromSource, metricNames: List<String>, current: PromPanel?): PromChat = kubeCall { cfg, ctx, server ->
        val names = TalosJson.encodeToString(ListSerializer(String.serializer()), metricNames)
        Ichorgo.newPromChat(cfg, ctx, server, source.toGoJson(), names, current?.takeIf { it.query.isNotBlank() }?.toGoPanelJson().orEmpty())
    }

    /**
     * Sends [message] with the conversation so far and streams the answer; cancelling the
     * collector stops waiting for it. [language] is the app's language tag.
     */
    fun ask(chat: PromChat, settings: AiSettings, apiKey: String, language: String, message: String): Flow<ChatEvent> =
        callbackFlow {
            val run = chat.ask(
                settings.provider,
                apiKey,
                settings.model,
                settings.baseUrl,
                language,
                message,
                object : PromChatListener {
                    override fun onAnswer(text: String) {
                        trySend(ChatEvent.Text(text))
                    }

                    override fun onDone(panelJSON: String, errMessage: String) {
                        val panel = panelJSON.takeIf { it.isNotEmpty() }?.let { TalosJson.decodeFromString(PanelSuggestion.serializer(), it) }
                        trySend(ChatEvent.Done(panel, errMessage.ifEmpty { null }?.let(::goErrorText)))
                        close()
                    }
                },
            )
            awaitClose { run.cancel() }
        }.buffer(Channel.UNLIMITED) // the last text and the final Done are never dropped

    private suspend fun <T> kubeCall(block: (config: String, context: String, kubeServer: String) -> T): T {
        val target = kubeServers.targetFor(configs.forCall())
        return withContext(Dispatchers.IO) { block(target.yaml, target.context, target.server) }
    }
}
