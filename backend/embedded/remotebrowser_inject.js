(function () {
  var wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  var ws = new WebSocket(wsProto + '//' + window.location.host + '/__WS_PATH__/__CR_ID__/__RB_ID__');
  ws.binaryType = 'arraybuffer'; // stream frames arrive as raw binary JPEG messages
  var h = {};              // event handlers keyed as "e:eventName" or "stream_start:name" etc.
  var streams = {};        // name → {canvas, w, h, cssW, cssH, autoSize, el}
  var streamLastStart = {} // name → last stream_start message, so mountStream called late still sizes correctly

  ws.onopen = function () {
    ws.send(JSON.stringify({ type: 'viewport', width: window.innerWidth, height: window.innerHeight, dpr: window.devicePixelRatio || 1, screenWidth: screen.width, screenHeight: screen.height }));
  };

  // Apply stream_start sizing to an already-mounted stream entry.
  function applyStreamStart(st, m) {
    st.canvas.width  = m.width;
    st.canvas.height = m.height;
    st.w = m.width;
    st.h = m.height;
    // Display size = element's true CSS-pixel dimensions.
    // Replace cssText entirely so there is no max-width left to fight against.
    var dw = m.cssWidth  || m.width;
    var dh = m.cssHeight || m.height;
    st.cssW = dw;
    st.cssH = dh;
    st.canvas.style.cssText = 'display:block;outline:none;width:' + dw + 'px;height:' + dh + 'px;';
    if (st.autoSize) {
      st.el.style.width  = dw + 'px';
      st.el.style.height = dh + 'px';
    }
  }

  // handleStreamFrame draws a binary frame message: [type=1][uint16 nameLen][name][jpeg].
  // Decoding runs off the main thread via createImageBitmap; if newer frames arrive
  // while one is decoding they replace the pending one so only the latest is drawn.
  function handleStreamFrame(buf) {
    var dv = new DataView(buf);
    if (dv.getUint8(0) !== 1) return;
    var nameLen = dv.getUint16(1);
    var name = new TextDecoder().decode(new Uint8Array(buf, 3, nameLen));
    var st = streams[name];
    if (!st) return;
    st.pending = new Uint8Array(buf, 3 + nameLen);
    if (st.decoding) return;
    st.decoding = true;
    (function drain() {
      var bytes = st.pending;
      st.pending = null;
      createImageBitmap(new Blob([bytes])).then(function (bmp) {
        if (st.canvas.width !== bmp.width) { st.canvas.width = bmp.width; st.w = bmp.width; }
        if (st.canvas.height !== bmp.height) { st.canvas.height = bmp.height; st.h = bmp.height; }
        st.canvas.getContext('2d').drawImage(bmp, 0, 0);
        bmp.close();
        if (st.pending) { drain(); } else { st.decoding = false; }
      }).catch(function () {
        if (st.pending) { drain(); } else { st.decoding = false; }
      });
    })();
  }

  ws.onmessage = function (e) {
    // Binary messages are raw JPEG stream frames.
    if (typeof e.data !== 'string') {
      handleStreamFrame(e.data);
      return;
    }
    try {
      var m = JSON.parse(e.data);

      if (m.type === 'event' && m.key) {
        (h['e:' + m.key] || []).forEach(function (f) { f(m.value); });

      } else if (m.type === 'stream_start' && m.name) {
        // Always store so mountStream() called inside the handler still gets sized.
        streamLastStart[m.name] = m;
        var st = streams[m.name];
        if (st) {
          // Stream already mounted — this is a resize/reposition update only.
          // Do NOT re-fire user handlers; that would call mountStream() again
          // and create duplicate canvases.
          applyStreamStart(st, m);
        } else {
          // First stream_start for this name: fire user handlers so the page can
          // call mountStream() to attach a canvas.
          (h['stream_start:' + m.name] || []).forEach(function (f) {
            f(m.cssWidth || m.width, m.cssHeight || m.height);
          });
          // If the handler called mountStream() just now, apply sizing immediately.
          if (streams[m.name]) {
            applyStreamStart(streams[m.name], m);
          }
        }

      } else if (m.type === 'done') {
        (h['e:done'] || []).forEach(function (f) { f(); });

      } else if (m.type === 'stream_stop' && m.name) {
        // Remove the canvas from DOM and clear the tracking entry so the next
        // stream_start for the same name triggers a fresh mountStream() call.
        // Without this, a stop→start cycle (e.g. element removed and re-added)
        // leaves a stale canvas in `streams` that silently receives frames while
        // subsequent mountStream() calls add new canvases on top.
        var stStopped = streams[m.name];
        if (stStopped && stStopped.canvas && stStopped.canvas.parentNode) {
          stStopped.canvas.parentNode.removeChild(stStopped.canvas);
        }
        delete streams[m.name];
        delete streamLastStart[m.name];
        (h['stream_stop:' + m.name] || []).forEach(function (f) { f(); });
      }
    } catch (ex) {}
  };

  window.remoteBrowser = {
    on: function (ev, nameOrFn, fn) {
      if (typeof nameOrFn === 'function') {
        h['e:' + ev] = h['e:' + ev] || [];
        h['e:' + ev].push(nameOrFn);
      } else {
        var k = ev + ':' + nameOrFn;
        h[k] = h[k] || [];
        h[k].push(fn);
      }
    },

    send: function (ev, data) {
      if (ws.readyState === 1) ws.send(JSON.stringify({ event: ev, data: data || {} }));
    },

    mountStream: function (name, el, opts) {
      // stream_start fires on every viewport/JPEG-dimension change; guard against
      // appending a second canvas if the stream is already mounted.
      if (streams[name]) return;

      var autoSize    = !!(opts && opts.autoSize);
      var allowScroll = !!(opts && opts.scroll);
      var allowArrows = !!(opts && opts.arrowKeys);
      var ARROW_KEYS  = { ArrowUp: 1, ArrowDown: 1, ArrowLeft: 1, ArrowRight: 1 };

      var canvas = document.createElement('canvas');
      canvas.style.cssText = 'display:block;outline:none;';
      canvas.setAttribute('tabindex', '0');
      el.appendChild(canvas);

      var st = { canvas: canvas, w: 0, h: 0, autoSize: autoSize, el: el };
      streams[name] = st;

      // If stream_start already arrived (e.g. mountStream called inside the handler),
      // apply the stored sizing now so the canvas has the right CSS dimensions immediately.
      if (streamLastStart[name]) {
        applyStreamStart(st, streamLastStart[name]);
      }

      function coords(e) {
        var r  = canvas.getBoundingClientRect();
        var sx = st.w > 0 ? st.w / r.width  : 1;
        var sy = st.h > 0 ? st.h / r.height : 1;
        return { x: Math.round((e.clientX - r.left) * sx), y: Math.round((e.clientY - r.top) * sy) };
      }

      function snd(o) {
        if (ws.readyState === 1) ws.send(JSON.stringify(o));
      }

      canvas.addEventListener('mousedown', function (e) {
        e.preventDefault();
        canvas.focus();
        var p = coords(e);
        snd({ type: 'stream_input', name: name, action: 'mousedown', x: p.x, y: p.y,
              button: e.button === 2 ? 'right' : 'left' });
      });

      canvas.addEventListener('mouseup', function (e) {
        var p = coords(e);
        snd({ type: 'stream_input', name: name, action: 'mouseup', x: p.x, y: p.y,
              button: e.button === 2 ? 'right' : 'left' });
      });

      canvas.addEventListener('mousemove', function (e) {
        var p = coords(e);
        snd({ type: 'stream_input', name: name, action: 'mousemove', x: p.x, y: p.y });
      });

      // Scroll: disabled by default to avoid accidentally scrolling the remote browser.
      // Enable with { scroll: true } in mountStream options.
      if (allowScroll) {
        canvas.addEventListener('wheel', function (e) {
          e.preventDefault();
          var p = coords(e);
          snd({ type: 'stream_input', name: name, action: 'scroll', x: p.x, y: p.y,
                deltaX: e.deltaX, deltaY: e.deltaY });
        }, { passive: false });
      }

      // isPasteCombo reports the paste shortcut. Its default action must run so the
      // browser fires a paste event, and the key itself must not be forwarded: the
      // remote browser would paste from its own clipboard, which lives on the
      // server and holds nothing the person in front of this page copied.
      // Shift+Insert pastes on Linux and Windows just like Ctrl+V.
      function isPasteCombo(e) {
        if ((e.ctrlKey || e.metaKey) && !e.altKey && (e.key === 'v' || e.key === 'V')) return true;
        return e.shiftKey && !e.ctrlKey && !e.metaKey && e.key === 'Insert';
      }

      // Releasing the modifier before the letter leaves a keyup whose modifier
      // flags no longer say paste. Forwarding it would send the remote page a
      // keyup with no matching keydown, so remember which keys were held back.
      var suppressedKeys = {};

      // Arrow keys: always preventDefault (prevent page scroll when canvas is focused),
      // but only forwarded to the remote browser when { arrowKeys: true }.
      canvas.addEventListener('keydown', function (e) {
        if (isPasteCombo(e)) {
          suppressedKeys[e.code || e.key] = true;
          return;
        }
        var isArrow = !!ARROW_KEYS[e.key];
        e.preventDefault();
        if (isArrow && !allowArrows) return;
        snd({ type: 'stream_input', name: name, action: 'keydown',
              key: e.key, code: e.code, keyCode: e.keyCode,
              modifiers: (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0),
              charText: (e.ctrlKey || e.metaKey) ? '' : (e.key === 'Enter' ? '\r' : e.key.length === 1 ? e.key : '') });
      });

      canvas.addEventListener('keyup', function (e) {
        var held = e.code || e.key;
        if (isPasteCombo(e) || suppressedKeys[held]) {
          delete suppressedKeys[held];
          return;
        }
        var isArrow = !!ARROW_KEYS[e.key];
        e.preventDefault();
        if (isArrow && !allowArrows) return;
        snd({ type: 'stream_input', name: name, action: 'keyup',
              key: e.key, code: e.code, keyCode: e.keyCode,
              modifiers: (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0) });
      });

      // Paste carries its text on the event itself, so filling a form from a
      // password manager or from a copied address works without asking for
      // clipboard permission, which would put a browser prompt in front of the
      // page. Only what is pasted into this canvas is read, never the clipboard
      // on its own. The server turns it into Input.insertText on the remote page.
      canvas.addEventListener('paste', function (e) {
        e.preventDefault();
        var data = e.clipboardData || window.clipboardData;
        var text = data ? data.getData('text') : '';
        if (!text) return;
        snd({ type: 'stream_input', name: name, action: 'paste', text: text });
      });

      canvas.addEventListener('contextmenu', function (e) { e.preventDefault(); });
    }
  };

  window.rb = window.remoteBrowser;
})();
