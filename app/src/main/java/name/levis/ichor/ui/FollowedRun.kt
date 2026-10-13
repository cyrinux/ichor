package name.levis.ichor.ui

/** A run the app follows app-wide (an upgrade, a node maintenance): its state while [OperationService] keeps the app alive. */
interface FollowedRun {
    /** The node's name, for the notification. */
    val hostname: String
    val finished: Boolean
    val running: Boolean get() = !finished
    /** Why the run failed; null when it succeeded or still runs. */
    val error: String?
}
