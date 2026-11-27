// The "Copy" buttons of the site (features.html's donation addresses, parts/'s part numbers):
// a button with data-copy="<id>" copies that element's text. It always answers: its
// data-copied text once copied, or, when the browser has no Clipboard API or refuses it, the
// text selected and its data-copy-failed text, so the visitor copies it themselves. Each
// label shows for 3 seconds; aria-live reads it out.
(function () {
  "use strict";

  var SHOW_MS = 3000;

  function select(element) {
    var range = document.createRange();
    range.selectNodeContents(element);
    var selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
  }

  function setUp(button) {
    var label = button.textContent;
    var timer = 0;
    button.setAttribute("aria-live", "polite");

    function show(text) {
      clearTimeout(timer);
      button.textContent = text;
      timer = setTimeout(function () {
        button.textContent = label;
      }, SHOW_MS);
    }

    button.addEventListener("click", function () {
      var target = document.getElementById(button.dataset.copy);
      if (!target) return;
      var failed = function () {
        select(target);
        show(button.dataset.copyFailed || "Selected: copy it");
      };
      var text = target.textContent.trim();
      if (!navigator.clipboard || !navigator.clipboard.writeText) return failed();
      navigator.clipboard.writeText(text).then(function () {
        show(button.dataset.copied || "Copied");
      }, failed);
    });
  }

  document.querySelectorAll("[data-copy]").forEach(setUp);
})();
