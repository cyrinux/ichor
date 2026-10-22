"""Tests for crd-request.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("crd_request", os.path.join(HERE, "crd-request.py"))
crd_request = importlib.util.module_from_spec(spec)
spec.loader.exec_module(crd_request)

KUBE_GREEN = "https://raw.githubusercontent.com/kube-green/kube-green/main/config/crd/bases/kube-green.com_sleepinfos.yaml"


def body(url=KUBE_GREEN, operator="kube-green", docs="_No response_", notes="_No response_"):
    """An issue body as GitHub writes a submitted crd-request.yml form."""
    return (
        f"### CRD manifest URL\n\n{url}\n\n### Operator\n\n{operator}\n\n### Documentation link\n\n{docs}\n\n"
        f"### What should the app show?\n\n{notes}\n\n### Before submitting\n\n- [X] The CRD is public\n"
    )


class ParseTest(unittest.TestCase):
    def parse(self, text, number="12"):
        return crd_request.parse(text, number)

    def refused(self, text, number="12"):
        with self.assertRaises(crd_request.Refused) as ctx:
            self.parse(text, number)
        return str(ctx.exception)

    def test_happy_body(self):
        got = self.parse(body(docs="https://kube-green.dev/docs/"))
        self.assertEqual(got, {
            "crd_url": KUBE_GREEN, "operator": "kube-green", "docs": "https://kube-green.dev/docs/",
            "id": "kube-green", "branch": "crd-request/12-kube-green",
        })

    def test_no_response_fields_are_empty(self):
        self.assertEqual(self.parse(body())["docs"], "")

    def test_windows_line_ends(self):
        self.assertEqual(self.parse(body().replace("\n", "\r\n"))["crd_url"], KUBE_GREEN)

    def test_github_blob_link_becomes_raw(self):
        url = "https://github.com/kube-green/kube-green/blob/main/config/crd/bases/kube-green.com_sleepinfos.yaml"
        self.assertEqual(self.parse(body(url=url))["crd_url"], KUBE_GREEN)

    def test_gitlab_blob_link_becomes_raw(self):
        url = "https://gitlab.com/example-org/operator/-/blob/main/deploy/crds/widgets.yaml"
        self.assertEqual(self.parse(body(url=url))["crd_url"], "https://gitlab.com/example-org/operator/-/raw/main/deploy/crds/widgets.yaml")

    def test_codeberg_src_link_becomes_raw(self):
        url = "https://codeberg.org/example-org/operator/src/branch/main/crds/widgets.yml"
        self.assertEqual(self.parse(body(url=url))["crd_url"], "https://codeberg.org/example-org/operator/raw/branch/main/crds/widgets.yml")

    def test_github_page_that_is_not_a_file(self):
        self.assertIn("point at a file", self.refused(body(url="https://github.com/kube-green/kube-green")))

    def test_http_link(self):
        self.assertIn("https://", self.refused(body(url=KUBE_GREEN.replace("https", "http", 1))))

    def test_host_off_the_list(self):
        self.assertIn("must be on", self.refused(body(url="https://example.com/crds/widgets.yaml")))

    def test_lookalike_host(self):
        self.assertIn("must be on", self.refused(body(url="https://raw.githubusercontent.com.example.com/a/b/main/c.yaml")))

    def test_userinfo_in_the_authority(self):
        self.assertIn("user name", self.refused(body(url="https://raw.githubusercontent.com@example.com/a/b/main/c.yaml")))

    def test_port(self):
        self.assertIn("port", self.refused(body(url="https://raw.githubusercontent.com:8443/a/b/main/c.yaml")))

    def test_query_token(self):
        self.assertIn("query", self.refused(body(url=KUBE_GREEN + "?token=abc")))

    def test_not_a_manifest_file(self):
        self.assertIn(".yaml", self.refused(body(url="https://raw.githubusercontent.com/a/b/main/README.md")))

    def test_long_url(self):
        self.assertIn("plain link", self.refused(body(url="https://raw.githubusercontent.com/a/b/main/" + "x" * 2100 + ".yaml")))

    def test_three_megabyte_body(self):
        self.assertIn("longer", self.refused(body(notes="x" * 3_000_000)))

    def test_missing_url(self):
        self.assertIn("empty", self.refused(body(url="_No response_")))

    def test_shell_injection_in_the_operator_is_refused(self):
        for operator in ("$(id)", "`id`", "kube-green; rm -rf /", "a\nb"):
            with self.subTest(operator=operator):
                self.assertIn("operator", self.refused(body(operator=operator)))

    def test_shell_injection_in_the_url_is_refused(self):
        for url in (KUBE_GREEN + "$(id)", KUBE_GREEN + "`id`", KUBE_GREEN + " x"):
            with self.subTest(url=url):
                self.refused(body(url=url))

    def test_curl_globs_and_markdown_in_the_path(self):
        for path in ("x[1-9999].yaml", "x{a,b}.yaml", "![a](https:/x.example/p.png)/x.yaml", "[click](x)/x.yaml"):
            with self.subTest(path=path):
                self.refused(body(url="https://raw.githubusercontent.com/o/r/main/" + path))

    def test_unparseable_ports_are_refused_not_raised(self):
        for url in ("https://github.com:abc/o/r/blob/main/x.yaml", "https://github.com:99999999/o/r/blob/main/x.yaml",
                    "https://[x/o/r/main/x.yaml"):
            with self.subTest(url=url):
                self.refused(body(url=url))

    def test_docs_must_be_https(self):
        self.assertIn("documentation", self.refused(body(docs="javascript:alert(1)")))

    def test_bad_issue_number(self):
        self.assertIn("issue number", self.refused(body(), number="12; id"))

    def test_plugin_id_like_the_generator(self):
        self.assertEqual(crd_request.plugin_id("external-secrets.io"), "external-secrets")
        self.assertEqual(crd_request.plugin_id("Kube Green"), "kube-green")
        self.assertEqual(crd_request.plugin_id("cnpg.io"), "cnpg")

    def test_operator_with_no_id(self):
        self.assertIn("plugin id", self.refused(body(operator="...")))


class CommentsTest(unittest.TestCase):
    def test_fence_outlasts_backticks(self):
        out = crd_request.fenced("x ```` y")
        self.assertTrue(out.startswith("`````text\n"))
        self.assertTrue(out.endswith("\n`````"))

    def test_long_logs_are_cut(self):
        out = crd_request.fenced("\n".join(f"line {i}" for i in range(100)))
        self.assertIn("40 more lines", out)
        self.assertNotIn("line 99", out)

    def test_covered_by(self):
        log = "/tmp/crd/manifest.yaml: already covered by built-in plugin kube-green (-force writes a draft anyway)\n"
        self.assertEqual(crd_request.covered_by(log), "built-in plugin kube-green")
        native = "m.yaml: already covered by native integration cert-manager (-force writes a draft anyway)"
        self.assertEqual(crd_request.covered_by(native), "native integration cert-manager")
        self.assertEqual(crd_request.covered_by("something else"), "")

    def test_covered_cannot_be_spoofed_by_an_earlier_line(self):
        log = ("m.yaml: field says: already covered by built-in plugin evil (-force writes a draft anyway)\n"
               "m.yaml: the CRD has no spec.names.plural or spec.names.kind")
        self.assertEqual(crd_request.covered_by(log), "")

    def test_pr_body(self):
        env = {"ISSUE_NUMBER": "12", "OPERATOR": "Widgets", "CRD_URL": KUBE_GREEN, "ID": "widgets"}
        out = crd_request.pr_body("draft.yaml: 0 warnings", env)
        self.assertTrue(out.startswith("Closes #12\n"))
        self.assertIn(f"`{KUBE_GREEN}`", out)
        self.assertIn("git mv go/plugin/drafts/widgets.yaml go/plugin/builtin/", out)
        self.assertNotIn("<id>", out)
        self.assertNotIn("GITHUB_TOKEN", out)
        self.assertIn("GITHUB_TOKEN", crd_request.pr_body("", {**env, "FALLBACK": "1"}))

    def test_main_parse_prints_output_lines(self):
        os.environ["ISSUE_BODY"], os.environ["ISSUE_NUMBER"] = body(), "7"
        try:
            with tempfile.TemporaryFile("w+") as out:
                stdout, crd_request.sys.stdout = crd_request.sys.stdout, out
                try:
                    self.assertEqual(crd_request.main(["crd-request.py", "parse"]), 0)
                finally:
                    crd_request.sys.stdout = stdout
                out.seek(0)
                lines = out.read().splitlines()
        finally:
            del os.environ["ISSUE_BODY"], os.environ["ISSUE_NUMBER"]
        self.assertIn("branch=crd-request/7-kube-green", lines)
        self.assertTrue(all("\n" not in line and "=" in line for line in lines))


if __name__ == "__main__":
    unittest.main()
