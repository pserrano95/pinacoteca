"use strict";
const { test } = require("node:test");
const assert = require("node:assert/strict");
const { clampScale, pinchScale, clampPan, swipeTarget, isDoubleTap } = require("../static/viewer.js");

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
