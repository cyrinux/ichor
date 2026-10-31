// Shared by the home page and the full tour: language, top bar, menu, reveal on scroll,
// screenshots' platform.

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

// The screenshots show the Android app or the iPhone app. The pages hold the Android files, what
// a visitor without JavaScript sees; an Apple device starts on the iPhone ones, and a choice made
// with the switch is remembered. A picture with no iPhone file stays on Android.
(function () {
  var pictures = document.querySelectorAll("picture[data-shot]");
  var buttons = document.querySelectorAll(".platform [data-platform]");

  function setUrl(element, name, platform) {
    var folder = platform === "ios" ? "screenshots/ios/" : "screenshots/";
    var url = element.getAttribute(name).replace(/screenshots\/(ios\/)?/, folder);
    if (url !== element.getAttribute(name)) element.setAttribute(name, url);
  }

  function show(picture, platform) {
    picture.querySelectorAll("source").forEach(function (source) {
      setUrl(source, "srcset", platform);
    });
    setUrl(picture.querySelector("img"), "src", platform);
  }

  function apply(platform) {
    document.documentElement.setAttribute("data-platform", platform);
    pictures.forEach(function (picture) {
      show(picture, platform);
    });
    buttons.forEach(function (button) {
      button.setAttribute("aria-pressed", String(button.dataset.platform === platform));
    });
  }

  function initial() {
    var stored = null;
    try {
      stored = localStorage.getItem("platform");
    } catch (e) {}
    if (stored === "ios" || stored === "android") return stored;
    return /iPhone|iPad|Macintosh/.test(navigator.userAgent) ? "ios" : "android";
  }

  document.documentElement.classList.add("js");
  pictures.forEach(function (picture) {
    var image = picture.querySelector("img");
    image.addEventListener("error", function () {
      if (image.getAttribute("src").indexOf("screenshots/ios/") !== -1) show(picture, "android");
    });
  });
  buttons.forEach(function (button) {
    button.addEventListener("click", function () {
      try {
        localStorage.setItem("platform", button.dataset.platform);
      } catch (e) {}
      apply(button.dataset.platform);
    });
  });
  apply(initial());
})();
