package name.levis.ichor.model

/** The overview's cards that can be moved and hidden; banners and notices stay on top. */
enum class OverviewCard {
    TALOS_UPDATE,
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

    /** Was pinned above the cards before it could be arranged: a layout saved then keeps it first. */
    val leadsWhenNew: Boolean get() = this == TALOS_UPDATE
}

/**
 * The overview's cards in the order chosen, and those hidden. Every card is in [order] exactly
 * once, so a card added by a later release shows up (last, or first when it [OverviewCard.leadsWhenNew])
 * without touching the saved layout.
 */
data class OverviewLayout(
    val order: List<OverviewCard> = OverviewCard.entries,
    val hidden: Set<OverviewCard> = emptySet(),
) {
    val visible: List<OverviewCard> get() = order.filter { it !in hidden }
    val hiddenCards: List<OverviewCard> get() = order.filter { it in hidden }
    val isDefault: Boolean get() = this == OverviewLayout()

    /** [visible] without the cards the cluster lacks ([absent]): what the editor offers to arrange. */
    fun visible(absent: Set<OverviewCard>): List<OverviewCard> = visible.filter { it !in absent }

    /** [hiddenCards] without the cards the cluster lacks ([absent]). */
    fun hiddenCards(absent: Set<OverviewCard>): List<OverviewCard> = hiddenCards.filter { it !in absent }

    /**
     * Moves the shown card at [from] to [to], both indices in [visible] without [absent]; absent
     * cards keep their place, for clusters that have them. Out of range: unchanged.
     */
    fun move(from: Int, to: Int, absent: Set<OverviewCard> = emptySet()): OverviewLayout {
        val shown = visible(absent)
        if (from !in shown.indices || to !in shown.indices || from == to) return this
        val moved = shown.toMutableList().apply { add(to, removeAt(from)) }.iterator()
        return copy(order = visible.map { if (it in absent) it else moved.next() } + hiddenCards)
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
        /** Reads [encode]'s form; unknown or repeated names are skipped, missing cards added. */
        fun parse(text: String?): OverviewLayout {
            if (text.isNullOrBlank()) return OverviewLayout()
            val entries = text.split(',').mapNotNull { raw ->
                val name = raw.trim()
                val hidden = name.startsWith("-")
                OverviewCard.entries.firstOrNull { it.name == name.removePrefix("-") }?.let { it to hidden }
            }.distinctBy { it.first }
            val known = entries.map { it.first }
            val (leading, trailing) = OverviewCard.entries.filter { it !in known }.partition { it.leadsWhenNew }
            return OverviewLayout(
                order = leading + known + trailing,
                hidden = entries.filter { it.second }.map { it.first }.toSet(),
            )
        }
    }
}
