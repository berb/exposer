// The only script the site carries: D-11's fullscreen view and F-25's arrow
// keys, both on a photograph's page and neither needed to use the page.
//
// D-11 is the browser's own fullscreen mode, entered from a single photograph
// on its own page — the real Fullscreen API, not an in-page overlay. F-12's
// lightbox was withdrawn once every grid began navigating to a scoped page.
//
// Progressive enhancement throughout: the trigger ships hidden and is only
// revealed when the API is actually available, so nothing offers an action it
// cannot perform, and the arrow keys only follow links that are on the page
// already, reachable with Tab and Enter without any of this.

// F-25: left and right follow the neighbours of a scoped page, which carries
// them as rel="prev" and rel="next". A photograph's own page (F-11) has no
// pager, and the ends of a listing have only one neighbour, so this finds
// nothing to follow there and leaves the key to the browser.
(function () {
  "use strict";

  var steps = {
    ArrowLeft: document.querySelector(".pager a[rel='prev']"),
    ArrowRight: document.querySelector(".pager a[rel='next']"),
  };
  if (!steps.ArrowLeft && !steps.ArrowRight) return;

  document.addEventListener("keydown", function (event) {
    if (event.defaultPrevented || event.ctrlKey || event.altKey ||
        event.metaKey || event.shiftKey) {
      return; // Alt+Left is the browser's history, not ours
    }
    // Nothing on the site takes typed input today, but a page that did would
    // have to keep its arrows.
    var focused = document.activeElement;
    if (focused && (focused.isContentEditable ||
        /^(input|textarea|select)$/i.test(focused.tagName))) {
      return;
    }
    var step = steps[event.key];
    if (!step) return;
    // Only now: on a page with one neighbour the other arrow still scrolls.
    event.preventDefault();
    step.click();
  });
})();

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

  if (trigger) {
    trigger.hidden = false;
  } else {
    // A scoped page shows no button -- its pager already fills that rail -- so
    // the photograph is the control (D-9): focusable, named, and answering the
    // keys a button answers. This is set only once the API is known to work,
    // so nothing offers an action it cannot perform.
    viewer.setAttribute("tabindex", "0");
    viewer.setAttribute("role", "button");
    viewer.setAttribute("aria-pressed", "false");
    viewer.setAttribute("aria-label", "Show the photograph full screen");
    viewer.addEventListener("keydown", function (event) {
      if (event.key !== "Enter" && event.key !== " ") return;
      event.preventDefault(); // Space would scroll the page
      toggle();
    });
  }

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
    } else {
      viewer.setAttribute("aria-pressed", active ? "true" : "false");
      viewer.setAttribute("aria-label",
        active ? "Leave full screen" : "Show the photograph full screen");
    }
    if (image) image.style.cursor = active ? "zoom-out" : "zoom-in";
  });

  // A rotated phone or a moved window changes which derivative is right.
  window.addEventListener("resize", function () {
    if (document.fullscreenElement === viewer) applyScreenSizes();
  });
})();
