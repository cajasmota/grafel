(function () {
  "use strict";

  var $ = function (id) { return document.getElementById(id); };

  /* install tabs */
  (function installTabs() {
    var widget = $("install-widget");
    if (!widget) return;
    var tabs = Array.prototype.slice.call(widget.querySelectorAll(".install-tab"));
    var panels = Array.prototype.slice.call(widget.querySelectorAll(".install-panel"));
    function showTab(key) {
      tabs.forEach(function (t) { t.setAttribute("aria-selected", t.dataset.tab === key ? "true" : "false"); });
      panels.forEach(function (p) { p.hidden = p.dataset.panel !== key; });
    }
    tabs.forEach(function (t, i) {
      t.addEventListener("click", function () { showTab(t.dataset.tab); });
      t.addEventListener("keydown", function (e) {
        if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
        e.preventDefault();
        var next = tabs[(i + (e.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length];
        next.focus(); showTab(next.dataset.tab);
      });
    });
  })();

  /* scroll reveal */
  var reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (!reduce && "IntersectionObserver" in window) {
    var io = new IntersectionObserver(function (ents) {
      ents.forEach(function (e) { if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); } });
    }, { threshold: 0.1 });
    document.querySelectorAll(".reveal").forEach(function (el) { io.observe(el); });
  } else {
    document.querySelectorAll(".reveal").forEach(function (el) { el.classList.add("in"); });
  }
})();
