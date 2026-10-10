(function () {
  var root = document.querySelector("[data-auth]");
  if (!root) return;
  var button = root.querySelector("button");
  var errEl = root.querySelector("[data-auth-error]");
  var meta = document.querySelector('meta[name="csrf-token"]');
  var token = meta ? meta.getAttribute("content") : "";

  function b64urlToBuf(s) {
    var pad = s.length % 4 === 0 ? "" : "=".repeat(4 - (s.length % 4));
    var b64 = (s + pad).replace(/-/g, "+").replace(/_/g, "/");
    var bin = atob(b64);
    var buf = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) buf[i] = bin.charCodeAt(i);
    return buf.buffer;
  }

  function bufToB64url(buf) {
    var bytes = new Uint8Array(buf);
    var bin = "";
    for (var i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
    return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
  }

  function credToJSON(cred) {
    var r = cred.response;
    var out = {
      id: cred.id,
      rawId: bufToB64url(cred.rawId),
      type: cred.type,
      response: { clientDataJSON: bufToB64url(r.clientDataJSON) }
    };
    if (r.attestationObject) {
      out.response.attestationObject = bufToB64url(r.attestationObject);
    }
    if (r.authenticatorData) {
      out.response.authenticatorData = bufToB64url(r.authenticatorData);
      out.response.signature = bufToB64url(r.signature);
      if (r.userHandle) out.response.userHandle = bufToB64url(r.userHandle);
    }
    return out;
  }

  function showError(msg) {
    if (!errEl) return;
    errEl.hidden = false;
    errEl.textContent = msg || root.getAttribute("data-error") || "";
  }

  button.addEventListener("click", function () {
    if (errEl) errEl.hidden = true;
    var headers = { "X-CSRF-Token": token, "Accept": "application/json" };
    fetch(root.getAttribute("data-begin"), { method: "POST", headers: headers, credentials: "same-origin" })
      .then(function (r) {
        return r.json().then(function (body) {
          if (!r.ok) throw body;
          return body;
        });
      })
      .then(function (opts) {
        var pk = opts.publicKey;
        pk.challenge = b64urlToBuf(pk.challenge);
        if (pk.user && typeof pk.user.id === "string") pk.user.id = b64urlToBuf(pk.user.id);
        if (pk.excludeCredentials) {
          pk.excludeCredentials.forEach(function (c) { c.id = b64urlToBuf(c.id); });
        }
        if (pk.allowCredentials) {
          pk.allowCredentials.forEach(function (c) { c.id = b64urlToBuf(c.id); });
        }
        if (root.getAttribute("data-auth") === "register") {
          return navigator.credentials.create({ publicKey: pk });
        }
        return navigator.credentials.get({ publicKey: pk });
      })
      .then(function (cred) {
        return fetch(root.getAttribute("data-finish"), {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "X-CSRF-Token": token,
            "Accept": "application/json",
            "Content-Type": "application/json"
          },
          body: JSON.stringify(credToJSON(cred))
        });
      })
      .then(function (r) {
        return r.json().then(function (body) {
          if (!r.ok) throw body;
          return body;
        });
      })
      .then(function (body) {
        if (body.redirect) window.location.assign(body.redirect);
      })
      .catch(function (e) {
        showError(e && e.error);
      });
  });
})();
