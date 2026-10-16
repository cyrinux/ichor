package name.levis.ichor.ui.operations

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import name.levis.ichor.TalosApp

private const val ACTION_PREFIX = "name.levis.ichor.OPERATION_"
private const val EXTRA_KIND = "name.levis.ichor.OPERATION_KIND"

/** The buttons of a running operation's notification: Stop, Pause / Resume / Abort, Keep / Revert now, Terminate. */
class OperationActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val app = context.applicationContext as TalosApp
        val kind = OperationKind.named(intent.getStringExtra(EXTRA_KIND)) ?: return
        val action = OperationAction.named(intent.action?.removePrefix(ACTION_PREFIX)) ?: return
        // Re-checked here: the lock may have been turned on since the notification was posted.
        if (action.opensAppWhenLocked && app.appLock.enabled.value) return
        app.followedOperations().firstOrNull { it.kind == kind }?.act(action)
    }

    companion object {
        fun intent(context: Context, kind: OperationKind, action: OperationAction): Intent =
            Intent(context, OperationActionReceiver::class.java)
                .setAction(ACTION_PREFIX + action.name)
                .putExtra(EXTRA_KIND, kind.name)
    }
}
