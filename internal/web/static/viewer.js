/* The room links work with this file blocked. The gestures only follow them. */
(function () {
  "use strict";
  var MIN_SCALE = 1;
  var MAX_SCALE = 4;
  var SWIPE_PX = 48;
  var TAP_MS = 300;
  var TAP_PX = 24;

  function clampScale(scale) {
    if (scale < MIN_SCALE) return MIN_SCALE;
    if (scale > MAX_SCALE) return MAX_SCALE;
    return scale;
  }

  function pinchScale(startScale, startDistance, distance) {
    if (!(startDistance > 0) || !(distance > 0)) return clampScale(startScale);
    return clampScale(startScale * (distance / startDistance));
  }

  function clampPan(x, y, scale, width, height) {
    if (!(scale > 1) || !(width > 0) || !(height > 0)) return { x: 0, y: 0 };
    var maxX = (width * (scale - 1)) / 2;
    var maxY = (height * (scale - 1)) / 2;
    return {
      x: Math.min(maxX, Math.max(-maxX, x)),
      y: Math.min(maxY, Math.max(-maxY, y))
    };
  }

  // A leftward swipe (finger moves left) follows the next link.
  function swipeTarget(dx, dy, min) {
    if (Math.abs(dx) < min || Math.abs(dx) <= Math.abs(dy)) return "";
    return dx < 0 ? "next" : "prev";
  }

  function isDoubleTap(previous, now, maxGap, maxMove) {
    if (!previous || !now) return false;
    var dt = now.t - previous.t;
    if (dt <= 0 || dt > maxGap) return false;
    var dx = now.x - previous.x;
    var dy = now.y - previous.y;
    return Math.hypot(dx, dy) <= maxMove;
  }

  var api = {
    clampScale: clampScale,
    pinchScale: pinchScale,
    clampPan: clampPan,
    swipeTarget: swipeTarget,
    isDoubleTap: isDoubleTap
  };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  if (typeof document === "undefined") return;

  var root = document.querySelector(".room-viewer");
  var stage = document.querySelector(".viewer-stage");
  var img = stage && stage.querySelector("img");
  if (!root || !stage || !img) return;

  var scale = 1;
  var tx = 0;
  var ty = 0;
  var pointers = new Map();
  var gesture = null;
  var lastTap = null;

  function paint() {
    img.style.transform = "translate(" + tx + "px, " + ty + "px) scale(" + scale + ")";
  }

  function fit(nextScale, x, y) {
    var pan = clampPan(x, y, nextScale, img.offsetWidth, img.offsetHeight);
    scale = nextScale;
    tx = pan.x;
    ty = pan.y;
    paint();
  }

  function follow(which) {
    var link = root.querySelector(which === "next" ? "a.viewer-next" : "a.viewer-prev");
    if (!link) return;
    window.location.assign(link.href);
  }

  function points() {
    return Array.from(pointers.values());
  }

  function span(pts) {
    return Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
  }

  stage.addEventListener("pointerdown", function (e) {
    if (e.pointerType === "mouse" && e.button !== 0) return;
    try { stage.setPointerCapture(e.pointerId); } catch (err) { /* synthetic events have no capture */ }
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pointers.size >= 2) {
      var pts = points();
      gesture = { kind: "pinch", scale: scale, dist: span(pts), tx: tx, ty: ty };
      return;
    }
    gesture = { kind: scale > 1 ? "pan" : "swipe", id: e.pointerId, x: e.clientX, y: e.clientY, tx: tx, ty: ty };
  });

  stage.addEventListener("pointermove", function (e) {
    if (!pointers.has(e.pointerId)) return;
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (gesture && gesture.kind === "pinch" && pointers.size >= 2) {
      fit(pinchScale(gesture.scale, gesture.dist, span(points())), gesture.tx, gesture.ty);
      e.preventDefault();
      return;
    }
    if (gesture && gesture.kind === "pan" && gesture.id === e.pointerId) {
      fit(scale, gesture.tx + (e.clientX - gesture.x), gesture.ty + (e.clientY - gesture.y));
      e.preventDefault();
    }
  }, { passive: false });

  function endPointer(e) {
    pointers.delete(e.pointerId);
    if (!gesture) return;
    if (gesture.kind === "pinch") {
      if (pointers.size < 2) gesture = null;
      return;
    }
    if (gesture.id !== e.pointerId) return;
    var dx = e.clientX - gesture.x;
    var dy = e.clientY - gesture.y;
    if (gesture.kind === "swipe") {
      var dir = swipeTarget(dx, dy, SWIPE_PX);
      if (dir) follow(dir);
    }
    if (Math.hypot(dx, dy) <= TAP_PX) {
      var now = { t: e.timeStamp, x: e.clientX, y: e.clientY };
      if (isDoubleTap(lastTap, now, TAP_MS, TAP_PX)) {
        fit(1, 0, 0);
        lastTap = null;
      } else {
        lastTap = now;
      }
    }
    gesture = null;
  }

  stage.addEventListener("pointerup", endPointer);
  stage.addEventListener("pointercancel", function (e) {
    pointers.delete(e.pointerId);
    gesture = null;
  });

  document.addEventListener("keydown", function (e) {
    var tag = e.target && e.target.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || e.altKey || e.metaKey || e.ctrlKey) return;
    if (e.key === "ArrowLeft") {
      e.preventDefault();
      follow("prev");
    } else if (e.key === "ArrowRight") {
      e.preventDefault();
      follow("next");
    }
  });
})();
