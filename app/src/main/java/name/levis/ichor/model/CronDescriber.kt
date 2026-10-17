package name.levis.ichor.model

import java.time.DayOfWeek

/** A CronJob schedule in a shape plain words can say; anything else is read as written. */
sealed interface CronShape {
    val hour: Int
    val minute: Int

    data class Daily(override val hour: Int, override val minute: Int) : CronShape
    data class Weekly(val day: DayOfWeek, override val hour: Int, override val minute: Int) : CronShape
    data class Monthly(val dayOfMonth: Int, override val hour: Int, override val minute: Int) : CronShape
}

private val WEEKDAYS = listOf("sun", "mon", "tue", "wed", "thu", "fri", "sat")

/**
 * The shape of a five-field [schedule] (or @daily, @midnight, @weekly, @monthly): a fixed minute and
 * hour every day, on one weekday or on one day of the month. Null for anything else (steps, ranges,
 * lists, a time zone prefix), which is then read as the raw string.
 */
fun describeCron(schedule: String): CronShape? {
    when (schedule.trim().lowercase()) {
        "@daily", "@midnight" -> return CronShape.Daily(0, 0)
        "@weekly" -> return CronShape.Weekly(DayOfWeek.SUNDAY, 0, 0)
        "@monthly" -> return CronShape.Monthly(1, 0, 0)
    }
    val fields = schedule.trim().split(Regex("\\s+"))
    if (fields.size != 5) return null
    val (minuteField, hourField, domField, monthField, dowField) = fields
    val minute = number(minuteField, 0..59) ?: return null
    val hour = number(hourField, 0..23) ?: return null
    if (monthField != "*") return null
    return when {
        domField == "*" && dowField == "*" -> CronShape.Daily(hour, minute)
        domField == "*" -> weekday(dowField)?.let { CronShape.Weekly(it, hour, minute) }
        dowField == "*" -> number(domField, 1..31)?.let { CronShape.Monthly(it, hour, minute) }
        else -> null
    }
}

private fun number(field: String, range: IntRange): Int? =
    field.takeIf { it.all(Char::isDigit) }?.toIntOrNull()?.takeIf { it in range }

/** 0 and 7 are Sunday; SUN..SAT name them too. */
private fun weekday(field: String): DayOfWeek? {
    val index = number(field, 0..7) ?: WEEKDAYS.indexOf(field.lowercase()).takeIf { it >= 0 } ?: return null
    return if (index % 7 == 0) DayOfWeek.SUNDAY else DayOfWeek.of(index)
}
