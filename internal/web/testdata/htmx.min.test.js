// Behavior of the vendored htmx 2.0.4 script (internal/web/static/htmx.min.js).
// Node's built-in test runner executes this file; internal/web/htmx_static_test.go
// invokes it so `go test ./...` is the command that already runs in CI.
// The script is a browser IIFE, so the suite installs the host objects it
// touches and then calls the public functions. No extra test dependency.

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { describe, before, beforeEach, test } = require("node:test");
const vm = require("node:vm");

let htmx;
let document;
let logs;
let XMLHttpRequest;

function installBrowser() {
  const logs = [];
  let docRef = null;

  class Node {}
  Node.ELEMENT_NODE = 1;
  Node.TEXT_NODE = 3;
  Node.COMMENT_NODE = 8;
  Node.DOCUMENT_POSITION_PRECEDING = 2;
  Node.DOCUMENT_POSITION_FOLLOWING = 4;

  class ClassList {
    constructor(el) {
      this.el = el;
      this.tokens = new Set();
    }
    get length() {
      return this.tokens.size;
    }
    add(token) {
      if (token == null || token === "") return;
      this.tokens.add(String(token));
      this._sync();
    }
    remove(token) {
      this.tokens.delete(String(token));
      this._sync();
    }
    toggle(token) {
      token = String(token);
      if (this.tokens.has(token)) this.tokens.delete(token);
      else this.tokens.add(token);
      this._sync();
      return this.tokens.has(token);
    }
    contains(token) {
      return this.tokens.has(String(token));
    }
    _replace(value) {
      this.tokens = new Set(String(value).split(/\s+/).filter(Boolean));
      this._sync();
    }
    _sync() {
      if (this.tokens.size === 0) delete this.el._attr.class;
      else this.el._attr.class = [...this.tokens].join(" ");
      this.el._rebuild();
    }
  }

  class Element {
    constructor(tag) {
      this.tagName = String(tag || "div").toUpperCase();
      this._attr = {};
      this.attributes = [];
      this.children = [];
      this.childNodes = [];
      this.parentElement = null;
      this.parentNode = null;
      this.id = "";
      this.name = "";
      this.value = "";
      this.type = "";
      this.disabled = false;
      this.textContent = "";
      this.classList = new ClassList(this);
      this._listeners = {};
    }
    _rebuild() {
      this.attributes = Object.keys(this._attr).map((name) => ({
        name,
        value: this._attr[name],
      }));
    }
    getAttribute(name) {
      return Object.prototype.hasOwnProperty.call(this._attr, name) ? this._attr[name] : null;
    }
    hasAttribute(name) {
      return Object.prototype.hasOwnProperty.call(this._attr, name);
    }
    setAttribute(name, value) {
      this._attr[name] = String(value);
      if (name === "id") this.id = String(value);
      if (name === "name") this.name = String(value);
      if (name === "class") this.classList._replace(String(value));
      else this._rebuild();
    }
    removeAttribute(name) {
      delete this._attr[name];
      if (name === "id") this.id = "";
      if (name === "name") this.name = "";
      if (name === "class") this.classList.tokens = new Set();
      this._rebuild();
    }
    matches(selector) {
      return String(selector)
        .split(",")
        .map((part) => part.trim())
        .filter(Boolean)
        .some((part) => this._matchesOne(part));
    }
    _matchesOne(sel) {
      if (sel.startsWith("#")) return this.id === sel.slice(1);
      if (sel.startsWith(".")) return this.classList.contains(sel.slice(1));
      if (sel.startsWith("[") && sel.endsWith("]")) {
        const body = sel.slice(1, -1);
        const eq = body.indexOf("=");
        if (eq === -1) return this.hasAttribute(body.trim());
        const name = body.slice(0, eq).trim();
        let val = body.slice(eq + 1).trim();
        if (
          (val.startsWith('"') && val.endsWith('"')) ||
          (val.startsWith("'") && val.endsWith("'"))
        ) {
          val = val.slice(1, -1);
        }
        return this.getAttribute(name) === val;
      }
      if (/^[a-zA-Z][\w-]*$/.test(sel)) return this.tagName === sel.toUpperCase();
      return false;
    }
    closest(selector) {
      let node = this;
      while (node) {
        if (node instanceof Element && node.matches(selector)) return node;
        node = node.parentElement;
      }
      return null;
    }
    querySelector(selector) {
      const found = this.querySelectorAll(selector);
      return found[0] || null;
    }
    querySelectorAll(selector) {
      return queryAll(this, selector);
    }
    addEventListener(type, fn) {
      listen(this, type, fn);
    }
    removeEventListener(type, fn) {
      forget(this, type, fn);
    }
    dispatchEvent(event) {
      return emit(this, event);
    }
    appendChild(child) {
      if (child.parentElement && child.parentElement !== this) {
        child.parentElement.removeChild(child);
      }
      child.parentElement = this;
      child.parentNode = this;
      if (!this.childNodes.includes(child)) this.childNodes.push(child);
      if (child instanceof Element && !this.children.includes(child)) this.children.push(child);
      return child;
    }
    removeChild(child) {
      this.childNodes = this.childNodes.filter((node) => node !== child);
      this.children = this.children.filter((node) => node !== child);
      if (child.parentElement === this) child.parentElement = null;
      if (child.parentNode === this) child.parentNode = null;
      return child;
    }
    getRootNode() {
      let node = this;
      const seen = new Set();
      while (node && !seen.has(node)) {
        seen.add(node);
        if (docRef && (node === docRef || node === docRef.body || node === docRef.documentElement)) {
          return docRef;
        }
        node = node.parentElement || node.parentNode || null;
      }
      return this;
    }
    isSameNode(other) {
      return this === other;
    }
  }

  class HTMLElement extends Element {}
  class HTMLInputElement extends HTMLElement {
    constructor() {
      super("input");
      this.type = "text";
    }
  }
  class HTMLSelectElement extends HTMLElement {
    constructor() {
      super("select");
    }
  }
  class HTMLAnchorElement extends HTMLElement {
    constructor() {
      super("a");
    }
  }
  class HTMLFormElement extends HTMLElement {
    constructor() {
      super("form");
      this.elements = [];
      this.noValidate = true;
      this.name = "";
    }
    appendChild(child) {
      super.appendChild(child);
      if (
        child instanceof Element &&
        (child.tagName === "INPUT" || child.tagName === "SELECT" || child.tagName === "TEXTAREA") &&
        !this.elements.includes(child)
      ) {
        this.elements.push(child);
      }
      return child;
    }
    removeChild(child) {
      super.removeChild(child);
      this.elements = this.elements.filter((el) => el !== child);
      return child;
    }
  }
  class DocumentFragment {}
  class ShadowRoot {}
  class Document {
    constructor() {
      this.readyState = "loading";
      this.activeElement = null;
      this.children = [];
      this._listeners = {};
      this.location = null;
    }
    createElement(tag) {
      const name = String(tag || "div").toLowerCase();
      if (name === "form") return new HTMLFormElement();
      if (name === "input") return new HTMLInputElement();
      if (name === "select") return new HTMLSelectElement();
      if (name === "a") return new HTMLAnchorElement();
      return new HTMLElement(name);
    }
    querySelector(selector) {
      const found = this.querySelectorAll(selector);
      return found[0] || null;
    }
    querySelectorAll(selector) {
      return queryAll(this, selector);
    }
    addEventListener(type, fn) {
      listen(this, type, fn);
    }
    removeEventListener(type, fn) {
      forget(this, type, fn);
    }
    dispatchEvent(event) {
      return emit(this, event);
    }
    getElementById(id) {
      return this.querySelector("#" + id);
    }
  }

  function listen(target, type, fn) {
    if (!target._listeners[type]) target._listeners[type] = [];
    target._listeners[type].push(fn);
  }
  function forget(target, type, fn) {
    const bucket = target._listeners[type];
    if (!bucket) return;
    target._listeners[type] = bucket.filter((item) => item !== fn);
  }
  function emit(target, event) {
    if (event.defaultPrevented == null) event.defaultPrevented = false;
    if (event.target == null) event.target = target;
    const chain = [];
    const seen = new Set();
    let node = target;
    while (node && !seen.has(node)) {
      seen.add(node);
      chain.push(node);
      node = node.parentElement || node.parentNode || null;
    }
    for (const current of chain) {
      if (current !== target && event.bubbles !== true) break;
      const list = (current._listeners && current._listeners[event.type]) || [];
      for (const fn of list.slice()) fn.call(current, event);
    }
    return event.defaultPrevented !== true;
  }
  function queryAll(root, selector) {
    const out = [];
    const kids = root.children || [];
    for (let i = 0; i < kids.length; i++) {
      const child = kids[i];
      if (typeof child.matches === "function" && child.matches(selector)) out.push(child);
      out.push(...queryAll(child, selector));
    }
    return out;
  }

  class XPathEvaluator {
    createExpression() {
      return {
        evaluate() {
          return { iterateNext() { return null; } };
        },
      };
    }
  }

  class CustomEvent {
    constructor(type, init) {
      init = init || {};
      this.type = type;
      this.detail = init.detail;
      this.bubbles = !!init.bubbles;
      this.cancelable = !!init.cancelable;
      this.composed = !!init.composed;
      this.defaultPrevented = false;
      this.target = null;
    }
    preventDefault() {
      if (this.cancelable) this.defaultPrevented = true;
    }
  }

  class XMLHttpRequest {
    constructor() {
      XMLHttpRequest.created += 1;
      XMLHttpRequest.last = this;
      this.headers = {};
      this.method = "";
      this.url = "";
      this.sentFlag = false;
      this.sent = null;
      this.upload = { addEventListener() {} };
      this._listeners = {};
    }
    open(method, url) {
      this.method = method;
      this.url = url;
    }
    setRequestHeader(name, value) {
      this.headers[name] = value;
    }
    overrideMimeType() {}
    send(body) {
      this.sentFlag = true;
      this.sent = body;
    }
    addEventListener(type, fn) {
      if (!this._listeners[type]) this._listeners[type] = [];
      this._listeners[type].push(fn);
    }
    getResponseHeader() {
      return null;
    }
    getAllResponseHeaders() {
      return "";
    }
  }
  XMLHttpRequest.created = 0;
  XMLHttpRequest.last = null;

  class FormData {
    constructor(form) {
      this._entries = [];
      if (form && form.elements) {
        for (let i = 0; i < form.elements.length; i++) {
          const field = form.elements[i];
          if (!field || field.disabled || field.name == null || field.name === "") continue;
          this._entries.push([String(field.name), field.value]);
        }
      }
    }
    append(name, value) {
      this._entries.push([String(name), value]);
    }
    delete(name) {
      this._entries = this._entries.filter((entry) => entry[0] !== String(name));
    }
    getAll(name) {
      return this._entries.filter((entry) => entry[0] === String(name)).map((entry) => entry[1]);
    }
    has(name) {
      return this._entries.some((entry) => entry[0] === String(name));
    }
    forEach(fn) {
      for (const [name, value] of this._entries) fn(value, name);
    }
    keys() {
      return this._entries.map((entry) => entry[0])[Symbol.iterator]();
    }
  }

  class File {}
  class Blob {}

  const location = {
    href: "http://gallery.local/",
    origin: "http://gallery.local",
    pathname: "/",
    search: "",
    hostname: "gallery.local",
    protocol: "http:",
    host: "gallery.local",
  };

  const document = new Document();
  const html = new HTMLElement("html");
  const head = new HTMLElement("head");
  const body = new HTMLElement("body");
  html.appendChild(head);
  html.appendChild(body);
  html.parentElement = null;
  html.parentNode = document;
  document.documentElement = html;
  document.head = head;
  document.body = body;
  document.children = [html];
  document.location = location;
  docRef = document;

  const storage = new Map();
  const sandbox = {
    Element,
    HTMLElement,
    HTMLInputElement,
    HTMLSelectElement,
    HTMLAnchorElement,
    HTMLFormElement,
    Document,
    DocumentFragment,
    ShadowRoot,
    Node,
    XPathEvaluator,
    CustomEvent,
    XMLHttpRequest,
    FormData,
    File,
    Blob,
    URL,
    URLSearchParams,
    document,
    location,
    console: {
      log(...args) {
        logs.push(args);
      },
      error() {},
    },
    history: {
      pushState() {},
      replaceState() {},
      state: null,
    },
    localStorage: {
      getItem(key) {
        return storage.has(key) ? storage.get(key) : null;
      },
      setItem(key, value) {
        storage.set(String(key), String(value));
      },
      removeItem(key) {
        storage.delete(key);
      },
    },
    setTimeout(fn, ms, ...args) {
      return setTimeout(fn, ms, ...args);
    },
    clearTimeout(id) {
      return clearTimeout(id);
    },
    setInterval(fn, ms, ...args) {
      return setInterval(fn, ms, ...args);
    },
    clearInterval(id) {
      return clearInterval(id);
    },
    __logs: logs,
  };
  sandbox.window = sandbox;
  return { sandbox, logs, XMLHttpRequest };
}

function loadHtmx() {
  const { sandbox, logs, XMLHttpRequest: XHR } = installBrowser();
  sandbox.document.readyState = "loading";
  vm.createContext(sandbox);
  const source = fs.readFileSync(path.join(__dirname, "..", "static", "htmx.min.js"), "utf8");
  vm.runInContext(source, sandbox, { filename: "htmx.min.js" });
  sandbox.document.readyState = "complete";
  if (!sandbox.htmx || typeof sandbox.htmx.parseInterval !== "function") {
    throw new Error("htmx.min.js did not publish its public API");
  }
  return { htmx: sandbox.htmx, document: sandbox.document, logs, XMLHttpRequest: XHR };
}

function clearBody() {
  while (document.body.children.length > 0) {
    document.body.removeChild(document.body.children[0]);
  }
}

function field(name, value) {
  const input = document.createElement("input");
  input.setAttribute("name", name);
  input.value = value;
  return input;
}

describe("htmx.min.js", { concurrency: false }, () => {
  before(() => {
    const loaded = loadHtmx();
    htmx = loaded.htmx;
    document = loaded.document;
    logs = loaded.logs;
    XMLHttpRequest = loaded.XMLHttpRequest;
  });

  beforeEach(() => {
    clearBody();
    logs.length = 0;
    htmx.logNone();
    htmx.config.allowEval = true;
  });

  test("parseInterval converts durations and rejects empty or unparsable text", () => {
    assert.equal(htmx.parseInterval("10ms"), 10);
    assert.equal(htmx.parseInterval("2s"), 2000);
    assert.equal(htmx.parseInterval("1m"), 60000);
    assert.equal(htmx.parseInterval("15"), 15);
    assert.equal(htmx.parseInterval("0ms"), 0);
    assert.equal(htmx.parseInterval(""), undefined);
    assert.equal(htmx.parseInterval(undefined), undefined);
    assert.equal(htmx.parseInterval(null), undefined);
    assert.equal(htmx.parseInterval("nope"), undefined);
  });

  test("addClass adds a class once and ignores a value that is not an element", () => {
    const el = document.createElement("div");
    htmx.addClass(el, "on");
    htmx.addClass(el, "on");
    assert.equal(el.getAttribute("class"), "on");
    assert.equal(el.classList.contains("on"), true);
    htmx.addClass(null, "on");
    htmx.addClass({}, "on");
  });

  test("removeClass removes a class and drops the attribute when none remain", () => {
    const el = document.createElement("div");
    htmx.addClass(el, "on");
    htmx.removeClass(el, "missing");
    assert.equal(el.classList.contains("on"), true);
    htmx.removeClass(el, "on");
    assert.equal(el.classList.contains("on"), false);
    assert.equal(el.hasAttribute("class"), false);
    assert.equal(el.getAttribute("class"), null);
    htmx.removeClass(null, "on");
  });

  test("toggleClass turns a class on and then off", () => {
    const el = document.createElement("div");
    htmx.toggleClass(el, "on");
    assert.equal(el.classList.contains("on"), true);
    htmx.toggleClass(el, "on");
    assert.equal(el.classList.contains("on"), false);
  });

  test("takeClass keeps the class on the element and clears it from siblings", () => {
    const parent = document.createElement("div");
    const keeper = document.createElement("span");
    const other = document.createElement("span");
    parent.appendChild(keeper);
    parent.appendChild(other);
    htmx.addClass(keeper, "pick");
    htmx.addClass(other, "pick");
    htmx.takeClass(keeper, "pick");
    assert.equal(keeper.classList.contains("pick"), true);
    assert.equal(other.classList.contains("pick"), false);
  });

  test("remove detaches an element and, given a duplicated id, only the first match", () => {
    const parent = document.createElement("div");
    const only = document.createElement("span");
    parent.appendChild(only);
    htmx.remove(only);
    assert.equal(only.parentElement, null);
    assert.equal(parent.children.length, 0);

    document.body.appendChild(parent);
    const first = document.createElement("div");
    const second = document.createElement("div");
    first.setAttribute("id", "same");
    second.setAttribute("id", "same");
    parent.appendChild(first);
    parent.appendChild(second);
    htmx.remove("#same");
    assert.equal(first.parentElement, null);
    assert.equal(second.parentElement, parent);
  });

  test("find returns the first match and null when nothing matches", () => {
    const parent = document.createElement("div");
    const first = document.createElement("span");
    const second = document.createElement("span");
    first.classList.add("item");
    second.classList.add("item");
    parent.appendChild(first);
    parent.appendChild(second);
    assert.equal(htmx.find(parent, ".item"), first);
    assert.equal(htmx.find(parent, ".missing"), null);
  });

  test("findAll returns every match and an empty list when nothing matches", () => {
    const parent = document.createElement("div");
    const first = document.createElement("span");
    const second = document.createElement("span");
    first.classList.add("item");
    second.classList.add("item");
    parent.appendChild(first);
    parent.appendChild(second);
    const found = htmx.findAll(parent, ".item");
    assert.equal(found.length, 2);
    assert.equal(found[0], first);
    assert.equal(found[1], second);
    assert.equal(htmx.findAll(parent, ".missing").length, 0);
  });

  test("closest returns the nearest ancestor and null when none matches", () => {
    const outer = document.createElement("div");
    outer.classList.add("outer");
    const inner = document.createElement("span");
    outer.appendChild(inner);
    assert.equal(htmx.closest(inner, "div"), outer);
    assert.equal(htmx.closest(inner, ".missing"), null);
    assert.equal(htmx.closest(null, "div"), null);
  });

  test("values reads fields, keeps zero and empty text, and groups duplicate names", () => {
    const title = field("title", "hola");
    assert.equal(htmx.values(title).title, "hola");

    const zero = field("count", "0");
    assert.equal(htmx.values(zero).count, "0");

    const blank = field("note", "");
    assert.equal(htmx.values(blank).note, "");

    const nameless = document.createElement("input");
    nameless.value = "hidden";
    assert.equal(htmx.values(nameless).hidden, undefined);

    const form = document.createElement("form");
    const red = field("color", "red");
    const blue = field("color", "blue");
    form.appendChild(red);
    form.appendChild(blue);
    const grouped = htmx.values(form).color;
    assert.equal(grouped.length, 2);
    assert.equal(grouped[0], "red");
    assert.equal(grouped[1], "blue");
  });

  test("on reports the listened event and ignores a different one", () => {
    const el = document.createElement("div");
    let hits = 0;
    htmx.on(el, "ping", () => {
      hits += 1;
    });
    htmx.trigger(el, "ping");
    htmx.trigger(el, "other");
    assert.equal(hits, 1);
  });

  test("off removes a listener and stays quiet when called again", () => {
    const el = document.createElement("div");
    let hits = 0;
    const listener = () => {
      hits += 1;
    };
    htmx.on(el, "ping", listener);
    htmx.trigger(el, "ping");
    htmx.off(el, "ping", listener);
    htmx.trigger(el, "ping");
    htmx.off(el, "ping", listener);
    htmx.off(el, "never", listener);
    assert.equal(hits, 1);
  });

  test("trigger delivers detail and still settles when nobody is listening", () => {
    const el = document.createElement("div");
    let detail = null;
    htmx.on(el, "ping", (event) => {
      detail = event.detail;
    });
    const delivered = htmx.trigger(el, "ping", { n: 1 });
    assert.equal(delivered, true);
    assert.equal(detail.n, 1);
    assert.equal(detail.elt, el);

    const quiet = document.createElement("div");
    assert.equal(htmx.trigger(quiet, "absent"), true);
    const bare = htmx.trigger(el, "bare");
    assert.equal(bare, true);
  });

  test("ajax sends a same-origin GET and does not send for a detached element", () => {
    const before = XMLHttpRequest.created;
    const detached = document.createElement("div");
    htmx.ajax("GET", "/drawings", { source: detached, target: detached });
    assert.equal(XMLHttpRequest.created, before);
    assert.equal(detached.classList.contains("htmx-request"), false);

    const live = document.createElement("div");
    document.body.appendChild(live);
    htmx.ajax("GET", "/drawings", { source: live, target: live });
    assert.equal(XMLHttpRequest.created, before + 1);
    assert.equal(XMLHttpRequest.last.method, "GET");
    assert.equal(XMLHttpRequest.last.url, "/drawings");
    assert.equal(XMLHttpRequest.last.sentFlag, true);
    assert.equal(XMLHttpRequest.last.headers["HX-Request"], "true");
    assert.equal(live.classList.contains("htmx-request"), true);
  });

  test("swap replaces text and clears it when the new text is empty", () => {
    const el = document.createElement("div");
    el.textContent = "old";
    const spec = { swapStyle: "textContent", settleDelay: 0, swapDelay: 0 };
    htmx.swap(el, "new", spec);
    assert.equal(el.textContent, "new");
    assert.equal(el.classList.contains("htmx-settling"), false);
    htmx.swap(el, "", spec);
    assert.equal(el.textContent, "");
  });

  test("defineExtension handles its own events and a later definition replaces it", () => {
    let inited = 0;
    const seen = [];
    try {
      htmx.defineExtension("room", {
        init() {
          inited += 1;
        },
        onEvent(name) {
          seen.push(name);
          return true;
        },
      });
      assert.equal(inited, 1);
      const el = document.createElement("div");
      el.setAttribute("hx-ext", "room");
      htmx.trigger(el, "room:ping");
      assert.deepEqual(seen, ["room:ping"]);

      seen.length = 0;
      htmx.defineExtension("room", {
        onEvent(name) {
          seen.push("next:" + name);
          return true;
        },
      });
      htmx.trigger(el, "room:ping");
      assert.deepEqual(seen, ["next:room:ping"]);
    } finally {
      htmx.removeExtension("room");
    }
  });

  test("removeExtension stops a known name and ignores an unknown one", () => {
    const seen = [];
    try {
      htmx.defineExtension("room", {
        onEvent(name) {
          seen.push(name);
          return true;
        },
      });
      const el = document.createElement("div");
      el.setAttribute("hx-ext", "room");
      htmx.removeExtension("nobody");
      htmx.trigger(el, "room:ping");
      assert.deepEqual(seen, ["room:ping"]);
      htmx.removeExtension("room");
      htmx.trigger(el, "room:ping");
      assert.deepEqual(seen, ["room:ping"]);
    } finally {
      htmx.removeExtension("room");
    }
  });

  test("logAll records each triggered event once", () => {
    const el = document.createElement("div");
    htmx.logAll();
    htmx.logAll();
    htmx.trigger(el, "ping");
    assert.equal(logs.filter((row) => row[0] === "ping").length, 1);
    htmx.trigger(el, "other");
    assert.equal(logs.filter((row) => row[0] === "ping").length, 1);
  });

  test("logNone silences a logger that was recording", () => {
    const el = document.createElement("div");
    htmx.logAll();
    htmx.logNone();
    htmx.trigger(el, "ping");
    htmx.logNone();
    htmx.trigger(el, "ping");
    assert.equal(logs.filter((row) => row[0] === "ping").length, 0);
  });

  test("process runs hx-on and leaves a disabled or empty element alone", () => {
    const el = document.createElement("button");
    el.setAttribute("hx-on:click", 'this.setAttribute("data-ran","1")');
    htmx.process(el);
    el.dispatchEvent({ type: "click" });
    assert.equal(el.getAttribute("data-ran"), "1");

    const idle = document.createElement("div");
    htmx.process(idle);
    idle.dispatchEvent({ type: "click" });
    assert.equal(idle.getAttribute("data-ran"), null);

    const disabled = document.createElement("div");
    disabled.setAttribute("hx-disable", "true");
    disabled.setAttribute("hx-on:click", 'this.setAttribute("data-ran","1")');
    htmx.process(disabled);
    disabled.dispatchEvent({ type: "click" });
    assert.equal(disabled.getAttribute("data-ran"), null);
  });

  test("onLoad receives the loaded element and does not run before that event", () => {
    const child = document.createElement("div");
    document.body.appendChild(child);
    let seen = null;
    let calls = 0;
    htmx.onLoad((got) => {
      calls += 1;
      seen = got;
    });
    assert.equal(calls, 0);
    htmx.trigger(child, "htmx:load");
    assert.equal(calls, 1);
    assert.equal(seen, child);
  });

  test("the eval helper returns the expression and nothing when eval is disallowed", () => {
    assert.equal(htmx._("1+1"), 2);
    assert.equal(htmx._(""), undefined);
    htmx.config.allowEval = false;
    try {
      assert.equal(htmx._("1+1"), undefined);
    } finally {
      htmx.config.allowEval = true;
    }
  });
});
