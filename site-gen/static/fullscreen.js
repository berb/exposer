// D-11: the browser's own fullscreen mode, entered from a single photograph on
// its own page. This is the real Fullscreen API, not an in-page overlay —
// F-12's lightbox was withdrawn once every grid began navigating to a scoped
// page, and this is the only script the site carries.
//
// Progressive enhancement: the trigger ships hidden and is only revealed when
// the API is actually available, so nothing offers an action it cannot perform.
(function () {
  "use strict";

  var viewer = document.querySelector("[data-fullscreen-viewer]");
  if (!viewer) return; // not a photo page; D-11 only applies there

  var trigger = document.querySelector("[data-fullscreen-trigger]");
  var image = viewer.querySelector("img");
  var supported =
    document.fullscreenEnabled && typeof viewer.requestFullscreen === "function";
  if (!supported) {
    console.warn("exposer: fullscreen unavailable, leaving the trigger hidden");
    return;
  }

  if (trigger) trigger.hidden = false;

  // The page's own sizes hints describe the page layout, which is not the screen.
  // Remember them so leaving full screen restores exactly what the markup said.
  var candidates = viewer.querySelectorAll("source, img");
  var pageSizes = [];
  Array.prototype.forEach.call(candidates, function (node) {
    pageSizes.push(node.getAttribute("sizes"));
  });

  // Full screen contains the photograph, so its rendered width is limited by the
  // screen's width or by its height at this aspect ratio, whichever binds first.
  // Handing that to `sizes` lets the browser's own srcset rule do what D-11 asks:
  // take the first derivative at least that wide, or the widest one if none is.
  function applyScreenSizes() {
    var width = parseFloat(image && image.getAttribute("width")) || 0;
    var height = parseFloat(image && image.getAttribute("height")) || 0;
    var rendered = window.innerWidth;
    if (width > 0 && height > 0) {
      rendered = Math.min(window.innerWidth, window.innerHeight * (width / height));
    }
    var hint = Math.ceil(rendered) + "px";
    Array.prototype.forEach.call(candidates, function (node) {
      node.setAttribute("sizes", hint);
    });
  }

  function restorePageSizes() {
    Array.prototype.forEach.call(candidates, function (node, i) {
      if (pageSizes[i] === null) {
        node.removeAttribute("sizes");
      } else {
        node.setAttribute("sizes", pageSizes[i]);
      }
    });
  }

  function enter() {
    // Rejects if the gesture is not trusted or the browser refuses. Leave the
    // page as it was, but say why — swallowing this silently makes a failure
    // impossible to diagnose from the browser.
    var request = viewer.requestFullscreen({ navigationUI: "hide" });
    if (request && typeof request.catch === "function") {
      request.catch(function (error) {
        console.warn("exposer: fullscreen request refused:", error && error.message);
      });
    }
  }

  function toggle() {
    if (document.fullscreenElement === viewer) {
      document.exitFullscreen();
    } else {
      enter();
    }
  }

  // D-11 asks for a click on the photograph itself...
  if (image) {
    image.addEventListener("click", toggle);
    image.style.cursor = "zoom-in";
  }
  // ...and D-9 requires the same to be reachable without a pointer.
  if (trigger) trigger.addEventListener("click", toggle);

  document.addEventListener("fullscreenchange", function () {
    var active = document.fullscreenElement === viewer;
    if (active) {
      applyScreenSizes();
    } else {
      restorePageSizes();
    }
    if (trigger) {
      trigger.setAttribute("aria-pressed", active ? "true" : "false");
      trigger.textContent = active ? "Exit full screen" : "Full screen";
    }
    if (image) image.style.cursor = active ? "zoom-out" : "zoom-in";
  });

  // A rotated phone or a moved window changes which derivative is right.
  window.addEventListener("resize", function () {
    if (document.fullscreenElement === viewer) applyScreenSizes();
  });
})();
