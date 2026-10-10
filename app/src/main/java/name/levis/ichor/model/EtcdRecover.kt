package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/etcdrecover.go (StartEtcdRecover) and snapshotdecrypt.go (SnapshotInspect).

/** What a snapshot file needs before a recovery (Go SnapshotInspect). */
@Serializable
data class SnapshotInfo(
    val encrypted: Boolean = false,
    /** "x25519" (an age secret key), "scrypt" (a passphrase), "unknown", "" in clear. */
    val recipientsHint: String = "",
    val size: Long = 0,
    val sha256: String = "",
) {
    val opener: SnapshotOpener
        get() = when {
            !encrypted -> SnapshotOpener.NONE
            recipientsHint == "x25519" -> SnapshotOpener.SECRET_KEY
            recipientsHint == "scrypt" -> SnapshotOpener.PASSPHRASE
            else -> SnapshotOpener.UNSUPPORTED
        }
}

/** What the user gives to open a snapshot. */
enum class SnapshotOpener {
    /** A clear file. */
    NONE,

    /** An age secret key (AGE-SECRET-KEY-1…). */
    SECRET_KEY,
    PASSPHRASE,

    /** A YubiKey or SSH key: decrypt it on a laptop first. */
    UNSUPPORTED,
}

@Serializable
data class EtcdRecoverProgress(
    val phase: String = "",
    val message: String = "",
    val at: Long = 0,
    /** How much of the file was uploaded, out of [total]. */
    val bytes: Long = 0,
    val total: Long = 0,
)

/** A recovery's steps, in order. */
enum class EtcdRecoverPhase(val wire: String) {
    DECRYPTING("decrypting"),
    UPLOADING("uploading"),
    BOOTSTRAPPING("bootstrapping"),
    WAITING("waiting"),
}

/**
 * Each step's status after [events], like [etcdFixTimeline]; a clear file skips decrypting,
 * so it is left out when [encrypted] is false.
 */
fun etcdRecoverTimeline(
    events: List<EtcdRecoverProgress>,
    finished: Boolean,
    failed: Boolean,
    encrypted: Boolean,
): List<Pair<EtcdRecoverPhase, StepStatus>> {
    val phases = EtcdRecoverPhase.entries.filter { encrypted || it != EtcdRecoverPhase.DECRYPTING }
    val reached = events.mapNotNull { e -> phases.indexOfFirst { it.wire == e.phase }.takeIf { it >= 0 } }.maxOrNull()
        ?: if (failed) 0 else -1
    return phases.mapIndexed { i, phase ->
        phase to when {
            finished && !failed -> StepStatus.DONE
            i < reached -> StepStatus.DONE
            i == reached && failed -> StepStatus.FAILED
            i == reached -> StepStatus.CURRENT
            else -> StepStatus.PENDING
        }
    }
}

/**
 * etcd is lost: no member answered its status. Only then is a recovery offered (Go refuses
 * it otherwise: it would split a live cluster).
 */
val EtcdOverview.etcdLost: Boolean get() = statuses.isNotEmpty() && statuses.none { it.error == null }

/** The control planes a recovery can run on: every node etcd was asked on. */
val EtcdOverview.recoverCandidates: List<String> get() = statuses.map { it.node }
