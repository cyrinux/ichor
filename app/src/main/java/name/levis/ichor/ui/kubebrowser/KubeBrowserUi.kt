package name.levis.ichor.ui.kubebrowser

import android.content.Context
import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CellTone
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Browser screens a sheet deep in a Kubernetes list can open (a pod's YAML, a port-forward, a shell):
 * provided by the navigation, null where nothing can be opened.
 */
class KubeLinks(
    val onObject: (KubeObjectRef) -> Unit,
    val onPortForward: (namespace: String, pod: String) -> Unit,
    /** A terminal in [container] of the pod ("" for its only one), `kubectl exec -it`. */
    val onShell: (namespace: String, pod: String, container: String) -> Unit,
)

val LocalKubeLinks = compositionLocalOf<KubeLinks?> { null }

@Composable
fun CellTone.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        CellTone.OK -> colors.ok
        CellTone.WARN -> colors.warn
        CellTone.BAD -> colors.bad
        CellTone.NONE -> Color.Unspecified
    }
}

/** A short tinted label (a status, "Namespaced", "Deleting"). */
@Composable
fun ToneLabel(text: String, color: Color, modifier: Modifier = Modifier, mono: Boolean = false) {
    Text(
        text,
        style = MaterialTheme.typography.labelSmall,
        color = color,
        fontFamily = if (mono) FontFamily.Monospace else null,
        maxLines = 1,
        modifier = modifier
            .background(color.copy(alpha = 0.14f), RoundedCornerShape(6.dp))
            .padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

/** A red card saying why something was refused. */
@Composable
fun ErrorCard(text: String, modifier: Modifier = Modifier) {
    val bad = LocalStatusColors.current.bad
    Card(colors = CardDefaults.cardColors(containerColor = bad.copy(alpha = 0.15f)), modifier = modifier.fillMaxWidth()) {
        Text(text, color = bad, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(12.dp))
    }
}

/** Colours of the YAML highlighting: keys, comments and list dashes. */
data class YamlColors(val key: Color, val comment: Color, val dash: Color)

private val KEY = Regex("""^(\s*)(- )?("[^"]*"|'[^']*'|[^\s:#'"][^:#]*?):(\s|$)""")

/** [line] with its key, list dash and comment tinted: light, enough to read the structure. */
fun highlightYamlLine(line: String, colors: YamlColors): AnnotatedString = buildAnnotatedString {
    val trimmed = line.trimStart()
    if (trimmed.startsWith("#")) {
        withStyle(SpanStyle(color = colors.comment)) { append(line) }
        return@buildAnnotatedString
    }
    val key = KEY.find(line)
    if (key == null) {
        val dash = Regex("""^(\s*)- """).find(line)
        if (dash != null) {
            append(dash.groupValues[1])
            withStyle(SpanStyle(color = colors.dash)) { append("- ") }
            append(line.substring(dash.range.last + 1))
        } else {
            append(line)
        }
        return@buildAnnotatedString
    }
    val (indent, dash, name) = key.destructured
    append(indent)
    if (dash.isNotEmpty()) withStyle(SpanStyle(color = colors.dash)) { append(dash) }
    withStyle(SpanStyle(color = colors.key)) { append(name) }
    append(line.substring(indent.length + dash.length + name.length))
}

/** YAML (or any text) line by line, monospace, highlighted, selectable; long lines wrap. */
@Composable
fun YamlLines(text: String, modifier: Modifier = Modifier) {
    val colors = YamlColors(
        key = MaterialTheme.colorScheme.primary,
        comment = MaterialTheme.colorScheme.onSurfaceVariant,
        dash = MaterialTheme.colorScheme.tertiary,
    )
    val lines = remember(text) { text.removeSuffix("\n").split('\n') }
    SelectionContainer(modifier) {
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp)) {
            items(lines.size) { i ->
                val line = lines[i]
                Text(
                    remember(line, colors) { highlightYamlLine(line, colors) },
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                )
            }
        }
    }
}

/** A label and its value side by side, as a small rounded chip. */
@Composable
fun FieldChip(label: String, value: String, valueColor: Color = Color.Unspecified) {
    Row(
        Modifier
            .background(MaterialTheme.colorScheme.surfaceContainerHigh, RoundedCornerShape(8.dp))
            .padding(horizontal = 8.dp, vertical = 3.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        Text(value, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = valueColor, maxLines = 2)
    }
}

/** Copies [text] and says so; [sensitive] keeps it out of the clipboard preview. */
fun copyWithToast(context: Context, label: String, text: String, sensitive: Boolean) {
    copyToClipboard(context, label, text, sensitive)
    Toast.makeText(context, R.string.kb_copied, Toast.LENGTH_SHORT).show()
}
