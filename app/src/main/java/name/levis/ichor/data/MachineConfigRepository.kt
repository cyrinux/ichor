package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.model.ConfigApplyEvent
import name.levis.ichor.model.ConfigApplyMode
import name.levis.ichor.model.ConfigApplyProgress
import name.levis.ichor.model.ConfigEdit
import name.levis.ichor.model.ConfigPreview
import name.levis.ichor.model.ConfigSchemaStatus
import name.levis.ichor.model.ConfigSnippet
import name.levis.ichor.model.ConfigSnippetList
import name.levis.ichor.model.ConfigTree
import name.levis.ichor.model.ConfigTryCommand
import name.levis.ichor.model.ConfigTryEvent
import name.levis.ichor.model.ConfigTryProgress
import name.levis.ichor.model.DriftFix
import name.levis.ichor.model.MultiConfigEvent
import name.levis.ichor.model.MultiConfigPreview
import name.levis.ichor.model.MultiConfigProgress
import name.levis.ichor.model.SnippetEdits
import name.levis.ichor.model.TalosPatchDraft
import name.levis.ichor.model.TalosPatchInfo
import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.ConfigApplyListener
import name.levis.ichorgo.ConfigTryListener
import name.levis.ichorgo.Ichorgo

/**
 * A node's machine config through the Talos API: read, describe and edit it locally, preview a
 * draft with a dry run, then apply it for good, to several nodes, or in try mode.
 */
class MachineConfigRepository(go: GoCall) : GoRepository(go) {

    private val configs = go.configs

    /**
     * The node's active machine config as YAML (os:admin). Secrets are masked unless
     * [revealSecrets]. Never cached: it may hold secrets.
     */
    suspend fun machineConfig(node: String, revealSecrets: Boolean): String = call { cfg, ctx ->
        Ichorgo.nodeMachineConfig(cfg, ctx, node, revealSecrets)
    }

    /**
     * Makes the Talos config schema of [node]'s version ready for [machineConfigDescribe]:
     * downloaded the first time a version is seen, then kept by the core. A schema that
     * cannot be had is not an error ([ConfigSchemaStatus.available] is false).
     */
    suspend fun machineConfigSchema(node: String): ConfigSchemaStatus = call { cfg, ctx ->
        TalosJson.decodeFromString(ConfigSchemaStatus.serializer(), Ichorgo.machineConfigSchemaPrepare(cfg, ctx, node))
    }

    /** [yaml] (a machine config or a draft of one) as a tree, described with the schema of [talosVersion] when the core has it. Local. */
    suspend fun machineConfigDescribe(yaml: String, talosVersion: String): ConfigTree = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ConfigTree.serializer(), Ichorgo.machineConfigDescribe(yaml, talosVersion))
    }

    /** [draft] with [edit] applied. Local: nothing is sent to the node. */
    suspend fun machineConfigEdit(draft: String, edit: ConfigEdit): String = withContext(Dispatchers.IO) {
        Ichorgo.machineConfigEdit(draft, TalosJson.encodeToString(ConfigEdit.serializer(), edit))
    }

    /** The ready-made config changes. Local. */
    suspend fun configSnippets(): List<ConfigSnippet> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ConfigSnippetList.serializer(), Ichorgo.configSnippets()).snippets
    }

    /** The edits of [draft] that snippet [id] makes with [params]. Local. */
    suspend fun configSnippetEdits(draft: String, id: String, params: Map<String, String>): SnippetEdits = withContext(Dispatchers.IO) {
        val json = TalosJson.encodeToString(MapSerializer(String.serializer(), String.serializer()), params)
        TalosJson.decodeFromString(SnippetEdits.serializer(), Ichorgo.configSnippetEdits(draft, id, json))
    }

    /** The edits of [draft] that give it the reference's value of a drift difference. Local. */
    suspend fun driftFixEdits(draft: String, fix: DriftFix): SnippetEdits = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(SnippetEdits.serializer(), Ichorgo.driftFixEdits(draft, fix.key, fix.before, fix.after))
    }

    /** Whether [content] is a Talos patch that can be tried on a node. Local. */
    suspend fun talosPatchDetect(content: String): TalosPatchInfo = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(TalosPatchInfo.serializer(), Ichorgo.talosPatchDetect(content))
    }

    /**
     * [node]'s config with the patch [content] applied (os:admin: read with its secrets, checked
     * with a dry run), redacted: the draft the review shows.
     */
    suspend fun talosPatchDraft(node: String, content: String): TalosPatchDraft = call { cfg, ctx ->
        TalosJson.decodeFromString(TalosPatchDraft.serializer(), Ichorgo.talosPatchDraft(cfg, ctx, node, content))
    }

    /** The field edits of [draft] that make the patch [content], when it only sets values. Local. */
    suspend fun talosPatchEdits(draft: String, content: String): SnippetEdits = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(SnippetEdits.serializer(), Ichorgo.talosPatchEdits(draft, content))
    }

    /**
     * What applying [draft] to [node] would change (os:admin), validated by the node with a dry
     * run. [base] is the redacted config the draft was made from; refused when the node no
     * longer holds it.
     */
    suspend fun machineConfigPreview(node: String, base: String, draft: String): ConfigPreview = call { cfg, ctx ->
        TalosJson.decodeFromString(ConfigPreview.serializer(), Ichorgo.machineConfigPreview(cfg, ctx, node, base, draft))
    }

    /** StartConfigApply: [draft] applied for good in [mode]; closing the flow stops following it. */
    fun applyMachineConfig(node: String, base: String, draft: String, mode: ConfigApplyMode): Flow<ConfigApplyEvent> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startConfigApply(
            stored.yaml,
            stored.activeContext,
            node,
            base,
            draft,
            mode.wire,
            object : ConfigApplyListener {
                override fun onProgress(json: String) {
                    runCatching { TalosJson.decodeFromString(ConfigApplyProgress.serializer(), json) }
                        .onSuccess { trySend(ConfigApplyEvent.Progress(it)) }
                }

                override fun onDone(errMessage: String) {
                    trySend(ConfigApplyEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    /**
     * MachineConfigMultiPreview: [edits] replayed on each of [nodes]' own config, previewed per
     * node with a dry run (os:admin). A node the edits do not fit carries its error.
     */
    suspend fun machineConfigMultiPreview(nodes: List<String>, edits: List<ConfigEdit>): MultiConfigPreview = call { cfg, ctx ->
        TalosJson.decodeFromString(
            MultiConfigPreview.serializer(),
            Ichorgo.machineConfigMultiPreview(cfg, ctx, nodes.joinToString(","), TalosJson.encodeToString(ListSerializer(ConfigEdit.serializer()), edits)),
        )
    }

    /** StartConfigApplyMulti: [edits] applied to [nodes] one after the other in [mode]; closing the flow stops following. */
    fun applyMachineConfigMulti(nodes: List<String>, edits: List<ConfigEdit>, mode: ConfigApplyMode): Flow<MultiConfigEvent> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startConfigApplyMulti(
            stored.yaml,
            stored.activeContext,
            nodes.joinToString(","),
            TalosJson.encodeToString(ListSerializer(ConfigEdit.serializer()), edits),
            mode.wire,
            object : ConfigApplyListener {
                override fun onProgress(json: String) {
                    runCatching { TalosJson.decodeFromString(MultiConfigProgress.serializer(), json) }
                        .onSuccess { trySend(MultiConfigEvent.Progress(it)) }
                }

                override fun onDone(errMessage: String) {
                    trySend(MultiConfigEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    /**
     * Applies [draft] to [node] in Talos's try mode for [timeoutSeconds] and follows it: the
     * node reverts by itself unless [ConfigTryCommand.KEEP] comes through [commands] first.
     * Cancelling the collection only stops following; the node still reverts.
     */
    fun tryMachineConfig(node: String, base: String, draft: String, timeoutSeconds: Int, commands: Flow<ConfigTryCommand>): Flow<ConfigTryEvent> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startConfigTry(
            stored.yaml,
            stored.activeContext,
            node,
            base,
            draft,
            timeoutSeconds.toLong(),
            object : ConfigTryListener {
                override fun onProgress(json: String) {
                    runCatching { TalosJson.decodeFromString(ConfigTryProgress.serializer(), json) }
                        .onSuccess { trySend(ConfigTryEvent.Progress(it)) }
                }

                override fun onDone(outcome: String, errMessage: String) {
                    trySend(ConfigTryEvent.Done(outcome, errMessage))
                    close()
                }
            },
        )
        launch {
            commands.collect {
                when (it) {
                    ConfigTryCommand.KEEP -> run.keep()
                    ConfigTryCommand.REVERT -> run.revert()
                }
            }
        }
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    private suspend fun <T> call(block: (config: String, context: String) -> T): T = go.talos(block)
}
