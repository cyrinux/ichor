#!/usr/bin/env node
// Clicks everything on the website that looks clickable and fails when a click does nothing.
//
//   node scripts/check-site-buttons.mjs            (or: just site-buttons-check)
//
// Serves docs/ on a local port, then for each page (/, /features.html, /parts/ empty and with a
// sample part) at 1280 and 390 px wide clicks every visible <button>, [role=button], link and
// element whose computed cursor is "pointer". A click counts when it changes the DOM, the hash
// or the URL, starts a navigation (followed for this site, aborted for any other host), opens a
// window or trips a form's validation. The clipboard refuses every write, so a Copy button must
// answer without it. Links to another scheme (mailto:, bitcoin:…) hand off to the system and
// pass on their href alone. It also fails on a "Works with" pill that looks like a button (a
// border) without being a link. Local only: the Pages workflow has no browser.
import { createServer } from "node:http";
import { statSync } from "node:fs";
import { readFile, stat } from "node:fs/promises";
import { extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

// `just site-buttons-check` puts playwright-core on NODE_PATH; a full playwright works too.
const require = createRequire(import.meta.url);
const load = (name) => { try { return require(name); } catch { return null; } };
const { chromium } = load("playwright") || load("playwright-core") || {};
if (!chromium) {
  console.error("check-site-buttons: no Playwright; run it with `just site-buttons-check`");
  process.exit(2);
}
// A system Chromium (CHROMIUM, else chromium on PATH) when Playwright brought no browser of its own.
const executablePath = process.env.CHROMIUM || (() => {
  for (const dir of (process.env.PATH || "").split(":")) {
    for (const name of ["chromium", "chromium-browser", "google-chrome"]) {
      try { if (statSync(join(dir, name)).isFile()) return join(dir, name); } catch { /* not here */ }
    }
  }
  return undefined;
})();

// SITE_DIR checks another copy of the site (e.g. an older one, to see the check fail).
const DOCS = process.env.SITE_DIR || fileURLToPath(new URL("../docs/", import.meta.url));
// /parts/ with a sample part (made-up values), so its rows and Copy buttons show.
const PAGES = ["/", "/features.html", "/parts/", "/parts/#v=1&k=disk&m=Example%20Corp&p=EX-1000&s=SN0000001"];
const WIDTHS = [1280, 390];
const SETTLE_MS = 250;
let clicked = 0;
const TYPES = { ".html": "text/html", ".css": "text/css", ".js": "text/javascript", ".svg": "image/svg+xml",
  ".webp": "image/webp", ".png": "image/png", ".json": "application/json", ".woff2": "font/woff2" };

function serve() {
  const server = createServer(async (req, res) => {
    try {
      let path = normalize(decodeURIComponent(new URL(req.url, "http://x").pathname)).replace(/^(\.\.[/\\])+/, "");
      let file = join(DOCS, path);
      if ((await stat(file)).isDirectory()) file = join(file, "index.html");
      res.writeHead(200, { "content-type": TYPES[extname(file)] || "application/octet-stream" });
      res.end(await readFile(file));
    } catch {
      res.writeHead(404).end();
    }
  });
  return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve(server)));
}

// In the page: the clickable elements, outermost only (a span in a link inherits its cursor).
function clickables() {
  const visible = (el) => {
    const box = el.getBoundingClientRect();
    const style = getComputedStyle(el);
    return box.width > 0 && box.height > 0 && style.visibility !== "hidden" && style.display !== "none";
  };
  const pointer = (el) => getComputedStyle(el).cursor === "pointer";
  const all = [...document.querySelectorAll("body *")].filter((el) =>
    (el.matches("button, [role=button], a[href], summary") || pointer(el)) && visible(el));
  const set = new Set(all);
  return all.filter((el) => {
    for (let p = el.parentElement; p; p = p.parentElement) if (set.has(p)) return false;
    return true;
  }).map((el, i) => {
    el.dataset.checkClick = String(i);
    const href = el.getAttribute("href");
    const name = (el.getAttribute("aria-label") || el.textContent || "").trim().replace(/\s+/g, " ").slice(0, 50);
    return { i, tag: el.tagName.toLowerCase(), name, href };
  });
}

// In the page: counts every reaction a click can have.
function watch() {
  window.__reactions = 0;
  const bump = () => { window.__reactions++; };
  new MutationObserver(bump).observe(document, { subtree: true, childList: true, attributes: true, characterData: true });
  addEventListener("hashchange", bump);
  addEventListener("invalid", bump, true);
}

// In the page: pills styled as buttons (a border) with no link inside.
function fakePills() {
  return [...document.querySelectorAll(".chips li, .strip li, .provider-grid > *")]
    .filter((el) => !el.matches("a") && !el.querySelector("a"))
    .filter((el) => getComputedStyle(el).borderTopStyle !== "none" && getComputedStyle(el).borderTopWidth !== "0px")
    .map((el) => el.textContent.trim());
}

async function check(browser, base, path, width) {
  const context = await browser.newContext({ viewport: { width, height: 900 } });
  let navigations = 0;
  let popups = 0;
  context.on("page", () => { popups++; });
  // The clipboard refuses, as in a browser that blocks it: a Copy button must still answer.
  await context.addInitScript(() => {
    if (navigator.clipboard) navigator.clipboard.writeText = () => Promise.reject(new Error("blocked"));
  });
  await context.route("**/*", (route) => {
    const url = new URL(route.request().url());
    if (route.request().isNavigationRequest()) navigations++;
    return url.host === new URL(base).host ? route.continue() : route.abort();
  });
  const page = await context.newPage();
  const open = async () => {
    await page.goto(base + path, { waitUntil: "load" });
    await page.evaluate(watch);
    navigations = 0;
    return page.evaluate(clickables);
  };
  // The same page lists its elements in the same order: after a reload, index i is the same one.
  const targets = await open();
  const failures = (await page.evaluate(fakePills)).map((name) => `pill looks like a button: ${name}`);
  for (const target of targets) {
    if (target.href && !/^https?:$/.test(new URL(target.href, page.url()).protocol)) continue;
    const element = page.locator(`[data-check-click="${target.i}"]`);
    // Centred (clear of the sticky bar) and settled first: the page's reveal-on-scroll classes
    // are no answer to the click.
    await element.evaluate((el) => el.scrollIntoView({ block: "center", inline: "center" })).catch(() => {});
    await page.waitForTimeout(SETTLE_MS);
    const url = page.url();
    const before = { popups, reactions: await page.evaluate(() => window.__reactions) };
    try {
      await element.click({ timeout: 2000 }).catch(() => element.click({ force: true, timeout: 2000 }));
    } catch (error) {
      failures.push(`cannot click <${target.tag}> "${target.name}": ${error.message.split("\n")[0]}`);
      continue;
    }
    clicked++;
    await page.waitForTimeout(SETTLE_MS);
    const moved = navigations > 0 || page.url() !== url || popups > before.popups;
    const reacted = moved || (await page.evaluate(() => window.__reactions).catch(() => 1)) > before.reactions;
    if (!reacted) failures.push(`click does nothing: <${target.tag}> "${target.name}"`);
    else await open();
  }
  await context.close();
  return failures.map((f) => `${path} @${width}px: ${f}`);
}

const server = await serve();
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch(executablePath ? { executablePath, args: ["--headless", "--no-sandbox"] } : {});
const failures = [];
try {
  for (const path of PAGES) for (const width of WIDTHS) failures.push(...(await check(browser, base, path, width)));
} finally {
  await browser.close();
  server.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  console.error(`${failures.length} element(s) look clickable and do nothing`);
  process.exit(1);
}
process.stdout.write(`site buttons OK: ${clicked} clicks on ${PAGES.join(", ")} at ${WIDTHS.join(" and ")} px\n`);
