/* Queues an artwork upload in IndexedDB and sends it when the network answers.
   The file is stored as the Blob the form already has; nothing is re-encoded. */
(function () {
  "use strict";

  function uploadID(bytes) {
    const view = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || []);
    let hex = "";
    for (let i = 0; i < view.length; i++) hex += view[i].toString(16).padStart(2, "0");
    return hex;
  }

  // 200/201 leave the queue and count as sent. 422 leaves too: the server
  // rejected the payload. Anything else, including a thrown fetch, stays.
  function disposition(status) {
    if (status === 200 || status === 201) return "sent";
    if (status === 422) return "drop";
    return "keep";
  }

  function sortQueue(items) {
    return items.slice().sort((a, b) => {
      const as = typeof a.seq === "number" ? a.seq : 0;
      const bs = typeof b.seq === "number" ? b.seq : 0;
      if (as !== bs) return as - bs;
      if (a.upload_id < b.upload_id) return -1;
      if (a.upload_id > b.upload_id) return 1;
      return 0;
    });
  }

  function recordFromFormData(fd, id, seq) {
    const fields = [];
    let file = null;
    for (const [name, value] of fd.entries()) {
      if (typeof value === "string") {
        fields.push({ name: name, value: value });
        continue;
      }
      file = { field: name, name: value.name || "image", type: value.type || "", blob: value };
    }
    let csrf = "";
    let lang = "";
    for (const field of fields) {
      if (field.name === "csrf") csrf = field.value;
      if (field.name === "lang") lang = field.value;
    }
    return { upload_id: id, seq: seq, csrf: csrf, lang: lang, fields: fields, file: file, url: "/artworks/new" };
  }

  function buildRequest(record) {
    const fd = new FormData();
    for (const field of record.fields || []) {
      if (field.name === "upload_id") continue;
      fd.append(field.name, field.value);
    }
    fd.append("upload_id", record.upload_id);
    if (record.file && record.file.blob) {
      fd.append(record.file.field || "image", record.file.blob, record.file.name || "image");
    }
    return {
      url: record.url || "/artworks/new",
      init: {
        method: "POST",
        credentials: "same-origin",
        headers: { Accept: "application/json", "X-CSRF-Token": record.csrf || "" },
        body: fd
      }
    };
  }

  function statusText(template, count, notice) {
    const parts = [];
    if (count > 0) parts.push(String(template == null ? "" : template).split("{n}").join(String(count)));
    if (notice) parts.push(String(notice));
    return parts.join(" ");
  }

  function flashPath(lang) {
    return "/?lang=" + encodeURIComponent(lang || "en") + "&flash=upload_success";
  }

  function paint(doc, count, message, isError) {
    const el = doc.getElementById("upload-queue");
    if (!el) return;
    const text = statusText(el.getAttribute("data-pending") || "", count, message || "");
    el.textContent = text;
    el.hidden = text === "";
    el.classList.toggle("error", Boolean(isError) && text !== "");
  }

  async function nextSeq(store) {
    const items = await store.list();
    let max = 0;
    for (const item of items) {
      if (typeof item.seq === "number" && item.seq > max) max = item.seq;
    }
    return max + 1;
  }

  async function errorText(res) {
    if (!res || typeof res.json !== "function") return "";
    try {
      const body = await res.json();
      if (body && typeof body.error === "string") return body.error;
    } catch (err) {
      return "";
    }
    return "";
  }

  function createQueue(deps) {
    const store = deps.store;
    let flight = null;

    async function sendOne(record) {
      const built = buildRequest(record);
      let res;
      try {
        res = await deps.fetch(built.url, built.init);
      } catch (err) {
        return { disposition: "keep", record: record };
      }
      const kind = disposition(res && res.status);
      let error = "";
      if (kind === "drop") error = await errorText(res);
      if (kind !== "keep") await store.delete(record.upload_id);
      return { disposition: kind, status: res ? res.status : 0, error: error, record: record };
    }

    async function run() {
      const dropped = [];
      let sent = 0;
      let lang = "en";
      for (;;) {
        const items = sortQueue(await store.list());
        if (items.length === 0) break;
        const result = await sendOne(items[0]);
        if (result.disposition === "keep") return { sent: sent, dropped: dropped, kept: true, lang: lang };
        if (result.disposition === "drop") dropped.push(result);
        if (result.disposition === "sent") {
          sent++;
          if (result.record && result.record.lang) lang = result.record.lang;
        }
      }
      return { sent: sent, dropped: dropped, kept: false, lang: lang };
    }

    function drain() {
      if (flight) return flight;
      flight = run().finally(() => { flight = null; });
      return flight;
    }

    async function submit(fd, bytes) {
      let record;
      try {
        const id = uploadID(bytes);
        record = recordFromFormData(fd, id, await nextSeq(store));
        await store.put(record);
      } catch (err) {
        const failure = new Error("queue-put");
        failure.cause = err;
        throw failure;
      }
      let result = await drain();
      const left = await store.list();
      if (result && !result.kept && left.some((item) => item.upload_id === record.upload_id)) {
        result = await drain();
      }
      return result;
    }

    return { drain: drain, submit: submit };
  }

  const DB_NAME = "pinacoteca-upload-queue";
  const STORE_NAME = "queue";

  function idbStore(idb) {
    let opening = null;
    function open() {
      if (opening) return opening;
      opening = new Promise((resolve, reject) => {
        const req = idb.open(DB_NAME, 1);
        req.onupgradeneeded = () => {
          const db = req.result;
          if (!db.objectStoreNames.contains(STORE_NAME)) db.createObjectStore(STORE_NAME, { keyPath: "upload_id" });
        };
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => { opening = null; reject(req.error); };
      });
      return opening;
    }
    function write(fn) {
      return open().then((db) => new Promise((resolve, reject) => {
        const tx = db.transaction(STORE_NAME, "readwrite");
        fn(tx.objectStore(STORE_NAME));
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error);
        tx.onabort = () => reject(tx.error || new Error("aborted"));
      }));
    }
    return {
      put(record) { return write((store) => { store.put(record); }); },
      delete(id) { return write((store) => { store.delete(id); }); },
      list() {
        return open().then((db) => new Promise((resolve, reject) => {
          const tx = db.transaction(STORE_NAME, "readonly");
          const req = tx.objectStore(STORE_NAME).getAll();
          let rows = [];
          req.onsuccess = () => { rows = req.result || []; };
          tx.oncomplete = () => resolve(rows);
          tx.onerror = () => reject(tx.error);
        }));
      }
    };
  }

  function boot(doc, win) {
    if (!win || !win.indexedDB || typeof win.fetch !== "function") return;
    const store = idbStore(win.indexedDB);
    const q = createQueue({ store: store, fetch: (url, init) => win.fetch(url, init) });
    async function refresh(result) {
      let items = [];
      try { items = await store.list(); } catch (err) { items = []; }
      const el = doc.getElementById("upload-queue");
      let message = "";
      let isError = false;
      if (result && result.kept) {
        message = el ? (el.getAttribute("data-kept") || "") : "";
      } else if (result && result.dropped && result.dropped.length) {
        message = result.dropped[result.dropped.length - 1].error || "";
        isError = message !== "";
      }
      paint(doc, items.length, message, isError);
      if (result && result.sent > 0 && items.length === 0 && !result.kept) {
        win.location.assign(flashPath(result.lang));
      }
    }
    function kick() {
      q.drain().then(refresh).catch(() => refresh({ kept: true, sent: 0, dropped: [] }));
    }
    const form = doc.querySelector('form[action="/artworks/new"]');
    if (form) {
      form.addEventListener("submit", (event) => {
        event.preventDefault();
        const bytes = new Uint8Array(16);
        if (win.crypto && typeof win.crypto.getRandomValues === "function") win.crypto.getRandomValues(bytes);
        q.submit(new FormData(form), bytes).then(refresh).catch((err) => {
          if (err && err.message === "queue-put") { form.submit(); return; }
          refresh({ kept: true, sent: 0, dropped: [] });
        });
      });
    }
    win.addEventListener("online", kick);
    kick();
  }

  const api = {
    uploadID: uploadID,
    disposition: disposition,
    sortQueue: sortQueue,
    recordFromFormData: recordFromFormData,
    buildRequest: buildRequest,
    statusText: statusText,
    flashPath: flashPath,
    createQueue: createQueue,
    idbStore: idbStore,
    boot: boot
  };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  if (typeof document !== "undefined") boot(document, window);
})();
