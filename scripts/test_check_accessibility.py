"""Tests for check-accessibility.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("check_accessibility", os.path.join(HERE, "check-accessibility.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class ImageButtonTest(unittest.TestCase):
    def test_one_line_buttons(self):
        src = """Button(action: up) { Image(systemName: "arrow.up") }
    .buttonStyle(.borderless)
Button { next() } label: { Image(systemName: "chevron.right") }
    .accessibilityLabel(Text("Next page"))
"""
        self.assertEqual(check.unlabeled_image_buttons(src), [1])

    def test_multi_line_label(self):
        src = """Button {
    remove()
} label: {
    Image(systemName: "minus.circle").foregroundStyle(.red)
}
.buttonStyle(.borderless)
"""
        self.assertEqual(check.unlabeled_image_buttons(src), [3])

    def test_label_on_a_later_button_does_not_count(self):
        src = """Button(action: a) { Image(systemName: "a") }
Button(action: b) { Image(systemName: "b") }
    .accessibilityLabel(Text("B"))
"""
        self.assertEqual(check.unlabeled_image_buttons(src), [1])

    def test_buttons_with_text_or_help(self):
        src = """Button(action: a) { Label("Add", systemImage: "plus") }
Button(action: b) { Image(systemName: "b") }
    .help("Back")
Button {
    Image(systemName: "c")
    Text("Copy")
}
"""
        self.assertEqual(check.unlabeled_image_buttons(src), [])


class SwiftFontTest(unittest.TestCase):
    def test_small_literal_sizes_only(self):
        src = """Image(systemName: "x").font(.system(size: 9, weight: .bold))
Image(systemName: "lock").font(.system(size: 44))
Text(initials).font(.system(size: size * 0.36))
Text("a").font(.system(size: 11))
static let font = Font.system(size: 11, design: .monospaced)
.font(.system(size: base, design: .monospaced))
"""
        self.assertEqual(check.small_swift_fonts(src), [1, 4, 5])


class SpTest(unittest.TestCase):
    def test_small_text_sizes(self):
        src = """Text(a, fontSize = 11.sp, lineHeight = 14.sp)
Text(b, fontSize = 12.sp)
Text(c, letterSpacing = 0.sp)
autoSize = TextAutoSize.StepBased(minFontSize = 10.sp, maxFontSize = 20.sp)
withStyle(SpanStyle(fontSize = 10.sp)) { }
"""
        self.assertEqual(check.small_sp(src), [1, 5])


class IconButtonTest(unittest.TestCase):
    def test_unlabeled(self):
        src = """IconButton(onClick = close) {
    Icon(
        Icons.Outlined.Close,
        contentDescription = null,
    )
}
"""
        self.assertEqual(check.unlabeled_icon_buttons(src), [1])

    def test_labelled_icon_or_button(self):
        src = """IconButton(onClick = close) { Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.close)) }
FilledTonalIconButton(onClick = up, modifier = Modifier.semantics { contentDescription = up }) {
    Icon(Icons.Outlined.ArrowUpward, contentDescription = null)
}
IconButton(onClick = {
    open()
}) { Icon(Icons.Outlined.Menu, contentDescription = null) }
"""
        self.assertEqual(check.unlabeled_icon_buttons(src), [5])


if __name__ == "__main__":
    unittest.main()
