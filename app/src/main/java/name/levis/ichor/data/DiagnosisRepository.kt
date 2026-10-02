package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.talosmobile.Diagnosis
import name.levis.talosmobile.DiagnosisListener
import name.levis.talosmobile.Talosmobile

/** What a model sends back: the whole answer so far, then how it ended. */
sealed interface AnswerEvent {
    data class Text(val text: String) : AnswerEvent

    /** [error] is null when the answer is complete or the user stopped it. */
    data class Done(val error: String?) : AnswerEvent
}

/**
 * The optional AI diagnosis, through the Go core: a report about the cluster, shown to the
 * user, then sent to the model they chose only when they ask.
 */
class DiagnosisRepository(private val configs: ConfigRepository) {

    /** Providers and their default models, the same on Android and iOS. */
    val providers: List<AiProvider> by lazy {
        TalosJson.decodeFromString(ListSerializer(AiProvider.serializer()), Talosmobile.aiProviders())
    }

    /** The models the key can use, newest first; fails with a readable message on a bad key or URL. */
    suspend fun models(provider: String, apiKey: String, baseUrl: String): List<AiModel> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ListSerializer(AiModel.serializer()), Talosmobile.aiModels(provider, apiKey, baseUrl))
    }

    /** Reads the cluster state into a report (os:reader calls only). Nothing leaves the phone here. */
    suspend fun collect(anonymize: Boolean): Diagnosis {
        val stored = configs.config.value ?: throw NoConfigException()
        return withContext(Dispatchers.IO) { Talosmobile.collectDiagnosis(stored.yaml, stored.activeContext, anonymize) }
    }

    /**
     * Sends the report and [note] to the model and streams its answer. Cancelling the
     * collector stops waiting for it. [language] is the app's language tag.
     */
    fun ask(diagnosis: Diagnosis, settings: AiSettings, apiKey: String, language: String, note: String): Flow<AnswerEvent> =
        callbackFlow {
            val run = diagnosis.ask(
                settings.provider,
                apiKey,
                settings.model,
                settings.baseUrl,
                language,
                note,
                object : DiagnosisListener {
                    override fun onAnswer(text: String) {
                        trySend(AnswerEvent.Text(text))
                    }

                    override fun onDone(errMessage: String) {
                        trySend(AnswerEvent.Done(errMessage.ifEmpty { null }))
                        close()
                    }
                },
            )
            awaitClose { run.cancel() }
        }.buffer(Channel.UNLIMITED) // the last text and the final Done are never dropped
}
