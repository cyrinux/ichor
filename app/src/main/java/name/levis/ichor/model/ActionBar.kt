package name.levis.ichor.model

/**
 * One screen's app-bar actions ([entries], in their default order) and how many of them show as
 * icons by default; the rest are in its menu.
 */
class ActionBarKind<A : Enum<A>>(val entries: List<A>, val defaultIcons: Int) {
    val default: ActionBar<A> get() = ActionBar(this, entries, defaultIcons)

    /** Reads [ActionBar.encode]'s form; unknown or repeated names are skipped, missing actions go last in the menu. */
    fun parse(text: String?): ActionBar<A> {
        if (text.isNullOrBlank() || SEPARATOR !in text) return default
        val (iconPart, menuPart) = text.split(SEPARATOR, limit = 2)
        val icons = names(iconPart).distinct()
        val menu = names(menuPart).filter { it !in icons }.distinct()
        val known = icons + menu
        return ActionBar(this, known + entries.filter { it !in known }, icons.size)
    }

    private fun names(part: String): List<A> = part.split(',').mapNotNull { raw -> entries.firstOrNull { it.name == raw.trim() } }

    internal companion object {
        const val SEPARATOR = '|'
    }
}

/**
 * A screen's app-bar actions in the order chosen: the first [iconCount] shown as icons, the rest
 * in its menu, so the title keeps room on a phone. Every action is in [order] exactly once, so
 * one added by a later release shows up (last, in the menu) without touching the saved bar.
 */
data class ActionBar<A : Enum<A>>(val kind: ActionBarKind<A>, val order: List<A>, val iconCount: Int) {
    val icons: List<A> get() = order.take(iconCount)
    val menu: List<A> get() = order.drop(iconCount)
    val isDefault: Boolean get() = this == kind.default

    /** One step towards the start; the menu's first action becomes the last icon. */
    fun up(action: A): ActionBar<A> {
        val index = order.indexOf(action)
        return when {
            index == iconCount -> copy(iconCount = iconCount + 1)
            index <= 0 -> this
            else -> copy(order = order.swap(index, index - 1))
        }
    }

    /** One step towards the end; the last icon becomes the menu's first action. */
    fun down(action: A): ActionBar<A> {
        val index = order.indexOf(action)
        return when {
            index < 0 -> this
            index == iconCount - 1 -> copy(iconCount = iconCount - 1)
            index == order.lastIndex -> this
            else -> copy(order = order.swap(index, index + 1))
        }
    }

    /** Puts an icon at the top of the menu. */
    fun toMenu(action: A): ActionBar<A> {
        if (action !in icons) return this
        val rest = order - action
        return copy(order = rest.take(iconCount - 1) + action + rest.drop(iconCount - 1), iconCount = iconCount - 1)
    }

    /** Shows a menu action as the last icon. */
    fun toBar(action: A): ActionBar<A> {
        if (action !in menu) return this
        val rest = order - action
        return copy(order = rest.take(iconCount) + action + rest.drop(iconCount), iconCount = iconCount + 1)
    }

    /** "HEALTH,EVENTS|METRICS,SETTINGS": the icons, then the menu after the bar. */
    fun encode(): String = icons.joinToString(",") + ActionBarKind.SEPARATOR + menu.joinToString(",")

    private fun List<A>.swap(a: Int, b: Int): List<A> =
        toMutableList().apply { this[a] = this[b].also { this[b] = this[a] } }
}
