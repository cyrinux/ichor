package name.levis.ichor.model

/** A home screen's card that can be moved and hidden; banners and notices stay on top. */
interface HomeCard {
    /** Only shown when the cluster has what it reports on. */
    val whenDetected: Boolean

    /** Was pinned above the cards before it could be arranged: a layout saved then keeps it first. */
    val leadsWhenNew: Boolean
}

/** The Talos overview's cards. */
enum class OverviewCard : HomeCard {
    TALOS_UPDATE,
    SUMMARY,
    APPS,
    DATA_SERVICES,
    ARGO_CD,
    FLUX,
    NODES,
    TIME_DRIFT,
    ;

    override val whenDetected: Boolean get() = this == DATA_SERVICES || this == ARGO_CD || this == FLUX

    override val leadsWhenNew: Boolean get() = this == TALOS_UPDATE

    companion object {
        val layout = CardLayoutKind(entries)
    }
}

/**
 * The cards of the Kubernetes home (a cluster added from a kubeconfig): the API server and the
 * credentials, the nodes as Kubernetes lists them, the Kubernetes screens, and the operators
 * found on the cluster.
 */
enum class KubeHomeCard : HomeCard {
    SUMMARY,
    APPS,
    NODES,
    TOOLS,
    DATA_SERVICES,
    ARGO_CD,
    FLUX,
    ;

    override val whenDetected: Boolean get() = this == DATA_SERVICES || this == ARGO_CD || this == FLUX

    override val leadsWhenNew: Boolean get() = false

    companion object {
        val layout = CardLayoutKind(entries)
    }
}

/** One home's cards ([entries], in their default order), reading their saved layout. */
class CardLayoutKind<C>(val entries: List<C>) where C : Enum<C>, C : HomeCard {
    val default: CardLayout<C> get() = CardLayout(this, entries, emptySet())

    /** Reads [CardLayout.encode]'s form; unknown or repeated names are skipped, missing cards added. */
    fun parse(text: String?): CardLayout<C> {
        if (text.isNullOrBlank()) return default
        val parsed = text.split(',').mapNotNull { raw ->
            val name = raw.trim()
            val hidden = name.startsWith("-")
            entries.firstOrNull { it.name == name.removePrefix("-") }?.let { it to hidden }
        }.distinctBy { it.first }
        val known = parsed.map { it.first }
        val (leading, trailing) = entries.filter { it !in known }.partition { it.leadsWhenNew }
        return CardLayout(
            this,
            order = leading + known + trailing,
            hidden = parsed.filter { it.second }.map { it.first }.toSet(),
        )
    }
}

/**
 * A home's cards in the order chosen, and those hidden. Every card is in [order] exactly once,
 * so a card added by a later release shows up (last, or first when it [HomeCard.leadsWhenNew])
 * without touching the saved layout.
 */
data class CardLayout<C>(
    val kind: CardLayoutKind<C>,
    val order: List<C>,
    val hidden: Set<C>,
) where C : Enum<C>, C : HomeCard {
    val visible: List<C> get() = order.filter { it !in hidden }
    val hiddenCards: List<C> get() = order.filter { it in hidden }
    val isDefault: Boolean get() = this == kind.default

    /** [visible] without the cards the cluster lacks ([absent]): what the editor offers to arrange. */
    fun visible(absent: Set<C>): List<C> = visible.filter { it !in absent }

    /** [hiddenCards] without the cards the cluster lacks ([absent]). */
    fun hiddenCards(absent: Set<C>): List<C> = hiddenCards.filter { it !in absent }

    /**
     * Moves the shown card at [from] to [to], both indices in [visible] without [absent]; absent
     * cards keep their place, for clusters that have them. Out of range: unchanged.
     */
    fun move(from: Int, to: Int, absent: Set<C> = emptySet()): CardLayout<C> {
        val shown = visible(absent)
        if (from !in shown.indices || to !in shown.indices || from == to) return this
        val moved = shown.toMutableList().apply { add(to, removeAt(from)) }.iterator()
        return copy(order = visible.map { if (it in absent) it else moved.next() } + hiddenCards)
    }

    fun hide(card: C): CardLayout<C> = copy(hidden = hidden + card)

    /** Shows [card] again, after the cards already shown. */
    fun show(card: C): CardLayout<C> {
        if (card !in hidden) return this
        val rest = hidden - card
        return copy(order = visible + card + order.filter { it in rest }, hidden = rest)
    }

    /** "SUMMARY,-APPS,NODES": the order, a dash before hidden cards. */
    fun encode(): String = order.joinToString(",") { if (it in hidden) "-${it.name}" else it.name }
}

/** The Talos overview's cards as arranged; one layout for every cluster. */
typealias OverviewLayout = CardLayout<OverviewCard>

/** The Kubernetes home's cards as arranged; one layout for every cluster added from a kubeconfig. */
typealias KubeHomeLayout = CardLayout<KubeHomeCard>
