package name.levis.ichor.model

import java.time.Instant
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/network_bgp.go and network_bgp_cluster.go.

/**
 * One of the node's own BGP sessions (Talos native BGP, 1.14+). [peer] is the neighbour's
 * address, or "link/<name>" for an unnumbered one; [since] RFC 3339, set only while established.
 */
@Serializable
data class BgpPeer(
    val instance: String = "",
    val peer: String = "",
    val peerAsn: Long = 0,
    val localAsn: Long = 0,
    /** IDLE, CONNECT, ACTIVE, OPEN_SENT, OPEN_CONFIRM, ESTABLISHED or UNKNOWN. */
    val state: String = "",
    val established: Boolean = false,
    val since: String = "",
    val received: Long = 0,
    val advertised: Long = 0,
    /** Talos 1.15+: graceful restart was negotiated. */
    val gracefulRestart: Boolean = false,
    /** Talos 1.15+: the peer is restarting and its routes are kept meanwhile (not down). */
    val peerRestarting: Boolean = false,
)

/** One node of [BgpPeerReport]: its sessions, or [error] when it could not be read. */
@Serializable
data class BgpNodePeers(
    val node: String = "",
    val hostname: String = "",
    val supported: Boolean = false,
    val peers: List<BgpPeer> = emptyList(),
    val error: String = "",
)

/** A session down, as Go judged it: not established, and its peer not restarting. */
@Serializable
data class BgpDown(
    val node: String = "",
    val hostname: String = "",
    val instance: String = "",
    val peer: String = "",
    val state: String = "",
)

/** What the monitor reads of ClusterBGPPeers: every node's sessions and those down. */
@Serializable
data class BgpPeerReport(
    val context: String = "",
    val nodes: List<BgpNodePeers> = emptyList(),
    val down: List<BgpDown> = emptyList(),
)

/** Whether the Network screen shows the BGP section: supported and a session, or a failure. */
val NodeNetwork.showsBgp: Boolean
    get() = bgpSupported && (bgpPeers.isNotEmpty() || errors[NetworkSection.BGP] != null)

/** A session neither up nor kept by graceful restart. */
val BgpPeer.isDown: Boolean get() = !established && !peerRestarting

/** When the session came up, epoch millis; 0 when not up or unknown. */
val BgpPeer.sinceMillis: Long
    get() = if (since.isEmpty()) 0 else runCatching { Instant.parse(since).toEpochMilli() }.getOrDefault(0)
