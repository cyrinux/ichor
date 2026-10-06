package name.levis.ichor.monitor

/**
 * What a sync of a cluster's freeze reminders has to do: the freezes whose reminder to
 * [cancel], and those to [schedule] with the delay (millis) until theirs. [signature] is
 * what to remember for the next sync.
 */
data class ReminderPlan(val signature: Set<String>, val cancel: Set<String>, val schedule: Map<String, Long>)

/**
 * Plans the reminders of [freezes] (a freeze's key to its end, epoch millis), each due [lead]
 * before the end. [previous] is the signature of the last sync, null when there was none.
 * Null when nothing changed since. A freeze already ended is ignored; one already inside the
 * lead time keeps its pending reminder (it is neither cancelled nor scheduled again); one
 * gone since the last sync (ended early, removed) is cancelled.
 */
fun planFreezeReminders(previous: Set<String>?, freezes: Map<String, Long>, now: Long, lead: Long): ReminderPlan? {
    val running = freezes.filterValues { it > now }
    val signature = running.map { "${it.key}@${it.value}" }.toSet()
    if (signature == previous) return null
    val cancel = previous.orEmpty().map { it.substringBeforeLast('@') }.filter { it !in running.keys }.toSet()
    val schedule = running.filterValues { it - lead > now }.mapValues { it.value - lead - now }
    return ReminderPlan(signature, cancel, schedule)
}
