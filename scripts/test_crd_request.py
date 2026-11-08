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

# Offline DNS: these names resolve to private addresses, any other name to a public one.
PRIVATE = {"intranet.example.com": "10.0.0.5", "rebind.example.com": "127.0.0.1", "meta.example.com": "169.254.169.254",
           "v6.example.com": "fd00::1"}


def fake_resolve(host, port, proto=0):
    if host == "nxdomain.example.com":
        raise OSError("Name or service not known")
    address = PRIVATE.get(host, "93.184.215.14")
    family = 10 if ":" in address else 2
    return [(family, 1, proto, "", (address, port))]


crd_request.RESOLVE = fake_resolve


def body(url=KUBE_GREEN, operator="kube-green", docs="_No response_", notes="_No response_", crds="_No response_"):
    """An issue body as GitHub writes a submitted crd-request.yml form."""
    return (
        f"### CRD manifest URL\n\n{url}\n\n### CRD names\n\n{crds}\n\n### Operator\n\n{operator}\n\n"
        f"### Documentation link\n\n{docs}\n\n"
        f"### What should the app show?\n\n{notes}\n\n### Before submitting\n\n- [X] The CRD is public\n"
    )


def fake_github(repos, pages):
    """A GitHub API of search pages (query -> items) and repositories (full_name -> fields)."""
    calls = []

    def api(path, params, token):
        calls.append(path)
        if path == "/search/code":
            return {"items": pages.get(params["q"], [])}
        return repos[path.removeprefix("/repos/")]
    return api, calls


def hit(repo, path, fork=False):
    return {"path": path, "repository": {"full_name": repo, "fork": fork}}


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
            "crd_url": KUBE_GREEN, "fetch_url": KUBE_GREEN, "crds": "", "operator": "kube-green", "docs": "https://kube-green.dev/docs/",
            "id": "kube-green", "branch": "crd-request/12-kube-green",
        })

    def test_crd_names_instead_of_a_link(self):
        got = self.parse(body(url="_No response_", crds="clusterqueues.kueue.x-k8s.io\n`localqueues.kueue.x-k8s.io`\n", operator="kueue.x-k8s.io"))
        self.assertEqual(got["crd_url"], "")
        self.assertEqual(got["crds"], "clusterqueues.kueue.x-k8s.io localqueues.kueue.x-k8s.io")
        self.assertEqual(got["id"], "kueue")

    def test_crd_names_in_a_code_fence(self):
        got = self.parse(body(url="_No response_", crds="```text\nsleepinfos.kube-green.com\n```"))
        self.assertEqual(got["crds"], "sleepinfos.kube-green.com")

    def test_bad_crd_names(self):
        for crds in ("SleepInfo", "sleepinfos", "a.b; rm -rf /", "$(id).example.io", "x" * 254 + ".io"):
            with self.subTest(crds=crds):
                self.assertIn("CRD name", self.refused(body(url="_No response_", crds=crds)))
        self.assertIn("at most", self.refused(body(url="_No response_", crds="\n".join(f"a{i}.example.io" for i in range(21)))))

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

    def test_self_hosted_gitlab_blob_link_becomes_raw(self):
        url = "https://forge.example.net/example-org/operator/-/blob/main/deploy/crds/widgets.yaml"
        self.assertEqual(self.parse(body(url=url))["crd_url"], "https://forge.example.net/example-org/operator/-/raw/main/deploy/crds/widgets.yaml")

    def test_self_hosted_forgejo_src_link_becomes_raw(self):
        url = "https://git.example.org/example-org/operator/src/tag/v1.2.0/crds/widgets.yaml"
        self.assertEqual(self.parse(body(url=url))["crd_url"], "https://git.example.org/example-org/operator/raw/tag/v1.2.0/crds/widgets.yaml")

    def test_raw_link_on_any_public_host_is_kept(self):
        url = "https://forge.example.net/example-org/operator/-/raw/main/crds/widgets.yaml"
        self.assertEqual(self.parse(body(url=url))["crd_url"], url)

    def test_self_hosted_gitlab_file_is_fetched_through_its_api(self):
        url = "https://forge.example.net/example-org/operator/-/blob/main/config/crd/bases/example.org_widgets.yaml"
        got = self.parse(body(url=url))
        self.assertEqual(got["crd_url"], "https://forge.example.net/example-org/operator/-/raw/main/config/crd/bases/example.org_widgets.yaml")
        self.assertEqual(got["fetch_url"], "https://forge.example.net/api/v4/projects/example-org%2Foperator/repository/files/"
                                           "config%2Fcrd%2Fbases%2Fexample.org_widgets.yaml/raw?ref=main")

    def test_gitlab_api_link_keeps_nested_groups_and_encodes_once(self):
        url = "https://gitlab.com/example-org/team/operator/-/raw/v1.2.0/deploy/my%20crds/widgets.yaml"
        self.assertEqual(self.parse(body(url=url))["fetch_url"], "https://gitlab.com/api/v4/projects/example-org%2Fteam%2Foperator/"
                                                                 "repository/files/deploy%2Fmy%20crds%2Fwidgets.yaml/raw?ref=v1.2.0")

    def test_other_forges_are_fetched_from_the_raw_link(self):
        url = "https://git.example.org/example-org/operator/src/tag/v1.2.0/crds/widgets.yaml"
        got = self.parse(body(url=url))
        self.assertEqual(got["fetch_url"], got["crd_url"])

    def test_ip_address_host(self):
        for url in ("https://93.184.215.14/a/b.yaml", "https://[::1]/a/b.yaml"):
            with self.subTest(url=url):
                self.assertIn("IP address", self.refused(body(url=url)))

    def test_internal_names(self):
        for host in ("localhost", "forge", "git.corp", "gitlab.internal", "forge.local"):
            with self.subTest(host=host):
                self.assertIn("public forge", self.refused(body(url=f"https://{host}/a/b/-/raw/main/c.yaml")))

    def test_host_resolving_to_a_private_address(self):
        for host in PRIVATE:
            with self.subTest(host=host):
                self.assertIn("public forge", self.refused(body(url=f"https://{host}/a/b/-/raw/main/c.yaml")))

    def test_host_that_does_not_resolve(self):
        self.assertIn("resolve", self.refused(body(url="https://nxdomain.example.com/a/b/-/raw/main/c.yaml")))

    def test_github_page_that_is_not_a_file(self):
        self.assertIn("point at a file", self.refused(body(url="https://github.com/kube-green/kube-green")))

    def test_http_link(self):
        self.assertIn("https://", self.refused(body(url=KUBE_GREEN.replace("https", "http", 1))))


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

    def test_missing_url_and_names(self):
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


class FetchTest(unittest.TestCase):
    def test_check_url_takes_the_api_query_it_built(self):
        crd_request.check_url("https://forge.example.net/api/v4/projects/a%2Fb/repository/files/c.yaml/raw?ref=main", query=True)

    def test_web_page_is_not_a_manifest(self):
        page = "\n<!doctype html><html><head><title>Making sure you&#39;re not a bot!</title>"
        self.assertTrue(crd_request.is_web_page(page))
        self.assertTrue(crd_request.is_web_page("﻿<html>"))
        self.assertFalse(crd_request.is_web_page("---\napiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\n"))
        self.assertFalse(crd_request.is_web_page('{"apiVersion": "apiextensions.k8s.io/v1"}'))

    def test_main_check_manifest_refuses_a_web_page(self):
        with tempfile.NamedTemporaryFile("w", suffix=".yaml") as f:
            f.write("<!doctype html><html></html>")
            f.flush()
            with tempfile.TemporaryFile("w+") as err:
                stderr, crd_request.sys.stderr = crd_request.sys.stderr, err
                try:
                    self.assertEqual(crd_request.main(["crd-request.py", "check-manifest", f.name]), 1)
                finally:
                    crd_request.sys.stderr = stderr
                err.seek(0)
                self.assertIn("a web page", err.read())


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

    def test_search_prefers_the_repository_named_after_the_project(self):
        pages = {"filename:kueue.x-k8s.io_clusterqueues.yaml": [
            hit("big/charts", "charts/kueue/crds/kueue.x-k8s.io_clusterqueues.yaml"),
            hit("someone/kueue", "config/crd/bases/kueue.x-k8s.io_clusterqueues.yaml", fork=True),
            hit("kubernetes-sigs/kueue", "charts/kueue/templates/crd/kueue.x-k8s.io_clusterqueues.yaml"),
            hit("kubernetes-sigs/kueue", "config/components/crd/bases/kueue.x-k8s.io_clusterqueues.yaml"),
        ]}
        repos = {
            "big/charts": {"stargazers_count": 50000, "default_branch": "main", "homepage": ""},
            "kubernetes-sigs/kueue": {"stargazers_count": 3000, "default_branch": "main", "homepage": "https://kueue.sigs.k8s.io"},
        }
        api, calls = fake_github(repos, pages)
        got = crd_request.search(["clusterqueues.kueue.x-k8s.io", "localqueues.kueue.x-k8s.io"], "t", api)
        self.assertEqual(got, {
            "crd_url": "https://raw.githubusercontent.com/kubernetes-sigs/kueue/main/config/components/crd/bases/kueue.x-k8s.io_clusterqueues.yaml",
            "website": "https://kueue.sigs.k8s.io", "repo": "kubernetes-sigs/kueue",
        })
        self.assertNotIn("/repos/someone/kueue", calls)

    def test_search_falls_back_to_stars_and_the_repository_page(self):
        pages = {
            "filename:postgresql.cnpg.io_clusters.yaml": [],
            "filename:postgresql.cnpg.io_clusters.yml": [],
            '"name: clusters.postgresql.cnpg.io" extension:yaml': [
                hit("certified/operators", "operators/cloudnative-pg/1.22.2/manifests/postgresql.cnpg.io_clusters.yaml"),
                hit("cloudnative-pg/cloudnative-pg", "config/crd/bases/postgresql.cnpg.io_clusters.yaml"),
                hit("old/mirror", "crds/clusters.yaml"),
            ],
        }
        repos = {
            "certified/operators": {"stargazers_count": 200, "default_branch": "main", "homepage": "http://insecure.example"},
            "cloudnative-pg/cloudnative-pg": {"stargazers_count": 9000, "default_branch": "main", "homepage": None},
            "old/mirror": {"stargazers_count": 99999, "default_branch": "main", "archived": True},
        }
        api, _ = fake_github(repos, pages)
        got = crd_request.search(["clusters.postgresql.cnpg.io"], "", api)
        self.assertEqual(got["repo"], "cloudnative-pg/cloudnative-pg")
        self.assertEqual(got["website"], "https://github.com/cloudnative-pg/cloudnative-pg")
        self.assertTrue(got["crd_url"].endswith("/main/config/crd/bases/postgresql.cnpg.io_clusters.yaml"))

    def test_search_learns_the_project_name_from_operator_catalogs(self):
        pages = {"filename:postgresql.cnpg.io_clusters.yaml": [
            hit("k8s-operatorhub/community-operators", "operators/cloudnative-pg/1.23.4/manifests/postgresql.cnpg.io_clusters.yaml"),
            hit("k8s-operatorhub/community-operators", "operators/cloudnative-pg/1.21.0/manifests/postgresql.cnpg.io_clusters.yaml"),
            hit("cloudnative-pg/artifacts", "bundles/1.21.5/manifests/postgresql.cnpg.io_clusters.yaml"),
            hit("cloudnative-pg/artifacts", "bundles/1.23.10/manifests/postgresql.cnpg.io_clusters.yaml"),
            hit("cloudnative-pg/artifacts", "bundles/1.23.9/manifests/postgresql.cnpg.io_clusters.yaml"),
        ]}
        repos = {
            "k8s-operatorhub/community-operators": {"stargazers_count": 900, "default_branch": "main", "homepage": "https://operatorhub.io"},
            "cloudnative-pg/artifacts": {"stargazers_count": 5, "default_branch": "main", "homepage": "https://cloudnative-pg.io"},
        }
        api, _ = fake_github(repos, pages)
        got = crd_request.search(["clusters.postgresql.cnpg.io"], "", api)
        self.assertEqual(got["repo"], "cloudnative-pg/artifacts")
        self.assertTrue(got["crd_url"].endswith("/bundles/1.23.10/manifests/postgresql.cnpg.io_clusters.yaml"))
        self.assertEqual(got["website"], "https://cloudnative-pg.io")

    def test_best_path_prefers_the_source_over_a_bundle(self):
        self.assertEqual(crd_request.best_path(["bundle/manifests/x.yaml", "config/crd/bases/x.yaml"]), "config/crd/bases/x.yaml")
        self.assertEqual(crd_request.best_path(["bundles/v1.9.0/x.yaml", "bundles/v1.10.0/x.yaml"]), "bundles/v1.10.0/x.yaml")

    def test_search_nothing_found_is_refused_within_the_query_budget(self):
        api, calls = fake_github({}, {})
        with self.assertRaises(crd_request.Refused) as ctx:
            crd_request.search([f"a{i}.example.io" for i in range(5)], "", api)
        self.assertIn("give its link", str(ctx.exception))
        self.assertEqual(len(calls), crd_request.MAX_QUERIES)

    def test_search_refuses_an_odd_path_or_branch(self):
        pages = {"filename:example.io_things.yaml": [hit("acme/things", "crds/example.io_things.yaml")]}
        api, _ = fake_github({"acme/things": {"stargazers_count": 1, "default_branch": "$(id)"}}, pages)
        with self.assertRaises(crd_request.Refused):
            crd_request.search(["things.example.io"], "", api)
        self.assertEqual(crd_request.manifest_candidates([hit("acme/things", "crds/`x`.yaml"), hit("acme/things", "a/templates/x.yaml")]), {})

    def test_pr_body(self):
        env = {"ISSUE_NUMBER": "12", "OPERATOR": "Widgets", "CRD_URL": KUBE_GREEN, "ID": "widgets"}
        out = crd_request.pr_body("draft.yaml: 0 warnings", env)
        self.assertTrue(out.startswith("Closes #12\n"))
        self.assertIn(f"`{KUBE_GREEN}`", out)
        self.assertIn("git mv go/plugin/drafts/widgets.yaml go/plugin/builtin/", out)
        self.assertNotIn("<id>", out)
        self.assertNotIn("GITHUB_TOKEN", out)
        self.assertIn("GITHUB_TOKEN", crd_request.pr_body("", {**env, "FALLBACK": "1"}))
        self.assertNotIn("searching GitHub", out)
        self.assertIn("**kube-green/kube-green**", crd_request.pr_body("", {**env, "FOUND_REPO": "kube-green/kube-green"}))

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


class TranslateTest(unittest.TestCase):
    PLUGIN = (
        "# yaml-language-server: $schema=../schema/plugin.schema.json\n"
        "# Draft written by probe plugin-from-crd from nodedrains.draino.acides.org (v1alpha1).\n\n"
        "id: draino\nresources:\n  nodedrain:\n    kind: NodeDrain\n    scope: Cluster\n"
    )

    def test_request_carries_the_missing_labels_and_the_plugin_words(self):
        report = {"ok": True, "id": "draino", "filled": 3,
                  "missing": {"fr": ["Node", "%d NodeDrains"], "xx": ["Node"], "de": []}}
        request = crd_request.translate_request(report, self.PLUGIN)
        self.assertEqual(request["model"], crd_request.MODEL)
        self.assertEqual(request["response_format"], {"type": "json_object"})
        task = crd_request.json.loads(request["messages"][1]["content"])
        self.assertEqual(task["translate"], {"fr": ["Node", "%d NodeDrains"]})
        self.assertEqual(task["kinds"], ["NodeDrain"])
        self.assertIn("nodedrains.draino.acides.org", task["about"])
        self.assertNotIn("$schema", task["about"])
        self.assertIn("uk = Ukrainian", request["messages"][0]["content"])

    def test_request_is_none_when_nothing_is_missing(self):
        self.assertIsNone(crd_request.translate_request({"missing": {}}, self.PLUGIN))
        self.assertIsNone(crd_request.translate_request({"missing": {"fr": []}}, self.PLUGIN))
        self.assertIsNone(crd_request.translate_request({}, self.PLUGIN))

    def completion(self, content):
        return crd_request.json.dumps({"choices": [{"message": {"role": "assistant", "content": content}}]})

    def test_response_keeps_the_shape_only(self):
        answer = {"fr": {"Node": " Nœud ", "Age": "Â\nge", "Empty": "", "Long": "x" * 500, "N": 3},
                  "xx": {"Node": "x"}, "de": "Knoten", "uk": {"Node": "Вузол"}}
        out = crd_request.translate_response(self.completion("```json\n" + crd_request.json.dumps(answer) + "\n```"))
        self.assertEqual(out, {"fr": {"Node": "Nœud"}, "uk": {"Node": "Вузол"}})

    def test_response_refuses_what_is_not_translations(self):
        for body in ["not json", "{}", self.completion("not json"), self.completion("[1]"),
                     self.completion('{"fr": {}}'), self.completion('{"xx": {"Node": "x"}}')]:
            with self.assertRaises(crd_request.Refused):
                crd_request.translate_response(body)

    def test_promotion_ticks_the_translation_step_when_the_lint_sees_none_missing(self):
        env = {"ISSUE_NUMBER": "12", "OPERATOR": "Widgets", "CRD_URL": KUBE_GREEN, "ID": "widgets"}
        clean = crd_request.pr_body('{"ok":true,"warnings":0}', env)
        self.assertIn("- [x] Translate every label for fr, de, es, it and uk (`i18n`): done by the workflow, "
                      "proofread them.", clean)
        partial = crd_request.pr_body('draft.yaml:12: warning: cards.list.row.detail[0].items[1].label: '
                                      '"Node" has no translation in fr', env)
        self.assertIn("- [ ] Translate every label for fr, de, es, it and uk (`i18n`).", partial)
        none = crd_request.pr_body("warning: no i18n: the 11 labels stay English in every language", env)
        self.assertIn("- [ ] Translate every label", none)
        self.assertNotIn("<i18n", clean + partial + none)


if __name__ == "__main__":
    unittest.main()
