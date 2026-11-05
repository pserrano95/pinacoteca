"use strict";
const { test } = require("node:test");
const assert = require("node:assert/strict");
const { fitRect, dimensions, point, resizeRect } = require("../static/capture.js");
for (const [edge, r, expected] of [
  ["left", {x:-.2,y:.1,w:.5,h:.5}, {x:0,y:.1,w:.5,h:.5}],
  ["top", {x:.1,y:-.2,w:.5,h:.5}, {x:.1,y:0,w:.5,h:.5}],
  ["right", {x:.9,y:.1,w:.5,h:.5}, {x:.5,y:.1,w:.5,h:.5}],
  ["bottom", {x:.1,y:.9,w:.5,h:.5}, {x:.1,y:.5,w:.5,h:.5}],
  ["minimum", {x:0,y:0,w:0,h:-1}, {x:0,y:0,w:.05,h:.05}]
]) test("fit " + edge, () => assert.deepEqual(fitRect(r), expected));
test("rotated dimensions and pointer conversion", () => {
  assert.deepEqual(dimensions(800,600,1),{w:600,h:800});
  assert.deepEqual(dimensions(800,600,2),{w:800,h:600});
  assert.deepEqual(dimensions(800,600,3),{w:600,h:800});
  assert.deepEqual(point(160,220,{left:10,top:20,width:300,height:400}),{x:.5,y:.5});
});
test("each resize corner stays inside image", () => {
  for (const corner of ["nw","ne","sw","se"]) {
    const r=resizeRect({x:.2,y:.2,w:.6,h:.6},corner,{x:-2,y:3});
    assert.ok(r.x>=0 && r.y>=0 && r.x+r.w<=1 && r.y+r.h<=1);
    assert.ok(r.w>=.05 && r.h>=.05);
  }
});
test("minimum resize cannot invert", () => {
 const r=resizeRect({x:0,y:0,w:1,h:1},"se",{x:-10,y:-10});
 assert.equal(r.w,.05);assert.equal(r.h,.05);
});
