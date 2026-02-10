package cli

import (
	"context"
	"fmt"

	"github.com/veilm/cdp-cli/internal/cdp"
)

const webNavScript = `(function(){
  if (window.WebNavInjected) { return; }

  function isIterable(input) {
    return input && typeof input !== "string" && typeof input[Symbol.iterator] === "function";
  }

  function toArray(input) {
    if (!input) return [];
    if (Array.isArray(input)) return input;
    if (isIterable(input)) return Array.from(input);
    return [];
  }

  function normalizeSelectors(input) {
    if (!input) return [];
    if (typeof input === "string") return [input];
    if (Array.isArray(input)) return input;
    return [];
  }

  class WebNavElements extends Array {
    hasText(text, {
      caseSensitive = true,
      visible = false,
      trim = false,
      normalizeWhitespace = true,
      includeDescendants = true,
    } = {}) {
      const needle = caseSensitive ? String(text) : String(text).toLowerCase();

      const getDirectText = (el) => {
        let s = "";
        for (const node of el.childNodes) {
          if (node && node.nodeType === Node.TEXT_NODE) {
            s += node.textContent || "";
          }
        }
        return s;
      };

      const getHay = (el) => {
        let s = "";
        if (visible) {
          s = el.innerText || "";
        } else {
          s = includeDescendants ? (el.textContent || "") : getDirectText(el);
        }
        if (normalizeWhitespace) s = s.replace(/\s+/g, " ");
        if (trim) s = s.trim();
        if (!caseSensitive) s = s.toLowerCase();
        return s;
      };

      return new WebNavElements(...this.filter((el) => getHay(el).includes(needle)));
    }

    hasAttValue(value) {
      const needle = String(value);
      return new WebNavElements(...this.filter((el) => {
        const attrs = el.attributes;
        if (!attrs) return false;
        for (let i = 0; i < attrs.length; i++) {
          if ((attrs[i].value || "").includes(needle)) return true;
        }
        return false;
      }));
    }

    querySelectorAll(sel) {
      return new WebNavElements(
        ...this.flatMap((el) => Array.from(el.querySelectorAll(sel)))
      );
    }

    querySelector(sel) {
      for (const el of this) {
        const found = el.querySelector(sel);
        if (found) return found;
      }
      return null;
    }
  }

  const toWebNavElements = (iterable) => {
    if (iterable instanceof WebNavElements) {
      return iterable;
    }
    return new WebNavElements(...iterable);
  };

  if (!window.WebNavElements) {
    window.WebNavElements = WebNavElements;
  }

  if (!NodeList.prototype.hasText) {
    NodeList.prototype.hasText = function (text, opts) {
      return toWebNavElements(this).hasText(text, opts);
    };
  }

  if (!NodeList.prototype.hasAttValue) {
    NodeList.prototype.hasAttValue = function (value) {
      return toWebNavElements(this).hasAttValue(value);
    };
  }

  if (!NodeList.prototype.querySelectorAll) {
    NodeList.prototype.querySelectorAll = function (sel) {
      return toWebNavElements(this).querySelectorAll(sel);
    };
  }

  if (!NodeList.prototype.querySelector) {
    NodeList.prototype.querySelector = function (sel) {
      return toWebNavElements(this).querySelector(sel);
    };
  }

  function resolveElement(input) {
    if (input && input.nodeType === 1) {
      return { el: input, selector: "" };
    }

    // Array of strings: try each as CSS selector (fallback pattern)
    if (Array.isArray(input) && input.length > 0 && typeof input[0] === "string") {
      for (const selector of input) {
        const el = document.querySelector(selector);
        if (el) return { el, selector };
      }
      return { el: null, selector: "" };
    }

    // Iterable of elements (NodeList, WebNavElements from .hasText() chains, etc.)
    if (isIterable(input)) {
      const list = toArray(input).filter((item) => item && item.nodeType === 1);
      if (list.length > 0) return { el: list[0], selector: "" };
      return { el: null, selector: "" };
    }

    // Single string selector
    if (typeof input === "string") {
      const el = document.querySelector(input);
      return { el, selector: input };
    }

    return { el: null, selector: "" };
  }

  function focusElement(el) {
    if (!el) return;
    if (el.scrollIntoView) {
      el.scrollIntoView({block: "center", inline: "center"});
    }
    if (el.focus) {
      el.focus();
    }
  }

  // --- Key string parsing ---
  function parseKeyString(spec) {
    const modMap = {
      ctrl: "ctrlKey", control: "ctrlKey",
      alt: "altKey",
      meta: "metaKey", cmd: "metaKey", command: "metaKey", win: "metaKey",
      shift: "shiftKey",
    };
    const namedKeys = {
      enter: {key: "Enter", code: "Enter"},
      escape: {key: "Escape", code: "Escape"},
      esc: {key: "Escape", code: "Escape"},
      tab: {key: "Tab", code: "Tab"},
      backspace: {key: "Backspace", code: "Backspace"},
      delete: {key: "Delete", code: "Delete"},
      space: {key: " ", code: "Space"},
      arrowup: {key: "ArrowUp", code: "ArrowUp"},
      arrowdown: {key: "ArrowDown", code: "ArrowDown"},
      arrowleft: {key: "ArrowLeft", code: "ArrowLeft"},
      arrowright: {key: "ArrowRight", code: "ArrowRight"},
      home: {key: "Home", code: "Home"},
      end: {key: "End", code: "End"},
      pageup: {key: "PageUp", code: "PageUp"},
      pagedown: {key: "PageDown", code: "PageDown"},
    };
    for (let i = 1; i <= 12; i++) namedKeys["f" + i] = {key: "F" + i, code: "F" + i};

    const tokens = spec.split("+");
    const mods = {ctrlKey: false, shiftKey: false, altKey: false, metaKey: false};
    let keyToken = null;

    for (const t of tokens) {
      const s = t.trim();
      const lower = s.toLowerCase();
      if (modMap[lower]) {
        mods[modMap[lower]] = true;
        continue;
      }
      if (keyToken !== null) throw new Error("only one non-modifier key: " + spec);
      keyToken = s;
    }

    if (!keyToken) throw new Error("no key in spec: " + spec);

    const lower = keyToken.toLowerCase();
    if (namedKeys[lower]) return { ...namedKeys[lower], ...mods };

    if (keyToken.length === 1) {
      const ch = keyToken;
      const upper = ch.toUpperCase();
      let code = "";
      if (upper >= "A" && upper <= "Z") {
        code = "Key" + upper;
        if (ch === upper && !mods.shiftKey) mods.shiftKey = true;
      } else if (ch >= "0" && ch <= "9") {
        code = "Digit" + ch;
      }
      return { key: ch, code, ...mods };
    }

    throw new Error("unknown key: " + keyToken);
  }

  const WebNav = {};

  WebNav.focus = function(target) {
    const resolved = resolveElement(target);
    if (!resolved.el) throw new Error("no element matched selector");
    focusElement(resolved.el);
    return true;
  };

  WebNav.click = function(target, count, opts) {
    const clicks = count && count > 0 ? count : 1;

    // If target is a direct iterable of elements and opts.all, click all
    if (opts && opts.all && isIterable(target)) {
      const list = toArray(target).filter((item) => item && item.nodeType === 1);
      if (!list.length) {
        throw new Error("no element matched");
      }
      let submitForm = false;
      for (const el of list) {
        focusElement(el);
        const tag = el.tagName ? el.tagName.toLowerCase() : "";
        let isSubmit = false;
        if (tag === "button") {
          const t = el.getAttribute("type");
          isSubmit = !t || String(t).toLowerCase() === "submit";
        } else if (tag === "input") {
          const t = el.getAttribute("type") || el.type;
          isSubmit = String(t || "").toLowerCase() === "submit";
        }
        const inForm = !!(el.closest && el.closest("form"));
        if (isSubmit && inForm) submitForm = true;
        for (let i = 0; i < clicks; i++) {
          el.click();
        }
      }
      return { submitForm, selector: "" };
    }

    const resolved = resolveElement(target);
    if (!resolved.el) {
      const selectors = normalizeSelectors(target);
      throw new Error("no element matched selectors: " + selectors.join(", "));
    }
    const el = resolved.el;
    focusElement(el);
    const tag = el.tagName ? el.tagName.toLowerCase() : "";
    let isSubmit = false;
    if (tag === "button") {
      const t = el.getAttribute("type");
      isSubmit = !t || String(t).toLowerCase() === "submit";
    } else if (tag === "input") {
      const t = el.getAttribute("type") || el.type;
      isSubmit = String(t || "").toLowerCase() === "submit";
    }
    const inForm = !!(el.closest && el.closest("form"));
    for (let i = 0; i < clicks; i++) {
      el.click();
    }
    return { submitForm: isSubmit && inForm, selector: resolved.selector };
  };

  WebNav.hover = function(target) {
    const resolved = resolveElement(target);
    if (!resolved.el) {
      const selectors = normalizeSelectors(target);
      throw new Error("no element matched selectors: " + selectors.join(", "));
    }
    const el = resolved.el;
    focusElement(el);

    const rect = el.getBoundingClientRect();
    const x = rect.left + rect.width / 2;
    const y = rect.top + rect.height / 2;

    function dispatchMouse(type) {
      const evt = new MouseEvent(type, {
        bubbles: true,
        cancelable: true,
        clientX: x,
        clientY: y,
        button: 0,
        buttons: 0,
      });
      el.dispatchEvent(evt);
    }

    if (typeof PointerEvent !== "undefined") {
      const pe = (type) => new PointerEvent(type, {bubbles: true, cancelable: true, clientX: x, clientY: y, pointerType: "mouse"});
      el.dispatchEvent(pe("pointerenter"));
      el.dispatchEvent(pe("pointerover"));
      el.dispatchEvent(pe("pointermove"));
    }

    dispatchMouse("mouseenter");
    dispatchMouse("mouseover");
    dispatchMouse("mousemove");
    return { x, y, selector: resolved.selector };
  };

  WebNav.drag = async function(fromTarget, toTarget, fromIndex, toIndex, delayMs) {
    function sleep(ms) {
      if (!ms || ms <= 0) return Promise.resolve();
      return new Promise(resolve => setTimeout(resolve, ms));
    }

    function pick(target, index) {
      if (target && target.nodeType === 1) return { el: target, list: [target] };
      if (isIterable(target)) {
        const list = toArray(target).filter((item) => item && item.nodeType === 1);
        if (!list.length) return { el: null, list };
        const idx = Math.min(Math.max(index || 0, 0), list.length - 1);
        return { el: list[idx], list };
      }
      if (typeof target !== "string") return { el: null, list: [] };
      const list = Array.from(document.querySelectorAll(target));
      if (!list.length) return { el: null, list };
      const idx = Math.min(Math.max(index || 0, 0), list.length - 1);
      return { el: list[idx], list };
    }

    const fromPick = pick(fromTarget, fromIndex);
    const toPick = pick(toTarget, toIndex);
    if (!fromPick.el) throw new Error("no element matched selector: " + fromTarget);
    if (!toPick.el) throw new Error("no element matched selector: " + toTarget);

    const fromEl = fromPick.el;
    const toEl = toPick.el;

    const fromRect = fromEl.getBoundingClientRect();
    const toRect = toEl.getBoundingClientRect();
    const fromPt = {x: fromRect.left + Math.max(2, Math.min(fromRect.width - 2, fromRect.width / 2)),
                    y: fromRect.top + Math.max(2, Math.min(fromRect.height - 2, fromRect.height / 2))};
    const toPt = {x: toRect.left + Math.max(2, Math.min(toRect.width - 2, toRect.width / 2)),
                  y: toRect.top + Math.max(2, Math.min(toRect.height - 2, toRect.height / 2))};

    let dataTransfer = null;
    if (typeof DataTransfer !== "undefined") {
      dataTransfer = new DataTransfer();
    } else {
      dataTransfer = {
        data: {},
        setData(type, val) { this.data[type] = String(val); },
        getData(type) { return this.data[type] || ""; },
        clearData(type) { if (type) delete this.data[type]; else this.data = {}; },
        effectAllowed: "all",
        dropEffect: "move",
        types: [],
      };
    }

    function dispatchMouse(el, type, pt) {
      const evt = new MouseEvent(type, {
        bubbles: true,
        cancelable: true,
        clientX: pt.x,
        clientY: pt.y,
        button: 0,
        buttons: type === "mouseup" ? 0 : 1,
      });
      el.dispatchEvent(evt);
    }

    function dispatchDrag(el, type, pt) {
      const evt = new DragEvent(type, {
        bubbles: true,
        cancelable: true,
        clientX: pt.x,
        clientY: pt.y,
        dataTransfer,
      });
      el.dispatchEvent(evt);
    }

    if (fromEl.scrollIntoView) fromEl.scrollIntoView({block: "center", inline: "center"});
    if (toEl.scrollIntoView) toEl.scrollIntoView({block: "center", inline: "center"});

    dispatchMouse(fromEl, "mousedown", fromPt);
    dispatchDrag(fromEl, "dragstart", fromPt);
    await sleep(delayMs);
    dispatchDrag(toEl, "dragenter", toPt);
    dispatchDrag(toEl, "dragover", toPt);
    await sleep(delayMs);
    dispatchDrag(toEl, "drop", toPt);
    dispatchDrag(fromEl, "dragend", toPt);
    dispatchMouse(toEl, "mouseup", toPt);
    return {fromIndex: fromIndex || 0, toIndex: toIndex || 0, fromCount: fromPick.list.length, toCount: toPick.list.length};
  };

  WebNav.gesture = async function(target, points, delayMs) {
    function sleep(ms) {
      if (!ms || ms <= 0) return Promise.resolve();
      return new Promise(resolve => setTimeout(resolve, ms));
    }

    const resolved = resolveElement(target);
    if (!resolved.el) throw new Error("no element matched selector: " + target);
    const el = resolved.el;
    focusElement(el);

    const rect = el.getBoundingClientRect();
    function toAbs(rx, ry) {
      return { x: rect.left + rx * rect.width, y: rect.top + ry * rect.height };
    }

    function dispatchPointer(type, x, y, isDown) {
      if (typeof PointerEvent !== "undefined") {
        el.dispatchEvent(new PointerEvent(type, {
          bubbles: true,
          cancelable: true,
          clientX: x,
          clientY: y,
          pointerType: "mouse",
          button: 0,
          buttons: isDown ? 1 : 0,
        }));
      }
    }

    function dispatchMouse(type, x, y, isDown) {
      el.dispatchEvent(new MouseEvent(type, {
        bubbles: true,
        cancelable: true,
        clientX: x,
        clientY: y,
        button: 0,
        buttons: isDown ? 1 : 0,
      }));
    }

    const first = toAbs(points[0][0], points[0][1]);
    dispatchPointer("pointerdown", first.x, first.y, true);
    dispatchMouse("mousedown", first.x, first.y, true);

    for (let i = 1; i < points.length; i++) {
      await sleep(delayMs);
      const pt = toAbs(points[i][0], points[i][1]);
      dispatchPointer("pointermove", pt.x, pt.y, true);
      dispatchMouse("mousemove", pt.x, pt.y, true);
    }

    const last = toAbs(points[points.length - 1][0], points[points.length - 1][1]);
    await sleep(delayMs);
    dispatchPointer("pointerup", last.x, last.y, false);
    dispatchMouse("mouseup", last.x, last.y, false);

    return { points: points.length };
  };

  WebNav.key = function(spec) {
    let params;
    if (typeof spec === "string") {
      params = parseKeyString(spec);
    } else {
      params = {
        key: spec && spec.key ? spec.key : "",
        code: spec && spec.code ? spec.code : "",
        ctrlKey: !!(spec && spec.ctrlKey),
        shiftKey: !!(spec && spec.shiftKey),
        altKey: !!(spec && spec.altKey),
        metaKey: !!(spec && spec.metaKey),
      };
    }
    const eventInit = {
      key: params.key,
      code: params.code,
      bubbles: true,
      ctrlKey: !!params.ctrlKey,
      shiftKey: !!params.shiftKey,
      altKey: !!params.altKey,
      metaKey: !!params.metaKey,
    };
    document.dispatchEvent(new KeyboardEvent("keydown", eventInit));
    document.dispatchEvent(new KeyboardEvent("keyup", eventInit));
    return true;
  };

  WebNav.typePrepare = function(target, inputText, append) {
    const resolved = resolveElement(target);
    if (!resolved.el) {
      throw new Error("no element matched");
    }
    const el = resolved.el;
    focusElement(el);

    const tag = el.tagName ? el.tagName.toLowerCase() : "";
    const isTextInput = tag === "input" || tag === "textarea";
    if (isTextInput) {
      const inputType = (el.getAttribute && el.getAttribute("type") ? el.getAttribute("type") : el.type) || "";
      const normalizedType = inputType ? String(inputType).toLowerCase() : "";
      const useNativeValue = tag === "input" && normalizedType === "number";
      if (useNativeValue) {
        const next = append ? String(el.value || "") + String(inputText) : String(inputText);
        const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
        if (setter) {
          setter.call(el, next);
        } else {
          el.value = next;
        }
        try {
          el.dispatchEvent(new Event("input", {bubbles: true}));
          el.dispatchEvent(new Event("change", {bubbles: true}));
        } catch (e) {}
        return { found: true, editable: true, contentEditable: false, handled: true, selector: resolved.selector };
      }
      if (!append) {
        el.value = "";
      }
      if (el.setSelectionRange) {
        try {
          const end = el.value.length;
          el.setSelectionRange(end, end);
        } catch (e) {}
      }
      return { found: true, editable: true, contentEditable: false, handled: false, selector: resolved.selector };
    }
    if (el.isContentEditable) {
      if (!append) {
        el.textContent = "";
      }
      const range = document.createRange();
      range.selectNodeContents(el);
      range.collapse(false);
      const sel = window.getSelection();
      sel.removeAllRanges();
      sel.addRange(range);
      return { found: true, editable: true, contentEditable: true, handled: false, selector: resolved.selector };
    }
    return { found: true, editable: false, contentEditable: false, handled: false, selector: resolved.selector };
  };

  WebNav.typeFallback = function(target, inputText, append) {
    const resolved = resolveElement(target);
    if (!resolved.el) return { ok: false };
    const el = resolved.el;
    if (!append) {
      el.textContent = "";
    }
    el.textContent = append ? el.textContent + String(inputText) : String(inputText);
    return { ok: true, selector: resolved.selector };
  };

  WebNav.type = function(target, inputText, append) {
    const resolved = resolveElement(target);
    if (!resolved.el) {
      throw new Error("no element matched");
    }
    const el = resolved.el;
    focusElement(el);

    const tag = (el.tagName || "").toLowerCase();
    const isInput = tag === "input";
    const isTextarea = tag === "textarea";

    if (isInput || isTextarea) {
      const next = append ? String(el.value || "") + String(inputText) : String(inputText);
      const proto = isInput ? HTMLInputElement.prototype : HTMLTextAreaElement.prototype;
      const setter = Object.getOwnPropertyDescriptor(proto, "value")?.set;
      if (setter) {
        setter.call(el, next);
      } else {
        el.value = next;
      }
      try {
        el.dispatchEvent(new Event("input", {bubbles: true}));
        el.dispatchEvent(new Event("change", {bubbles: true}));
      } catch (e) {}
      return { ok: true, selector: resolved.selector };
    }

    if (el.isContentEditable) {
      if (!append) el.textContent = "";
      el.textContent = append ? el.textContent + String(inputText) : String(inputText);
      try {
        el.dispatchEvent(new Event("input", {bubbles: true}));
      } catch (e) {}
      return { ok: true, selector: resolved.selector };
    }

    // Last resort
    el.textContent = append ? (el.textContent || "") + String(inputText) : String(inputText);
    return { ok: true, selector: resolved.selector };
  };

  WebNav.scroll = function(yPx, xPx, elementTarget, emit) {
    const SCROLL_Y_PX = yPx || 0;
    const SCROLL_X_PX = xPx || 0;
    const EMIT = emit !== false;
    let el = null;
    if (elementTarget && elementTarget.nodeType === 1) {
      el = elementTarget;
    } else if (typeof elementTarget === "string" && elementTarget !== "") {
      el = document.querySelector(elementTarget);
      if (!el) {
        throw new Error("no element matched selector: " + elementTarget);
      }
    } else {
      el = document.scrollingElement || document.documentElement;
    }

    if (elementTarget && (typeof elementTarget === "string" || elementTarget.nodeType === 1)) {
      try {
        el.scrollBy({ top: SCROLL_Y_PX, left: SCROLL_X_PX, behavior: "instant" });
      } catch (e) {
        try {
          el.scrollBy({ top: SCROLL_Y_PX, left: SCROLL_X_PX, behavior: "auto" });
        } catch (e2) {
          el.scrollTop = (el.scrollTop || 0) + SCROLL_Y_PX;
          el.scrollLeft = (el.scrollLeft || 0) + SCROLL_X_PX;
        }
      }
    } else {
      try {
        window.scrollBy({ top: SCROLL_Y_PX, left: SCROLL_X_PX, behavior: "instant" });
      } catch (e) {
        window.scrollBy({ top: SCROLL_Y_PX, left: SCROLL_X_PX, behavior: "auto" });
      }
    }

    if (EMIT) {
      const evt = new Event("scroll", { bubbles: true });
      if (elementTarget && (typeof elementTarget === "string" || elementTarget.nodeType === 1)) {
        el.dispatchEvent(evt);
      } else {
        window.dispatchEvent(evt);
        document.dispatchEvent(evt);
      }
    }

    return { scrollTop: el.scrollTop, scrollLeft: el.scrollLeft };
  };

  WebNav.read = async function(opts) {
    opts = opts || {};
    function sleep(ms) { return new Promise(function(r){ setTimeout(r, ms); }); }

    var waitMs = Number(opts.waitMs || 0);
    var rootSelector = (opts.rootSelector === undefined || opts.rootSelector === null || opts.rootSelector === "") ? null : String(opts.rootSelector);
    var hasTextRaw = (opts.hasText === undefined || opts.hasText === null) ? "" : String(opts.hasText);
    var hasValueRaw = (opts.attValue === undefined || opts.attValue === null) ? "" : String(opts.attValue);
    var classLimit = Number(opts.classLimit || 3);
    if (waitMs > 0) await sleep(waitMs);

    function normalize(s) { return String(s || "").replace(/\\s+/g, " ").trim(); }

    function formatHref(href) {
      try {
        var u = new URL(href, location.href);
        if (u.origin === location.origin) {
          return u.pathname + u.search + u.hash;
        }
        return u.href;
      } catch (e) {
        return href || "";
      }
    }

    function isVisible(_el) { return true; }

    var inlineTextTags = new Set(["h1","h2","h3","h4","h5","h6","p","li","label","button","span","strong","em","small","blockquote","figcaption","dt","dd"]);
    var containerTags = new Set(["div","main","header","nav","section","article","aside","footer","ul","ol","figure","form","fieldset"]);
    var ignoredTags = new Set(["script","style","noscript"]);

    var lines = [];
    function emit(level, line) {
      var text = normalize(line || "");
      if (!text) return;
      lines.push(Array(level + 1).join("\\t") + text);
    }
    function emitRawLine(level, line) {
      var text = String(line || "").replace(/\\s+$/, "");
      if (!text) return;
      lines.push(Array(level + 1).join("\\t") + text);
    }

    function imgInline(el) {
      var src = el.getAttribute("src") || "";
      var alt = el.getAttribute("alt") || el.getAttribute("aria-label") || "";
      var parts = [];
      if (src) parts.push("src=" + formatHref(src));
      if (alt) parts.push("alt=" + JSON.stringify(alt));
      return ("img " + parts.join(" ")).trim();
    }

    function inlineContent(node) {
      if (!node) return "";
      if (node.nodeType === Node.TEXT_NODE) return node.nodeValue || "";
      if (node.nodeType !== Node.ELEMENT_NODE) return "";

      var tag = node.tagName.toLowerCase();
      if (ignoredTags.has(tag)) return "";

      if (tag === "a") {
        var t = normalize(Array.from(node.childNodes).map(inlineContent).join(""));
        var href = node.getAttribute("href") || node.href || "";
        if (!t) return "";
        if (href) return "<a href=" + formatHref(href) + ">" + t + "</a>";
        return t;
      }

      if (tag === "img") {
        return imgInline(node);
      }

      return Array.from(node.childNodes).map(inlineContent).join("");
    }

    function hasDataAttrs(el) {
      var names = el.getAttributeNames();
      for (var i = 0; i < names.length; i++) {
        if (names[i].indexOf("data-") === 0) return true;
      }
      return false;
    }

    function shouldFlattenDiv(el) {
      if (el.tagName.toLowerCase() !== "div") return false;
      var id = el.getAttribute("id");
      var role = el.getAttribute("role");
      var aria = el.getAttribute("aria-label");
      var cls = el.getAttribute("class");
      if (id || role || aria || cls) return false;
      if (hasDataAttrs(el)) return false;
      return true;
    }

    function containerLabel(el) {
      var tag = el.tagName.toLowerCase();
      var id = el.getAttribute("id");
      var cls = (el.getAttribute("class") || "").trim();
      var role = el.getAttribute("role");
      var dataRole = el.getAttribute("data-role");
      var draggableAttr = el.getAttribute("draggable");

      var parts = [tag];
      if (id) parts.push("#" + id);
      if (cls) {
        var clsShort = cls.split(/\\s+/).slice(0, classLimit).join(".");
        parts.push("." + clsShort);
      }
      if (role) parts.push("[role=" + role + "]");
      if (dataRole) parts.push("[data-role=" + dataRole + "]");
      if (draggableAttr !== null && draggableAttr !== "auto") {
        var val = draggableAttr === "" ? "true" : draggableAttr;
        parts.push("[draggable=" + val + "]");
      }
      return parts.join("");
    }

    function buildRegex(value) {
      if (!value) return null;
      if (value[0] === "/" && value.lastIndexOf("/") > 0) {
        var last = value.lastIndexOf("/");
        var pattern = value.slice(1, last);
        var flags = value.slice(last + 1);
        try { return new RegExp(pattern, flags); } catch (e) { return new RegExp(value); }
      }
      var escaped = value.replace(/[.*+?^${}()|[\\]\\\\]/g, "\\\\$&");
      return new RegExp(escaped);
    }

    var hasTextRegex = buildRegex(hasTextRaw);
    var hasValueRegex = buildRegex(hasValueRaw);
    var includeSet = null;
    var matchInfo = null;
    var displaySelector = rootSelector ? rootSelector.replace(/\\\\\\//g, "/") : "";
    var noMatchLine = rootSelector ? ("no matches in the DOM for " + displaySelector) : "no-matches";

    function isSimpleClassSelector(sel) {
      if (!sel) return false;
      if (/[\\s>#\\[:]/.test(sel)) return false;
      if (sel.indexOf(".") === -1) return false;
      return true;
    }

    function escapeSelectorSlashes(sel) { return sel.replace(/\\//g, "\\\\/"); }

    function suggestFallbackSelector(sel) {
      if (!isSimpleClassSelector(sel)) return null;
      var parts = sel.split(".");
      var tag = parts[0] || "";
      var classes = parts.slice(1).filter(Boolean);
      if (classes.length === 0) return null;
      var attempts = 0;
      var currentClasses = classes.slice();
      while (attempts < 2 && currentClasses.length > 1) {
        currentClasses = currentClasses.slice(0, -1);
        var candidateDisplay = (tag ? tag : "") + "." + currentClasses.join(".");
        var candidate = escapeSelectorSlashes(candidateDisplay);
        var matches = Array.from(document.querySelectorAll(candidate));
        if (matches.length > 0) {
          return { selector: candidateDisplay, matches: matches };
        }
        attempts += 1;
      }
      return null;
    }

    function shouldSerializeElement(el) {
      if (!el || el.nodeType !== Node.ELEMENT_NODE) return false;
      var tag = el.tagName.toLowerCase();
      if (ignoredTags.has(tag)) return false;
      if (el.classList && el.classList.contains("web-nav-hidden")) return false;
      if (tag !== "body" && !isVisible(el)) return false;
      if (includeSet && !includeSet.has(el)) return false;
      return true;
    }

    function buildIncludeSet(root) {
      if (!hasTextRegex && !hasValueRegex) { includeSet = null; matchInfo = null; return true; }
      var set = new Set();
      var matches = [];
      var info = new Map();
      var all = [root].concat(Array.from(root.querySelectorAll("*")));
      for (var i = 0; i < all.length; i++) {
        var el = all[i];
        if (!isVisible(el)) continue;
        var matched = false;
        var text = (el.textContent || "").trim();
        if (hasTextRegex && text && hasTextRegex.test(text)) {
          matches.push(el);
          info.set(el, { kind: "text" });
          matched = true;
        }
        if (!matched && hasValueRegex) {
          var names = el.getAttributeNames();
          for (var j = 0; j < names.length; j++) {
            var name = names[j];
            var val = el.getAttribute(name) || "";
            if (val && hasValueRegex.test(val)) {
              matches.push(el);
              info.set(el, { kind: "attr", name: name });
              matched = true;
              break;
            }
          }
        }
      }
      if (matches.length === 0) {
        includeSet = null;
        matchInfo = null;
        return false;
      }
      for (var k = 0; k < matches.length; k++) {
        var cur = matches[k];
        while (cur && cur !== root) {
          set.add(cur);
          cur = cur.parentElement;
        }
      }
      set.add(root);
      includeSet = set;
      matchInfo = info;
      return true;
    }

    function emitPre(el, level) {
      var raw = String(el.textContent || "").replace(/\\r\\n?/g, "\\n");
      var parts = raw.split("\\n");
      if (parts.length <= 1) {
        var one = normalize(raw);
        if (one) emit(level, "pre: " + one);
        return;
      }
      emit(level, "pre:");
      for (var i = 0; i < parts.length; i++) {
        if (parts[i] === "") continue;
        emitRawLine(level + 1, parts[i]);
      }
    }

    function serialize(el, level) {
      if (!el || el.nodeType !== Node.ELEMENT_NODE) return;
      var tag = el.tagName.toLowerCase();
      if (!shouldSerializeElement(el)) return;

      if (tag === "body") {
        var kids = Array.from(el.children);
        for (var i = 0; i < kids.length; i++) serialize(kids[i], level);
        return;
      }

      if (tag === "hr") { emit(level, "hr"); return; }
      if (tag === "canvas") { emit(level, "<canvas>"); return; }

      if (tag === "img") {
        var noteImg = (matchInfo && matchInfo.get(el) && matchInfo.get(el).kind === "attr") ? (" [match attr=" + matchInfo.get(el).name + "]") : "";
        emit(level, imgInline(el) + noteImg);
        return;
      }

      if (tag === "input") {
        var type = el.getAttribute("type") || "text";
        var name = el.getAttribute("name") || "";
        var placeholder = el.getAttribute("placeholder") || "";
        var value = (el.value !== undefined && el.value !== null) ? String(el.value) : (el.getAttribute("value") || "");
        var p = ["type=" + type];
        if (name) p.push("name=" + name);
        if (placeholder) p.push("placeholder=" + JSON.stringify(placeholder));
        p.push("value=" + JSON.stringify(value));
        emit(level, "input: " + p.join(" "));
        return;
      }

      if (tag === "textarea") {
        var name2 = el.getAttribute("name") || "";
        var placeholder2 = el.getAttribute("placeholder") || "";
        var value2 = (el.value !== undefined && el.value !== null) ? String(el.value) : (el.textContent || "");
        var p2 = [];
        if (name2) p2.push("name=" + name2);
        if (placeholder2) p2.push("placeholder=" + JSON.stringify(placeholder2));
        p2.push("value=" + JSON.stringify(value2));
        emit(level, "textarea: " + p2.join(" "));
        return;
      }

      if (tag === "select") {
        var name3 = el.getAttribute("name") || "";
        var p3 = [];
        if (name3) p3.push("name=" + name3);
        emit(level, ("select: " + p3.join(" ")).trim());
        var opts2 = Array.from(el.querySelectorAll("option"));
        for (var i = 0; i < opts2.length; i++) {
          var opt = opts2[i];
          var val2 = opt.getAttribute("value") || "";
          var text2 = normalize(opt.textContent || "");
          var sel2 = opt.selected ? " selected" : "";
          var optParts = [];
          if (val2) optParts.push("value=" + val2);
          var line = ("option: " + text2 + sel2 + (optParts.length ? (" " + optParts.join(" ")) : "")).trim();
          emit(level + 1, line);
        }
        return;
      }

      if (tag === "pre") { emitPre(el, level); return; }

      if (tag === "a") {
        var href = el.getAttribute("href") || el.href || "";
        var text3 = normalize(Array.from(el.childNodes).map(inlineContent).join(""));
        var imgs = Array.from(el.querySelectorAll("img"));
        var noteA = (matchInfo && matchInfo.get(el) && matchInfo.get(el).kind === "attr") ? (" [match attr=" + matchInfo.get(el).name + "]") : "";
        if (imgs.length && !text3) {
          var imgText = imgInline(imgs[0]);
          emit(level, "a href=" + formatHref(href) + ": " + imgText + noteA);
        } else if (text3) {
          emit(level, "a href=" + formatHref(href) + ": " + text3 + noteA);
        } else if (noteA) {
          emit(level, "a href=" + formatHref(href) + ":" + noteA);
        }
        return;
      }

      if (inlineTextTags.has(tag)) {
        var content = normalize(Array.from(el.childNodes).map(inlineContent).join(""));
        var noteT = (matchInfo && matchInfo.get(el) && matchInfo.get(el).kind === "attr") ? (" [match attr=" + matchInfo.get(el).name + "]") : "";
        if (content) emit(level, tag + ": " + content + noteT);
        else if (noteT) emit(level, tag + ":" + noteT);
        return;
      }

      if (tag === "div" && shouldFlattenDiv(el)) {
        var kids2 = Array.from(el.children);
        for (var i = 0; i < kids2.length; i++) serialize(kids2[i], level);
        return;
      }

      if (containerTags.has(tag)) {
        var label = containerLabel(el);
        var children = Array.from(el.children).filter(function(c){ return c.nodeType === Node.ELEMENT_NODE; });
        var noteC = (matchInfo && matchInfo.get(el) && matchInfo.get(el).kind === "attr") ? (" [match attr=" + matchInfo.get(el).name + "]") : "";
        if (children.length === 0) {
          var content2 = normalize(Array.from(el.childNodes).map(inlineContent).join(""));
          if (content2) {
            emit(level, label + ": " + content2 + noteC);
          } else {
            emit(level, "<" + label + "></" + tag + ">");
          }
          return;
        }
        emit(level, label + ":" + noteC);
        var hiddenCount = 0;
        for (var i = 0; i < children.length; i++) {
          var child = children[i];
          if ((hasTextRegex || hasValueRegex) && includeSet && !includeSet.has(child)) {
            var childTag = child.tagName ? child.tagName.toLowerCase() : "";
            if (!ignoredTags.has(childTag)) hiddenCount += 1;
            continue;
          }
          serialize(child, level + 1);
        }
        if (hiddenCount > 0) {
          emit(level + 1, "[" + hiddenCount + " siblings not shown]");
        }
        return;
      }

      var content3 = normalize(Array.from(el.childNodes).map(inlineContent).join(""));
      if (content3) emit(level, tag + ": " + content3);
    }

    if (!rootSelector) {
      emit(0, "title: " + normalize(document.title || "Untitled"));
      emit(0, "url: " + location.href);
      emit(0, "");
    }

    var roots = rootSelector ? Array.from(document.querySelectorAll(rootSelector)) : [document.body];
    var uniqueRoots = [];
    for (var i = 0; i < roots.length; i++) {
      var el = roots[i];
      var nested = false;
      for (var j = 0; j < uniqueRoots.length; j++) {
        if (uniqueRoots[j].contains(el)) { nested = true; break; }
      }
      if (!nested) uniqueRoots.push(el);
    }

    if (uniqueRoots.length === 0) {
      emit(0, noMatchLine);
      var suggestion = suggestFallbackSelector(displaySelector);
      if (suggestion) {
        if (suggestion.matches.length === 1) {
          emit(0, "did you mean \"" + suggestion.selector + "\", which has 1 match:");
          serialize(suggestion.matches[0], 1);
        } else {
          emit(0, "did you mean \"" + suggestion.selector + "\", which has " + suggestion.matches.length + " matches");
          emit(0, "first match:");
          serialize(suggestion.matches[0], 1);
        }
      }
    } else {
      var renderedRoots = [];
      for (var i = 0; i < uniqueRoots.length; i++) {
        var root = uniqueRoots[i];
        if (hasTextRegex || hasValueRegex) {
          var ok = buildIncludeSet(root);
          if (!ok) continue;
        } else {
          includeSet = null;
          matchInfo = null;
        }
        renderedRoots.push(root);
      }

      if (renderedRoots.length === 0) {
        emit(0, noMatchLine);
        var suggestion2 = suggestFallbackSelector(displaySelector);
        if (suggestion2) {
          if (suggestion2.matches.length === 1) {
            emit(0, "did you mean \"" + suggestion2.selector + "\", which has 1 match:");
            serialize(suggestion2.matches[0], 1);
          } else {
            emit(0, "did you mean \"" + suggestion2.selector + "\", which has " + suggestion2.matches.length + " matches");
            emit(0, "first match:");
            serialize(suggestion2.matches[0], 1);
          }
        }
      } else if (renderedRoots.length === 1) {
        serialize(renderedRoots[0], 0);
      } else {
        var idx = 1;
        for (var i = 0; i < renderedRoots.length; i++) {
          var node = renderedRoots[i];
          if (hasTextRegex || hasValueRegex) buildIncludeSet(node);
          emit(0, "match: " + idx);
          serialize(node, 1);
          idx += 1;
        }
      }
    }

    return { url: location.href, title: document.title, lines: lines };
  };

  window.WebNav = WebNav;
  window.WebNavClick = WebNav.click;
  window.WebNavHover = WebNav.hover;
  window.WebNavDrag = WebNav.drag;
  window.WebNavGesture = WebNav.gesture;
  window.WebNavKey = WebNav.key;
  window.WebNavType = WebNav.type;
  window.WebNavTypePrepare = WebNav.typePrepare;
  window.WebNavTypeFallback = WebNav.typeFallback;
  window.WebNavScroll = WebNav.scroll;
  window.WebNavFocus = WebNav.focus;
  window.WebNavRead = WebNav.read;
  window.WebNavInjected = true;
})();`

func ensureWebNavInjected(ctx context.Context, client *cdp.Client) error {
	ok, err := isWebNavInjected(ctx, client)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return injectWebNav(ctx, client, true)
}

func isWebNavInjected(ctx context.Context, client *cdp.Client) (bool, error) {
	value, err := client.Evaluate(ctx, `(() => !!window.WebNavInjected)()`)
	if err != nil {
		return false, err
	}
	ok, _ := value.(bool)
	return ok, nil
}

func injectWebNav(ctx context.Context, client *cdp.Client, force bool) error {
	if !force {
		ok, err := isWebNavInjected(ctx, client)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	if _, err := client.Evaluate(ctx, webNavScript); err != nil {
		return fmt.Errorf("webnav inject failed: %w", err)
	}
	return nil
}
