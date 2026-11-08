package name.levis.ichor.ui.components

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.ZeroCornerSize
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp

/** Where a row sits in its card: decides which corners round and whether a divider follows it. */
enum class RowPosition { ONLY, FIRST, MIDDLE, LAST }

/** The [RowPosition] of row [index] in a card of [count] rows. */
fun rowPosition(index: Int, count: Int): RowPosition {
    require(index in 0 until count) { "row $index is outside a card of $count rows" }
    return when {
        count == 1 -> RowPosition.ONLY
        index == 0 -> RowPosition.FIRST
        index == count - 1 -> RowPosition.LAST
        else -> RowPosition.MIDDLE
    }
}

/** [items] split by [groupOf]: groups in order of first appearance, items keep their order. */
fun <T> groupInOrder(items: List<T>, groupOf: (T) -> String): List<Pair<String, List<T>>> =
    items.groupBy(groupOf).toList()

/** The `contentPadding` of a `LazyColumn` of card rows: room above the first card and below the last. */
val CardListPadding = PaddingValues(top = 8.dp, bottom = 16.dp)

/**
 * One row of an inset card, the lazy-list form of the home's cards: the card colour and the
 * theme's medium corners, rounded only where the card starts or ends ([position]), inset 16 dp
 * from the screen edges, with an inset divider under every row but the last.
 */
@Composable
fun RowCard(position: RowPosition, modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    val corners = MaterialTheme.shapes.medium
    val shape = when (position) {
        RowPosition.ONLY -> corners
        RowPosition.FIRST -> corners.copy(bottomStart = ZeroCornerSize, bottomEnd = ZeroCornerSize)
        RowPosition.LAST -> corners.copy(topStart = ZeroCornerSize, topEnd = ZeroCornerSize)
        RowPosition.MIDDLE -> RectangleShape
    }
    Surface(
        modifier.fillMaxWidth().padding(horizontal = 16.dp),
        shape = shape,
        color = CardDefaults.cardColors().containerColor,
    ) {
        Column {
            content()
            if (position == RowPosition.FIRST || position == RowPosition.MIDDLE) {
                HorizontalDivider(Modifier.padding(start = 16.dp))
            }
        }
    }
}

/** [items] as the rows of one inset card, each still a lazy item with its own [key]. */
fun <T> LazyListScope.cardRows(
    items: List<T>,
    key: ((T) -> Any)? = null,
    contentType: (T) -> Any? = { null },
    content: @Composable (T) -> Unit,
) {
    itemsIndexed(
        items,
        key = key?.let { k -> { _: Int, item: T -> k(item) } },
        contentType = { _, item -> contentType(item) },
    ) { index, item ->
        RowCard(rowPosition(index, items.size)) { content(item) }
    }
}

/** A title above a card, aligned with the card's content. [key] must differ from every row key. */
fun LazyListScope.cardGroupTitle(key: Any, text: String) {
    item(key = key, contentType = "cardGroupTitle") { SectionTitle(text, CardTitleInset) }
}

/** [cardGroupTitle] with the title from a string resource. */
fun LazyListScope.cardGroupTitle(key: Any, @StringRes text: Int) {
    item(key = key, contentType = "cardGroupTitle") { SectionTitle(stringResource(text), CardTitleInset) }
}

/** A card title's inset: aligned with the card's content, a little room above. */
private val CardTitleInset = Modifier.padding(start = 20.dp, end = 20.dp, top = 8.dp)

/** The space between two cards with no title between them. [key] must differ from every row key. */
fun LazyListScope.cardGap(key: Any) {
    item(key = key, contentType = "cardGap") { Spacer(Modifier.height(10.dp)) }
}

/**
 * [items] as one titled card per [groupOf] value (a namespace, a status), or as one untitled
 * card when [groupOf] is null.
 */
fun <T> LazyListScope.groupedCardRows(
    items: List<T>,
    groupOf: ((T) -> String)?,
    key: ((T) -> Any)? = null,
    contentType: (T) -> Any? = { null },
    content: @Composable (T) -> Unit,
) {
    if (groupOf == null) {
        cardRows(items, key, contentType, content)
        return
    }
    groupInOrder(items, groupOf).forEach { (group, rows) ->
        cardGroupTitle("cardGroup:$group", group)
        cardRows(rows, key, contentType, content)
    }
}
