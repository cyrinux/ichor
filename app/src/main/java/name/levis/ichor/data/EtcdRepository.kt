package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.model.EtcdForfeitResult
import name.levis.ichor.model.EtcdMemberPlan
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.SnapshotEncryption
import name.levis.ichor.model.SnapshotInfo
import name.levis.ichor.model.SnapshotRecipient
import name.levis.ichorgo.Ichorgo

/** etcd through the Talos API: status, maintenance, member changes, snapshots and recovery. */
class EtcdRepository(go: GoCall) : GoRepository(go) {

    private val streams = TalosStreams(go.configs)

    suspend fun etcd(): EtcdOverview = go.remember(ETCD) { etcdUncached() }

    private suspend fun etcdUncached(): EtcdOverview = call { cfg, ctx ->
        TalosJson.decodeFromString(EtcdOverview.serializer(), Ichorgo.etcdStatus(cfg, ctx))
    }

    /** `talosctl -n NODE etcd defrag` (os:operator or os:admin); one member at a time. */
    suspend fun etcdDefragment(node: String) = call { cfg, ctx -> Ichorgo.etcdDefragment(cfg, ctx, node) }

    /** `talosctl etcd alarm disarm` through [node] (os:operator or os:admin); alarms are cluster-wide. */
    suspend fun etcdAlarmDisarm(node: String) = call { cfg, ctx -> Ichorgo.etcdAlarmDisarm(cfg, ctx, node) }

    /** Checks the public keys typed for an encrypted snapshot (one per line); throws with the line at fault. */
    suspend fun checkSnapshotRecipients(text: String): List<SnapshotRecipient> = withContext(Dispatchers.Default) {
        TalosJson.decodeFromString(ListSerializer(SnapshotRecipient.serializer()), Ichorgo.checkSnapshotRecipients(text))
    }

    /**
     * Streams `talosctl -n NODE etcd snapshot` into [destPath] (written atomically by Go),
     * age-encrypted while it streams unless [encryption] is [SnapshotEncryption.None].
     * Cancelling the collector cancels the download.
     */
    fun etcdSnapshot(node: String, destPath: String, encryption: SnapshotEncryption): Flow<SnapshotEvent> = streams.etcdSnapshot(node, destPath, encryption)

    /** The one-tap NOSPACE fix (see [TalosStreams.etcdNospaceFix]); needs os:admin. */
    fun etcdNospaceFix(snapshotNode: String, destPath: String, encryption: SnapshotEncryption): Flow<EtcdFixEvent> =
        streams.etcdNospaceFix(snapshotNode, destPath, encryption)

    /** What the snapshot file at [path] needs before a recovery (encrypted, with what). */
    suspend fun snapshotInspect(path: String): SnapshotInfo = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(SnapshotInfo.serializer(), Ichorgo.snapshotInspect(path))
    }

    /** Recovers etcd on [node] from a snapshot (see [TalosStreams.etcdRecover]); needs os:admin. */
    fun etcdRecover(node: String, path: String, identity: String, passphrase: String, skipHashCheck: Boolean): Flow<EtcdRecoverEvent> =
        streams.etcdRecover(node, path, identity, passphrase, skipHashCheck)

    /** `talosctl -n NODE etcd forfeit-leadership` (os:admin); [node] must be the leader. */
    suspend fun etcdForfeitLeadership(node: String): EtcdForfeitResult = call { cfg, ctx ->
        TalosJson.decodeFromString(EtcdForfeitResult.serializer(), Ichorgo.etcdForfeitLeadership(cfg, ctx, node))
    }

    /** `talosctl -n NODE etcd remove-member MEMBER` (os:admin), asked to another member's [node]. */
    suspend fun etcdRemoveMember(node: String, memberId: String) = call { cfg, ctx ->
        Ichorgo.etcdRemoveMember(cfg, ctx, node, memberId)
    }

    /** What removing [memberId] would leave (members, quorum) and what forbids it. Read-only. */
    suspend fun etcdMemberPlan(memberId: String): EtcdMemberPlan = call { cfg, ctx ->
        TalosJson.decodeFromString(EtcdMemberPlan.serializer(), Ichorgo.etcdMemberPlan(cfg, ctx, memberId))
    }

    private suspend fun <T> call(block: (config: String, context: String) -> T): T = go.talos(block)
}
