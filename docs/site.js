// Shared by the home page and the full tour: language, top bar, menu, reveal on scroll.

// First visit in English: follow the browser's language when the site has it. Picking a
// language in the menu is remembered and wins from then on.
(function () {
  var languages = {
    fr: "fr",
    es: "es",
    uk: "uk",
    de: "de",
    it: "it",
    zh: "zh-Hans",
    ja: "ja",
    pt: "pt-BR",
    ru: "ru",
  };
  try {
    document.querySelectorAll(".langs a[hreflang]").forEach(function (link) {
      link.addEventListener("click", function () {
        localStorage.setItem("lang", link.hreflang);
      });
    });
    if (document.documentElement.lang !== "en" || localStorage.getItem("lang")) {
      return;
    }
    var wanted = (navigator.languages || [navigator.language || ""])
      .map(function (tag) {
        return tag.toLowerCase().split("-")[0];
      })
      .filter(function (code) {
        return code === "en" || languages[code];
      })[0];
    if (wanted && wanted !== "en") {
      var page = document.body.hasAttribute("data-home") ? "" : location.pathname.split("/").pop();
      location.replace(languages[wanted] + "/" + page + location.hash);
    }
  } catch (e) {}
})();

// The detailed sections live on features.html: a link to one of them on the home page, which
// is what shipped apps and older links use (#argocd, #cronjobs, #beta…), lands there.
(function () {
  if (!document.body.hasAttribute("data-home")) return;
  var target = decodeURIComponent(location.hash.slice(1));
  if (target && !document.getElementById(target)) {
    location.replace("features.html" + location.hash);
  }
})();

// The bar is see-through over the page's navy header, solid past it.
(function () {
  var bar = document.getElementById("bar");
  var head = document.querySelector(".hero, .page-head");
  new IntersectionObserver(
    function (entries) {
      bar.classList.toggle("solid", !entries[0].isIntersecting);
    },
    { rootMargin: "-64px 0px 0px" },
  ).observe(head);
})();

(function () {
  var button = document.getElementById("menu-button");
  function setMenu(open) {
    document.body.classList.toggle("menu-open", open);
    button.setAttribute("aria-expanded", String(open));
  }
  button.addEventListener("click", function () {
    setMenu(!document.body.classList.contains("menu-open"));
  });
  document.querySelectorAll("#menu a").forEach(function (link) {
    link.addEventListener("click", function () {
      setMenu(false);
    });
  });
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") setMenu(false);
  });
})();

(function () {
  var reveal = new IntersectionObserver(
    function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          entry.target.classList.add("seen");
          reveal.unobserve(entry.target);
        }
      });
    },
    { threshold: 0.15 },
  );
  document.querySelectorAll(".reveal").forEach(function (element) {
    reveal.observe(element);
  });
})();
