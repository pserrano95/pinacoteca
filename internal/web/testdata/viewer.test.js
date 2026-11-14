"use strict";
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const { clampScale, pinchScale, clampPan, swipeTarget, isDoubleTap } = require("../static/viewer.js");

function classList() {
  const tokens = new Set();
  return {
    add(token) {
      tokens.add(String(token));
    },
    contains(token) {
      return tokens.has(String(token));
    },
  };
}

// The mark is a side effect of loading the script, so each case runs the file
// on its own document. The page is a viewer when it has the room, the stage
// and the drawing; the second case drops only the stage.
function viewerDocument(withStage) {
  const img = { style: {} };
  const stage = {
    addEventListener() {},
    querySelector(sel) {
      return sel === "img" ? img : null;
    },
  };
  const document = {
    documentElement: { classList: classList() },
    addEventListener() {},
    querySelector(sel) {
      if (sel === ".room-viewer") return {};
      if (sel === ".viewer-stage" && withStage) return stage;
      return null;
    },
  };
  return document;
}

function loadViewer(document) {
  const source = fs.readFileSync(path.join(__dirname, "../static/viewer.js"), "utf8");
  vm.runInNewContext(source, { document, Map }, { filename: "viewer.js" });
}

test("scale stays between 1 and 4", () => {
  assert.equal(clampScale(0.2), 1);
  assert.equal(clampScale(1), 1);
  assert.equal(clampScale(2.5), 2.5);
  assert.equal(clampScale(9), 4);
});

test("pinch scales with the finger distance and clamps", () => {
  assert.equal(pinchScale(1, 100, 200), 2);
  assert.equal(pinchScale(2, 100, 300), 4);
  assert.equal(pinchScale(1, 100, 40), 1);
  assert.equal(pinchScale(1, 0, 10), 1);
});

test("pan is idle at 1x and stays inside the scaled drawing", () => {
  assert.deepEqual(clampPan(30, -20, 1, 200, 100), { x: 0, y: 0 });
  assert.deepEqual(clampPan(1000, -1000, 3, 200, 100), { x: 200, y: -100 });
});

test("horizontal swipe picks the neighbor link", () => {
  assert.equal(swipeTarget(-50, 10, 40), "next");
  assert.equal(swipeTarget(50, 10, 40), "prev");
  assert.equal(swipeTarget(-10, 0, 40), "");
  assert.equal(swipeTarget(-50, 80, 40), "");
});

test("double tap is two close taps inside the window", () => {
  assert.equal(isDoubleTap(null, { t: 1000, x: 0, y: 0 }, 300, 24), false);
  assert.equal(isDoubleTap({ t: 800, x: 0, y: 0 }, { t: 1000, x: 10, y: 0 }, 300, 24), true);
  assert.equal(isDoubleTap({ t: 100, x: 0, y: 0 }, { t: 1000, x: 0, y: 0 }, 300, 24), false);
  assert.equal(isDoubleTap({ t: 900, x: 0, y: 0 }, { t: 1000, x: 40, y: 0 }, 300, 24), false);
});

test("loading the script on a viewer marks the page viewer-ready", () => {
  const document = viewerDocument(true);
  loadViewer(document);
  assert.equal(document.documentElement.classList.contains("viewer-ready"), true);
});

test("without a viewer stage the page is not viewer-ready", () => {
  const document = viewerDocument(false);
  loadViewer(document);
  assert.equal(document.documentElement.classList.contains("viewer-ready"), false);
});
