// passkey.js: adds a passkey and signs in with one. A button with
// data-passkey ("add" or "sign-in") and data-passkey-url asks the hub for
// the options at <url>/options, lets the browser make or use the passkey,
// and sends its answer to <url>. The hub checks everything; this only
// carries the JSON both ways, with the browser's own PublicKeyCredential
// methods. On success the hub sends the page on, like any sign-in.
(() => {
  if (window.moinPasskey) return;
  window.moinPasskey = true;

  const say = (message) => {
    const line = document.querySelector("[data-passkey-error]");
    if (!line) return;
    line.textContent = message;
    line.hidden = !message;
  };

  const post = (url, body) =>
    fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": document.querySelector('meta[name="csrf-token"]').content },
      body,
    });

  // The hub says why in plain text; anything else is a page, not a reason.
  const reason = async (response) => {
    if (response.status === 429) return "Too many tries. Please wait a few minutes.";
    if ((response.headers.get("Content-Type") || "").startsWith("text/plain")) return (await response.text()).trim();
    return "Something went wrong. Please try again.";
  };

  document.addEventListener("click", async (event) => {
    const button = event.target.closest("[data-passkey]");
    if (!button || button.disabled) return;
    say("");
    if (!window.PublicKeyCredential?.parseCreationOptionsFromJSON) {
      say("This browser cannot use passkeys here. Update it, or open the hub over HTTPS.");
      return;
    }
    const url = button.dataset.passkeyUrl;
    button.disabled = true;
    try {
      const options = await post(url + "/options");
      if (options.redirected) return location.assign(options.url);
      if (!options.ok) return say(await reason(options));
      const json = await options.json();
      const credential =
        button.dataset.passkey === "add"
          ? await navigator.credentials.create({ publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(json) })
          : await navigator.credentials.get({ publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(json) });
      const answer = await post(url, JSON.stringify(credential.toJSON()));
      if (!answer.ok) return say(await reason(answer));
      location.assign(answer.url);
    } catch (error) {
      // Closing the browser's dialog is no mistake.
      if (error.name === "NotAllowedError" || error.name === "AbortError") return;
      if (error.name === "InvalidStateError") return say("This device has a passkey for your account already.");
      say("Something went wrong. Please try again.");
    } finally {
      button.disabled = false;
    }
  });
})();
