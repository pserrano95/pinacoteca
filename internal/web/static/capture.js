/* Progressive enhancement: the file input always submits the untouched file. */
(function () {
  "use strict";
  const MIN = 0.05;
  const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));
  function fitRect(r, minimum = MIN) {
    const w = clamp(r.w, minimum, 1), h = clamp(r.h, minimum, 1);
    return { x: clamp(r.x, 0, 1 - w), y: clamp(r.y, 0, 1 - h), w, h };
  }
  function dimensions(w, h, turns) {
    return turns % 2 ? { w: h, h: w } : { w, h };
  }
  function point(clientX, clientY, bounds) {
    return { x: (clientX - bounds.left) / bounds.width, y: (clientY - bounds.top) / bounds.height };
  }
  function resizeRect(r, corner, p) {
    const left = corner.includes("w") ? clamp(p.x, 0, r.x + r.w - MIN) : r.x;
    const top = corner.includes("n") ? clamp(p.y, 0, r.y + r.h - MIN) : r.y;
    const right = corner.includes("e") ? clamp(p.x, r.x + MIN, 1) : r.x + r.w;
    const bottom = corner.includes("s") ? clamp(p.y, r.y + MIN, 1) : r.y + r.h;
    return fitRect({ x: left, y: top, w: right - left, h: bottom - top });
  }
  const geometry = { fitRect, dimensions, point, resizeRect };
  if (typeof module !== "undefined") module.exports = geometry;
  if (typeof document === "undefined") return;
  const editor = document.getElementById("capture-editor");
  if (!editor || !window.createImageBitmap) return;
  const form = editor.closest("form"), input = form.elements.image;
  const canvas = editor.querySelector("canvas"), ctx = canvas.getContext("2d");
  const slider = document.getElementById("capture-straighten");
  const error = document.getElementById("capture-error");
  let bitmap, turns = 0, rect = { x: 0, y: 0, w: 1, h: 1 }, drag, generation = 0;
  const full = () => ({ x: 0, y: 0, w: 1, h: 1 });
  function clearFields() {
    for (const key of ["quarter_turns", "straighten_deg", "x", "y", "w", "h"]) form.elements["crop_" + key].value = "";
  }
  function draw() {
    if (!bitmap) return;
    const size = dimensions(bitmap.width, bitmap.height, turns);
    const scale = Math.min(1, 1200 / Math.max(size.w, size.h));
    canvas.width = Math.max(1, Math.round(size.w * scale));
    canvas.height = Math.max(1, Math.round(size.h * scale));
    ctx.fillStyle = "white";
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.save();
    ctx.translate(canvas.width / 2, canvas.height / 2);
    // Same-size oriented canvas: quarter turns, then straighten, then crop.
    ctx.rotate((turns * 90 + Number(slider.value)) * Math.PI / 180);
    ctx.drawImage(bitmap, -bitmap.width * scale / 2, -bitmap.height * scale / 2, bitmap.width * scale, bitmap.height * scale);
    ctx.restore();
    const x = rect.x * canvas.width, y = rect.y * canvas.height;
    const w = rect.w * canvas.width, h = rect.h * canvas.height;
    ctx.fillStyle = "rgba(0,0,0,.4)";
    ctx.beginPath();
    ctx.rect(0, 0, canvas.width, canvas.height);
    ctx.rect(x, y, w, h);
    ctx.fill("evenodd");
    ctx.strokeStyle = "white";
    ctx.lineWidth = 3;
    ctx.strokeRect(x, y, w, h);
    const handle = 18 * canvas.width / canvas.getBoundingClientRect().width;
    for (const [hx, hy] of [[x,y],[x+w,y],[x,y+h],[x+w,y+h]]) {
      ctx.fillStyle = "white"; ctx.fillRect(hx-handle/2, hy-handle/2, handle, handle);
      ctx.strokeStyle = "#222"; ctx.lineWidth = 1; ctx.strokeRect(hx-handle/2, hy-handle/2, handle, handle);
    }
    form.elements.crop_quarter_turns.value = turns;
    form.elements.crop_straighten_deg.value = slider.value;
    for (const key of ["x", "y", "w", "h"]) form.elements["crop_" + key].value = rect[key];
    document.getElementById("capture-angle").value = slider.value + "°";
  }
  input.addEventListener("change", async () => {
    const ticket = ++generation;
    clearFields(); editor.hidden = true; error.hidden = true; drag = null;
    if (bitmap) { bitmap.close(); bitmap = null; }
    if (!input.files[0]) return;
    try {
      // Browsers decode EXIF before exposing bitmap dimensions and pixels.
      const loaded = await createImageBitmap(input.files[0], { imageOrientation: "from-image" });
      if (ticket !== generation) { loaded.close(); return; }
      bitmap = loaded; turns = 0; slider.value = 0; rect = full();
      editor.hidden = false; draw();
    } catch (_) { if (ticket === generation) error.hidden = false; }
  });
  document.getElementById("capture-rotate").addEventListener("click", () => { turns = (turns + 1) % 4; rect = full(); draw(); });
  slider.addEventListener("input", draw);
  document.getElementById("capture-reset").addEventListener("click", () => { turns = 0; slider.value = 0; rect = full(); draw(); });
  canvas.addEventListener("pointerdown", e => {
    if (drag || !bitmap) return;
    const bounds = canvas.getBoundingClientRect(), p = point(e.clientX, e.clientY, bounds);
    let corner = "";
    for (const [name, x, y] of [["nw",rect.x,rect.y],["ne",rect.x+rect.w,rect.y],["sw",rect.x,rect.y+rect.h],["se",rect.x+rect.w,rect.y+rect.h]]) {
      if (Math.abs(p.x-x)*bounds.width <= 24 && Math.abs(p.y-y)*bounds.height <= 24) { corner = name; break; }
    }
    if (!corner && (p.x<rect.x || p.x>rect.x+rect.w || p.y<rect.y || p.y>rect.y+rect.h)) return;
    drag = { id: e.pointerId, p, rect: { ...rect }, corner };
    canvas.setPointerCapture(e.pointerId); e.preventDefault();
  });
  canvas.addEventListener("pointermove", e => {
    if (!drag || drag.id !== e.pointerId) return;
    const p = point(e.clientX, e.clientY, canvas.getBoundingClientRect());
    rect = drag.corner ? resizeRect(drag.rect, drag.corner, p) : fitRect({ ...drag.rect, x: drag.rect.x+p.x-drag.p.x, y: drag.rect.y+p.y-drag.p.y });
    draw();
  });
  for (const event of ["pointerup", "pointercancel", "lostpointercapture"]) canvas.addEventListener(event, () => { drag = null; });
})();
