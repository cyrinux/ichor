package name.levis.ichor.model

/** The overview's cards that can be moved and hidden; banners and notices stay on top. */
enum class OverviewCard {
    SUMMARY,
    APPS,
    DATA_SERVICES,
    ARGO_CD,
    FLUX,
    NODES,
    TIME_DRIFT,
    ;

    /** Only shown when the cluster has what they report on. */
    val whenDetected: Boolean get() = this == DATA_SERVICES || this == ARGO_CD || this == FLUX
}

/**
 * The overview's cards in the order chosen, and those hidden. Every card is in [order] exactly
 * once, so a card added by a later release shows up (last) without touching the saved layout.
 */
data class OverviewLayout(
    val order: List<OverviewCard> = OverviewCard.entries,
    val hidden: Set<OverviewCard> = emptySet(),
) {
    val visible: List<OverviewCard> get() = order.filter { it !in hidden }
    val hiddenCards: List<OverviewCard> get() = order.filter { it in hidden }
    val isDefault: Boolean get() = this == OverviewLayout()

    /** Moves the shown card at [from] to [to], both indices in [visible]. Out of range: unchanged. */
    fun move(from: Int, to: Int): OverviewLayout {
        val shown = visible
        if (from !in shown.indices || to !in shown.indices || from == to) return this
        val moved = shown.toMutableList().apply { add(to, removeAt(from)) }
        return copy(order = moved + hiddenCards)
    }

    fun hide(card: OverviewCard): OverviewLayout = copy(hidden = hidden + card)

    /** Shows [card] again, after the cards already shown. */
    fun show(card: OverviewCard): OverviewLayout {
        if (card !in hidden) return this
        val rest = hidden - card
        return OverviewLayout(order = visible + card + order.filter { it in rest }, hidden = rest)
    }

    /** "SUMMARY,-APPS,NODES": the order, a dash before hidden cards. */
    fun encode(): String = order.joinToString(",") { if (it in hidden) "-${it.name}" else it.name }

    companion object {
        /** Reads [encode]'s form; unknown or repeated names are skipped, missing cards appended. */
        fun parse(text: String?): OverviewLayout {
            if (text.isNullOrBlank()) return OverviewLayout()
            val entries = text.split(',').mapNotNull { raw ->
                val name = raw.trim()
                val hidden = name.startsWith("-")
                OverviewCard.entries.firstOrNull { it.name == name.removePrefix("-") }?.let { it to hidden }
            }.distinctBy { it.first }
            val known = entries.map { it.first }
            return OverviewLayout(
                order = known + OverviewCard.entries.filter { it !in known },
                hidden = entries.filter { it.second }.map { it.first }.toSet(),
            )
        }
    }
}
