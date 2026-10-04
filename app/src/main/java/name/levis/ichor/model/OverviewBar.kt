package name.levis.ichor.model

/** The screens the overview's app bar leads to. */
enum class OverviewAction { HEALTH, EVENTS, WORKLOADS, METRICS, KUBESPAN, ETCD, SETTINGS }

/**
 * The overview's app-bar actions in the order chosen: the first [iconCount] shown as icons, the
 * rest in its menu. Every action is in [order] exactly once, so one added by a later release
 * shows up (last, in the menu) without touching the saved bar.
 */
data class OverviewBar(
    val order: List<OverviewAction> = OverviewAction.entries,
    val iconCount: Int = DEFAULT_ICONS,
) {
    val icons: List<OverviewAction> get() = order.take(iconCount)
    val menu: List<OverviewAction> get() = order.drop(iconCount)
    val isDefault: Boolean get() = this == OverviewBar()

    /** One step towards the start; the menu's first action becomes the last icon. */
    fun up(action: OverviewAction): OverviewBar {
        val index = order.indexOf(action)
        return when {
            index == iconCount -> copy(iconCount = iconCount + 1)
            index <= 0 -> this
            else -> copy(order = order.swap(index, index - 1))
        }
    }

    /** One step towards the end; the last icon becomes the menu's first action. */
    fun down(action: OverviewAction): OverviewBar {
        val index = order.indexOf(action)
        return when {
            index < 0 -> this
            index == iconCount - 1 -> copy(iconCount = iconCount - 1)
            index == order.lastIndex -> this
            else -> copy(order = order.swap(index, index + 1))
        }
    }

    /** Puts an icon at the top of the menu. */
    fun toMenu(action: OverviewAction): OverviewBar {
        if (action !in icons) return this
        val rest = order - action
        return OverviewBar(rest.take(iconCount - 1) + action + rest.drop(iconCount - 1), iconCount - 1)
    }

    /** Shows a menu action as the last icon. */
    fun toBar(action: OverviewAction): OverviewBar {
        if (action !in menu) return this
        val rest = order - action
        return OverviewBar(rest.take(iconCount) + action + rest.drop(iconCount), iconCount + 1)
    }

    /** "HEALTH,EVENTS|METRICS,SETTINGS": the icons, then the menu after the bar. */
    fun encode(): String = icons.joinToString(",") + SEPARATOR + menu.joinToString(",")

    companion object {
        private const val DEFAULT_ICONS = 3
        private const val SEPARATOR = '|'

        /** Reads [encode]'s form; unknown or repeated names are skipped, missing actions go last in the menu. */
        fun parse(text: String?): OverviewBar {
            if (text.isNullOrBlank() || SEPARATOR !in text) return OverviewBar()
            val (iconPart, menuPart) = text.split(SEPARATOR, limit = 2)
            val icons = names(iconPart).distinct()
            val menu = names(menuPart).filter { it !in icons }.distinct()
            val known = icons + menu
            return OverviewBar(known + OverviewAction.entries.filter { it !in known }, icons.size)
        }

        private fun names(part: String): List<OverviewAction> =
            part.split(',').mapNotNull { raw -> OverviewAction.entries.firstOrNull { it.name == raw.trim() } }

        private fun <T> List<T>.swap(a: Int, b: Int): List<T> =
            toMutableList().apply { this[a] = this[b].also { this[b] = this[a] } }
    }
}
