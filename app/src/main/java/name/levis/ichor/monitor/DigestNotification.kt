package name.levis.ichor.monitor

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.core.app.NotificationManagerCompat
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.shareLinkFor

/** The alert key a cluster's digest is posted under: each run's digest replaces the previous one. */
const val DIGEST_ALERT_KEY = "digest"

/**
 * One notification for a digest cluster's alerts of a run: "3 findings on staging-eu" and
 * "Critical: 1, warnings: 2", on [post]'s channel, opening that cluster's home like a share
 * link. [cluster]: its name as the app shows it; [clusterId] as [postAlert] takes it.
 */
fun postDigest(context: Context, post: DigestPost, cluster: String, clusterId: String?, hideOnLockScreen: Boolean) {
    if (!canPostNotifications(context)) return
    ensureAlertChannels(context)
    val res = AppLocale.wrap(context)
    val digest = post.digest
    val title = res.resources.getQuantityString(R.plurals.alert_rules_digest_title, digest.count, digest.count, cluster)
    val text = res.getString(R.string.alert_rules_digest_text, digest.critical, digest.warning)
    val fingerprint = post.context.fingerprint
    val id = alertNotificationId(fingerprint, DIGEST_ALERT_KEY)

    val intent = Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
    clusterId?.let { shareLinkFor(ShareTarget.screen(ShareTarget.CLUSTER), it) }?.let { intent.setAction(Intent.ACTION_VIEW).setData(Uri.parse(it)) }
    val open = PendingIntent.getActivity(context, id, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    val builder = alertNotification(context, post.channel, title, text, open, hideOnLockScreen)
    cluster.takeIf { it.isNotBlank() }?.let(builder::setSubText)

    try {
        NotificationManagerCompat.from(context).notify(id, builder.build())
    } catch (_: SecurityException) {
        // Permission revoked between the check and the post; nothing to do.
    }
}
