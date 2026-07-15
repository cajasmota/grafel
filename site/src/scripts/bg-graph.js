(function () {
  "use strict";

  var $ = function (id) { return document.getElementById(id); };
  var reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  /* ambient knowledge-graph background */
  var canvas = $("bg-graph"), ctx = canvas.getContext("2d");
  var nodes = [], W = 0, H = 0, dpr = Math.min(window.devicePixelRatio || 1, 2), scrollY = 0, raf = null;
  function accentRGB() {
    var c = getComputedStyle(document.documentElement).getPropertyValue("--accent").trim() || "#5FB0FF";
    var m = c.match(/^#?([0-9a-f]{6})$/i); if (!m) return [95,176,255];
    var n = parseInt(m[1], 16); return [(n>>16)&255,(n>>8)&255,n&255];
  }
  var RGB = accentRGB();
  function seed() {
    W = canvas.width = Math.floor(window.innerWidth * dpr);
    H = canvas.height = Math.floor(window.innerHeight * dpr);
    canvas.style.width = window.innerWidth + "px"; canvas.style.height = window.innerHeight + "px";
    var count = Math.max(24, Math.min(58, Math.floor(window.innerWidth / 28)));
    nodes = [];
    for (var i = 0; i < count; i++) nodes.push({ x: Math.random()*W, y: Math.random()*H, vx: (Math.random()-.5)*.13*dpr, vy: (Math.random()-.5)*.13*dpr, r: (Math.random()*1.5+.8)*dpr, depth: Math.random()*.7+.3 });
  }
  function draw() {
    ctx.clearRect(0, 0, W, H);
    var linkDist = 150 * dpr;
    for (var i = 0; i < nodes.length; i++) {
      var a = nodes[i]; a.x += a.vx; a.y += a.vy;
      if (a.x < 0 || a.x > W) a.vx *= -1; if (a.y < 0 || a.y > H) a.vy *= -1;
      var py = a.y + scrollY * a.depth * .07 * dpr;
      for (var j = i + 1; j < nodes.length; j++) {
        var b = nodes[j], dx = a.x-b.x, dy = a.y-b.y, d = Math.sqrt(dx*dx+dy*dy);
        if (d < linkDist) {
          var pby = b.y + scrollY * b.depth * .07 * dpr, o = (1 - d/linkDist) * .26;
          ctx.strokeStyle = "rgba(" + RGB[0]+","+RGB[1]+","+RGB[2]+","+o+")"; ctx.lineWidth = .6*dpr;
          ctx.beginPath(); ctx.moveTo(a.x, py); ctx.lineTo(b.x, pby); ctx.stroke();
        }
      }
      ctx.fillStyle = "rgba(" + RGB[0]+","+RGB[1]+","+RGB[2]+","+(.32*a.depth+.14)+")";
      ctx.beginPath(); ctx.arc(a.x, py, a.r, 0, Math.PI*2); ctx.fill();
    }
    raf = requestAnimationFrame(draw);
  }
  function startBg() { seed(); if (raf) cancelAnimationFrame(raf); if (reduce) { ctx.clearRect(0,0,W,H); draw(); cancelAnimationFrame(raf); draw(); } else draw(); }
  window.addEventListener("scroll", function () { scrollY = window.pageYOffset; }, { passive: true });
  var rt; window.addEventListener("resize", function () { clearTimeout(rt); rt = setTimeout(function () { RGB = accentRGB(); startBg(); }, 180); });
  new MutationObserver(function () { RGB = accentRGB(); }).observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  startBg();
})();
