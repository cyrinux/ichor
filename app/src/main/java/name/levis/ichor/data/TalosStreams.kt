package name.levis.ichor.data

import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.EtcdFixListener
import name.levis.ichorgo.EtcdRecoverListener
import name.levis.ichorgo.EventListener
import name.levis.ichorgo.HealthListener
import name.levis.ichorgo.ImagePullListener
import name.levis.ichorgo.LogListener
import name.levis.ichorgo.SnapshotListener
import name.levis.ichorgo.Ichorgo
import name.levis.ichor.model.TalosEvent
import name.levis.ichor.model.EtcdFixProgress
import name.levis.ichor.model.EtcdRecoverProgress
import name.levis.ichor.model.ImagePullNamespace
import name.levis.ichor.model.ImagePullProgress
import name.levis.ichor.model.SnapshotEncryption
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow

/** Events streamed by the cluster health check. */
sealed interface HealthEvent {
    data class Progress(val node: String, val message: String) : HealthEvent
    data class Done(val error: String?) : HealthEvent
}

/** Events streamed by an etcd snapshot download. */
sealed interface SnapshotEvent {
    data class Progress(val bytes: Long) : SnapshotEvent
    data class Done(val path: String, val size: Long, val sha256: String) : SnapshotEvent
    data class Failed(val message: String) : SnapshotEvent
}

/** The NOSPACE fix's progress, then its end: [Done.error] null when it succeeded. */
/** A recovery's progress, then its end: [Done.error] null when etcd runs again. */
sealed interface EtcdRecoverEvent {
    data class Progress(val progress: EtcdRecoverProgress) : EtcdRecoverEvent
    data class Done(val error: String?) : EtcdRecoverEvent
}

sealed interface EtcdFixEvent {
    data class Progress(val progress: EtcdFixProgress) : EtcdFixEvent
    data class Done(val error: String?) : EtcdFixEvent
}

/** An image pull's per-node progress, then its end: [Done.error] null when every node pulled it. */
sealed interface ImagePullEvent {
    data class Progress(val progress: ImagePullProgress) : ImagePullEvent
    data class Done(val error: String?) : ImagePullEvent
}

/** Items of a live stream (events, followed log). [Done] ends it; [error] null when cancelled. */
sealed interface StreamItem<out T> {
    data class Item<T>(val value: T) : StreamItem<T>
    data class Done(val error: String?) : StreamItem<Nothing>
}

/**
 * The Go core's live streams (events, followed logs, the health check, an etcd snapshot) as
 * flows: cancelling the collector cancels the run. [TalosRepository] exposes them.
 */
internal class TalosStreams(private val configs: ConfigRepository) {
    fun events(nodes: List<String>, tail: Int): Flow<StreamItem<TalosEvent>> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startEvents(
            stored.yaml,
            stored.activeContext,
            nodes.joinToString(","),
            tail.toLong(),
            object : EventListener {
                override fun onEvent(json: String) {
                    runCatching { TalosJson.decodeFromString(TalosEvent.serializer(), json) }
                        .onSuccess { trySend(StreamItem.Item(it)) }
                }

                override fun onDone(errMessage: String) {
                    trySend(StreamItem.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    fun followLogs(node: String, service: String?, tailLines: Int): Flow<StreamItem<String>> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startLogFollow(
            stored.yaml,
            stored.activeContext,
            node,
            service.orEmpty(),
            tailLines.toLong(),
            object : LogListener {
                override fun onLine(line: String) {
                    trySend(StreamItem.Item(line))
                }

                override fun onDone(errMessage: String) {
                    trySend(StreamItem.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    fun followContainerLogs(node: String, containerId: String, tailLines: Int): Flow<StreamItem<String>> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startContainerLogFollow(
            stored.yaml,
            stored.activeContext,
            node,
            containerId,
            tailLines.toLong(),
            object : LogListener {
                override fun onLine(line: String) {
                    trySend(StreamItem.Item(line))
                }

                override fun onDone(errMessage: String) {
                    trySend(StreamItem.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    fun etcdSnapshot(node: String, destPath: String, encryption: SnapshotEncryption): Flow<SnapshotEvent> = callbackFlow {
        val stored = configs.forCall()
        val listener = object : SnapshotListener {
            override fun onProgress(bytes: Long) {
                trySend(SnapshotEvent.Progress(bytes))
            }

            override fun onDone(path: String, size: Long, sha256: String, errMessage: String) {
                trySend(if (errMessage.isEmpty()) SnapshotEvent.Done(path, size, sha256) else SnapshotEvent.Failed(errMessage))
                close()
            }
        }
        val (yaml, context) = stored.yaml to stored.activeContext
        val run = when (encryption) {
            SnapshotEncryption.None -> Ichorgo.startEtcdSnapshot(yaml, context, node, destPath, listener)
            is SnapshotEncryption.Keys -> Ichorgo.startEtcdSnapshotEncrypted(yaml, context, node, destPath, encryption.recipients, "", listener)
            is SnapshotEncryption.Passphrase -> Ichorgo.startEtcdSnapshotEncrypted(yaml, context, node, destPath, "", encryption.passphrase, listener)
        }
        awaitClose { run.cancel() }
    }.buffer(Channel.CONFLATED) // progress may be dropped, the final event is always kept

    /**
     * StartEtcdNospaceFix: snapshot into [destPath] (none when empty) through [snapshotNode],
     * defragment every member, disarm the alarm, read etcd again. Closing the flow cancels it.
     */
    fun etcdNospaceFix(snapshotNode: String, destPath: String, encryption: SnapshotEncryption): Flow<EtcdFixEvent> = callbackFlow {
        val stored = configs.forCall()
        val listener = object : EtcdFixListener {
            override fun onProgress(json: String) {
                runCatching { TalosJson.decodeFromString(EtcdFixProgress.serializer(), json) }.getOrNull()
                    ?.let { trySend(EtcdFixEvent.Progress(it)) }
            }

            override fun onDone(errMessage: String) {
                trySend(EtcdFixEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                close()
            }
        }
        val (recipients, passphrase) = when (encryption) {
            SnapshotEncryption.None -> "" to ""
            is SnapshotEncryption.Keys -> encryption.recipients to ""
            is SnapshotEncryption.Passphrase -> "" to encryption.passphrase
        }
        val run = Ichorgo.startEtcdNospaceFix(stored.yaml, stored.activeContext, snapshotNode, destPath, recipients, passphrase, listener)
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED) // every step is kept: the timeline needs them

    /**
     * StartImagePull: pull [image] into [namespace] on [nodes] (every node when empty), three
     * at a time. Closing the flow cancels the pulls in flight.
     */
    fun imagePull(nodes: List<String>, image: String, namespace: ImagePullNamespace): Flow<ImagePullEvent> = callbackFlow {
        val stored = configs.forCall()
        val listener = object : ImagePullListener {
            override fun onProgress(json: String) {
                runCatching { TalosJson.decodeFromString(ImagePullProgress.serializer(), json) }.getOrNull()
                    ?.let { trySend(ImagePullEvent.Progress(it)) }
            }

            override fun onDone(errMessage: String) {
                trySend(ImagePullEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                close()
            }
        }
        val run = Ichorgo.startImagePull(stored.yaml, stored.activeContext, nodes.joinToString(","), image, namespace.wire, listener)
        awaitClose { run.cancel() }
    }.buffer(Channel.CONFLATED) // each event carries every node's state: the latest is enough

    /**
     * StartEtcdRecover: upload the snapshot at [path] to [node] and bootstrap etcd from it,
     * decrypting with [identity] or [passphrase] on the fly. Closing the flow cancels it
     * (once the bootstrap is requested it cannot be undone).
     */
    fun etcdRecover(node: String, path: String, identity: String, passphrase: String, skipHashCheck: Boolean): Flow<EtcdRecoverEvent> = callbackFlow {
        val stored = configs.forCall()
        val listener = object : EtcdRecoverListener {
            override fun onProgress(json: String) {
                runCatching { TalosJson.decodeFromString(EtcdRecoverProgress.serializer(), json) }.getOrNull()
                    ?.let { trySend(EtcdRecoverEvent.Progress(it)) }
            }

            override fun onDone(errMessage: String) {
                trySend(EtcdRecoverEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                close()
            }
        }
        val run = Ichorgo.startEtcdRecover(stored.yaml, stored.activeContext, node, path, identity, passphrase, skipHashCheck, listener)
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED) // every step is kept: the timeline needs them

    fun health(): Flow<HealthEvent> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startClusterHealth(
            stored.yaml,
            stored.activeContext,
            object : HealthListener {
                override fun onProgress(node: String, message: String) {
                    trySend(HealthEvent.Progress(node, message))
                }

                override fun onDone(errMessage: String) {
                    trySend(HealthEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED) // never drop progress lines or the final Done event
}
