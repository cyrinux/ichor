"""Tests for check-section-lifecycle.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import tempfile
import unittest
from pathlib import Path

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location(
    "check_section_lifecycle", os.path.join(HERE, "check-section-lifecycle.py")
)
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)

ON_SECTION = """struct S: View {
    var body: some View {
        Section("Title") {
            Text("a")
            if shown { Text("b") }
        }
        .sheet(isPresented: $open) { Text("c") }
        .onChange(of: value) { _, now in
            go(now)
        }
        .task(id: key) { await load() }
    }
}
"""

WITH_HEADER = """struct S: View {
    var body: some View {
        // A comment first.
        Section {
            Text("a")
        } header: {
            Text("Title")
        } footer: {
            Text("Footer")
        }
        .onDisappear(perform: commit)
    }
}
"""

FIXED = """struct S: View {
    var body: some View {
        Section {
            Text("a")
                // On this row, always shown.
                .task { await load() }
        } header: {
            Text("Title")
                .onAppear { seen = true }
        }
        .sheet(isPresented: $open) { Text("c") }
    }
}
"""

NOT_A_SECTION = """struct S: View {
    var body: some View {
        List {
            Section {
                Text("a")
            }
            .task { await load() }
        }
        .onAppear { seen = true }
    }

    private var rows: some View {
        Section {
            Text("a")
        }
        .task { await load() }
    }
}
"""


class FileOffendersTest(unittest.TestCase):
    def test_modifiers_after_the_section_fail(self):
        self.assertEqual(check.file_offenders(ON_SECTION), [(8, "onChange"), (11, "task")])

    def test_header_and_footer_closures_are_followed(self):
        self.assertEqual(check.file_offenders(WITH_HEADER), [(11, "onDisappear")])

    def test_modifiers_on_a_row_or_the_header_pass(self):
        self.assertEqual(check.file_offenders(FIXED), [])

    def test_only_a_body_that_is_a_section(self):
        self.assertEqual(check.file_offenders(NOT_A_SECTION), [])


class OffendersTest(unittest.TestCase):
    def setUp(self):
        self._dir = tempfile.TemporaryDirectory()
        self.root = Path(self._dir.name)

    def tearDown(self):
        self._dir.cleanup()

    def write(self, rel: str, text: str):
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def test_names_file_and_line(self):
        self.write("ios/Ichor/Views/S.swift", ON_SECTION)
        self.write("ios/build/Generated.swift", ON_SECTION)
        self.assertEqual(
            check.offenders(self.root),
            [
                "ios/Ichor/Views/S.swift:8: .onChange on a body Section",
                "ios/Ichor/Views/S.swift:11: .task on a body Section",
            ],
        )

    def test_repository_passes(self):
        self.assertEqual(check.offenders(Path(HERE).parent), [])


if __name__ == "__main__":
    unittest.main()
