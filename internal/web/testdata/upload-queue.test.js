"use strict";
const { test } = require("node:test");
const assert = require("node:assert/strict");
const {
  uploadID, disposition, sortQueue, recordFromFormData, buildRequest,
  statusText, flashPath, createQueue
} = require("../static/upload-queue.js");

function memoryStore(initial) {
  const rows = (initial || []).map((row) => Object.assign({}, row));
  return {
    async put(record) {
      const i = rows.findIndex((row) => row.upload_id === record.upload_id);
      if (i >= 0) rows[i] = record;
      else rows.push(record);
    },
    async list() { return rows.map((row) => Object.assign({}, row)); },
    async delete(id) {
      const i = rows.findIndex((row) => row.upload_id === id);
      if (i >= 0) rows.splice(i, 1);
    }
  };
}

function queued(id, seq) {
  return {
    upload_id: id,
    seq: seq,
    csrf: "token-" + id,
    lang: "es",
    fields: [
      { name: "csrf", value: "token-" + id },
      { name: "lang", value: "es" },
      { name: "title", value: "Sun" }
    ],
    file: { field: "image", name: "sun.png", type: "image/png", blob: new Blob(["img-" + id]) },
    url: "/artworks/new"
  };
}

function jsonResponse(status, body) {
  return { status: status, async json() { return body || {}; } };
}

test("disposition keeps retries and drops only a rejection", () => {
  assert.equal(disposition(201), "sent");
  assert.equal(disposition(200), "sent");
  assert.equal(disposition(422), "drop");
  for (const status of [0, 401, 403, 404, 500, 503]) assert.equal(disposition(status), "keep");
});

test("pending text fills the count", () => {
  assert.equal(statusText("{n} subidas pendientes", 3, ""), "3 subidas pendientes");
  assert.equal(statusText("{n} uploads pending", 1, "still queued"), "1 uploads pending still queued");
  assert.equal(statusText("{n} uploads pending", 0, ""), "");
  assert.equal(flashPath("es"), "/?lang=es&flash=upload_success");
});

test("offline stays queued and the file was stored before fetch", async () => {
  const store = memoryStore();
  let seen = -1;
  const q = createQueue({
    store: store,
    fetch: async () => {
      seen = (await store.list()).length;
      throw new Error("offline");
    }
  });
  const fd = new FormData();
  const blob = new Blob(["pixels"], { type: "image/png" });
  fd.append("csrf", "csrf-1");
  fd.append("lang", "es");
  fd.append("title", "Sun");
  fd.append("image", blob, "sun.png");
  const bytes = new Uint8Array(16);
  bytes[0] = 1;
  const result = await q.submit(fd, bytes);
  assert.equal(seen, 1);
  assert.equal(result.kept, true);
  const left = await store.list();
  assert.equal(left.length, 1);
  assert.equal(left[0].upload_id, uploadID(bytes));
  assert.equal(left[0].file.blob.size, blob.size);
  assert.equal(await left[0].file.blob.text(), "pixels");
});

test("a queued upload is sent and deleted when the network returns", async () => {
  const id = "ab".repeat(16);
  const store = memoryStore([queued(id, 1)]);
  let calls = 0;
  const q = createQueue({
    store: store,
    fetch: async () => {
      calls++;
      if (calls === 1) throw new Error("offline");
      return jsonResponse(201, { artist: "lucia", folder: "f" });
    }
  });
  const first = await q.drain();
  assert.equal(first.kept, true);
  assert.equal((await store.list()).length, 1);
  const second = await q.drain();
  assert.equal(second.sent, 1);
  assert.equal(second.kept, false);
  assert.equal((await store.list()).length, 0);
});

test("422 is deleted, 401 403 and 503 stay", async () => {
  const dropped = memoryStore([queued("drop", 1)]);
  const qDrop = createQueue({
    store: dropped,
    fetch: async () => jsonResponse(422, { error: "Fecha no válida" })
  });
  const drop = await qDrop.drain();
  assert.equal(drop.kept, false);
  assert.equal(drop.dropped[0].error, "Fecha no válida");
  assert.equal((await dropped.list()).length, 0);

  for (const status of [401, 403, 503]) {
    const store = memoryStore([queued("keep-" + status, 1)]);
    const q = createQueue({
      store: store,
      fetch: async () => jsonResponse(status, {})
    });
    const result = await q.drain();
    assert.equal(result.kept, true, "status " + status);
    assert.equal((await store.list()).length, 1, "status " + status);
  }
});

test("three queued uploads send in order", async () => {
  const store = memoryStore([queued("c", 3), queued("a", 1), queued("b", 2)]);
  const order = [];
  const q = createQueue({
    store: store,
    fetch: async (url, init) => {
      order.push(init.body.get("upload_id"));
      return jsonResponse(201, {});
    }
  });
  const result = await q.drain();
  assert.deepEqual(order, ["a", "b", "c"]);
  assert.equal(result.sent, 3);
  assert.equal((await store.list()).length, 0);
});

test("two drains at once send each upload once", async () => {
  const store = memoryStore([queued("a", 1), queued("b", 2), queued("c", 3)]);
  const order = [];
  const q = createQueue({
    store: store,
    fetch: async (url, init) => {
      order.push(init.body.get("upload_id"));
      await new Promise((resolve) => setTimeout(resolve, 20));
      return jsonResponse(201, {});
    }
  });
  await Promise.all([q.drain(), q.drain()]);
  assert.deepEqual(order, ["a", "b", "c"]);
});

test("retries send the same upload id and csrf header", async () => {
  const id = "cd".repeat(16);
  const store = memoryStore([queued(id, 1)]);
  const seen = [];
  let calls = 0;
  const q = createQueue({
    store: store,
    fetch: async (url, init) => {
      calls++;
      const image = init.body.get("image");
      seen.push({
        csrf: init.headers["X-CSRF-Token"],
        accept: init.headers.Accept,
        contentType: init.headers["Content-Type"],
        ids: init.body.getAll("upload_id"),
        image: await image.text()
      });
      if (calls === 1) return jsonResponse(503, {});
      return jsonResponse(200, { artist: "lucia", folder: "sun" });
    }
  });
  const first = await q.drain();
  assert.equal(first.kept, true);
  assert.equal((await store.list()).length, 1);
  const second = await q.drain();
  assert.equal(second.sent, 1);
  assert.equal(seen.length, 2);
  for (const attempt of seen) {
    assert.equal(attempt.csrf, "token-" + id);
    assert.equal(attempt.accept, "application/json");
    assert.equal(attempt.contentType, undefined);
    assert.deepEqual(attempt.ids, [id]);
    assert.equal(attempt.image, "img-" + id);
  }
});

test("the stored file is the blob from the form", () => {
  const fd = new FormData();
  const blob = new Blob(["same-bytes"], { type: "image/png" });
  fd.append("csrf", "tok");
  fd.append("image", blob, "drawing.png");
  const rec = recordFromFormData(fd, "ab".repeat(16), 1);
  assert.equal(rec.file.blob.size, blob.size);
  assert.equal(rec.csrf, "tok");
  const built = buildRequest(rec);
  assert.equal(built.init.headers["X-CSRF-Token"], "tok");
  assert.deepEqual(sortQueue([{ upload_id: "b", seq: 2 }, { upload_id: "a", seq: 1 }]).map((item) => item.upload_id), ["a", "b"]);
});
