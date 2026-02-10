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
