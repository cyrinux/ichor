package name.levis.ichor.ui.machineconfig

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ConfigSyntaxError
import name.levis.ichor.ui.theme.LocalStatusColors

/** Lines of [text] containing [query] (case-insensitive); every line when [query] is blank. */
fun matchingLines(text: String, query: String): List<String> {
    val lines = text.lines()
    val q = query.trim()
    return if (q.isEmpty()) lines else lines.filter { it.contains(q, ignoreCase = true) }
}

/** The config as text, read-only, with the matches of [query] highlighted. */
@Composable
fun YamlView(lines: List<String>, query: String) {
    val highlight = MaterialTheme.colorScheme.tertiaryContainer
    val text = remember(lines, query, highlight) { highlighted(lines, query.trim(), highlight) }
    SelectionContainer {
        Box(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .horizontalScroll(rememberScrollState())
                .padding(horizontal = 16.dp, vertical = 8.dp),
        ) {
            Text(text, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, softWrap = false)
        }
    }
}

private fun highlighted(lines: List<String>, query: String, color: Color): AnnotatedString = buildAnnotatedString {
    val text = lines.joinToString("\n")
    append(text)
    if (query.isEmpty()) return@buildAnnotatedString
    var from = text.indexOf(query, ignoreCase = true)
    while (from >= 0) {
        addStyle(SpanStyle(background = color), from, from + query.length)
        from = text.indexOf(query, from + query.length, ignoreCase = true)
    }
}

/** The draft as text. [error] is why it is not valid YAML right now, if it is not. */
@Composable
fun ConfigYamlEditor(draft: String, error: ConfigSyntaxError?, onChange: (String) -> Unit, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxSize().imePadding()) {
        error?.let { SyntaxErrorCard(it, Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
        OutlinedTextField(
            value = draft,
            onValueChange = onChange,
            textStyle = TextStyle(fontFamily = FontFamily.Monospace, fontSize = MaterialTheme.typography.bodySmall.fontSize),
            keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false, keyboardType = KeyboardType.Ascii),
            isError = error != null,
            modifier = Modifier.fillMaxSize().padding(horizontal = 16.dp, vertical = 8.dp),
        )
    }
}

@Composable
fun SyntaxErrorCard(error: ConfigSyntaxError, modifier: Modifier = Modifier) {
    val bad = LocalStatusColors.current.bad
    Card(colors = CardDefaults.cardColors(containerColor = bad.copy(alpha = 0.15f)), modifier = modifier.fillMaxWidth()) {
        Text(
            if (error.line > 0) stringResource(R.string.machine_config_syntax_error, error.line, error.message) else error.message,
            color = bad,
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(12.dp),
        )
    }
}
