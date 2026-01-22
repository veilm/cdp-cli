package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
)

// Client is a lightweight CDP transport layer.
type Client struct {
	conn      *websocket.Conn
	pendingMu sync.Mutex
	pending   map[int64]chan response

	eventMu       sync.RWMutex
	eventHandlers map[int64]func(Event)
	handlerID     int64

	nextID    int64
	readCtx   context.Context
	cancel    context.CancelFunc
	closed    chan struct{}
	closeOnce sync.Once
}

// Event represents an async CDP notification.
type Event struct {
	Method string
	Params json.RawMessage
}

type response struct {
	payload responsePayload
	err     error
}

type responsePayload struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

// Error is a CDP protocol error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func (e *Error) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("cdp error %d: %s (%s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("cdp error %d: %s", e.Code, e.Message)
}

// Dial establishes a websocket connection to the DevTools target.
func Dial(ctx context.Context, wsURL string) (*Client, error) {
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(math.MaxInt64)
	readCtx, cancel := context.WithCancel(context.Background())
	c := &Client{
		conn:          conn,
		pending:       make(map[int64]chan response),
		eventHandlers: make(map[int64]func(Event)),
		readCtx:       readCtx,
		cancel:        cancel,
		closed:        make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// Close tears down the websocket connection.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.cancel()
		err = c.conn.Close(websocket.StatusNormalClosure, "")
		<-c.closed
	})
	return err
}

// Call sends a protocol command and decodes the response.
func (c *Client) Call(ctx context.Context, method string, params interface{}, result interface{}) error {
	id := atomic.AddInt64(&c.nextID, 1)
	payload := map[string]interface{}{
		"id":     id,
		"method": method,
	}
	if params != nil {
		payload["params"] = params
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	ch := make(chan response, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		c.removePending(id)
		return err
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case resp := <-ch:
		if resp.err != nil {
			return resp.err
		}
		if resp.payload.Error != nil {
			return resp.payload.Error
		}
		if result == nil {
			return nil
		}
		if len(resp.payload.Result) == 0 {
			return nil
		}
		return json.Unmarshal(resp.payload.Result, result)
	}
}

func (c *Client) removePending(id int64) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pending, id)
}

func (c *Client) readLoop() {
	defer close(c.closed)
	for {
		_, data, err := c.conn.Read(c.readCtx)
		if err != nil {
			c.failAll(err)
			return
		}
		var probe struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			continue
		}
		if probe.ID != nil {
			var payload responsePayload
			if err := json.Unmarshal(data, &payload); err != nil {
				payload = responsePayload{}
			}
			c.pendingMu.Lock()
			ch, ok := c.pending[*probe.ID]
			if ok {
				delete(c.pending, *probe.ID)
			}
			c.pendingMu.Unlock()
			if ok {
				ch <- response{payload: payload}
			}
			continue
		}
		if probe.Method != "" {
			c.dispatchEvent(Event{Method: probe.Method, Params: probe.Params})
		}
	}
}

func (c *Client) failAll(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		ch <- response{err: err}
		delete(c.pending, id)
	}
}

func (c *Client) dispatchEvent(evt Event) {
	c.eventMu.RLock()
	handlers := make([]func(Event), 0, len(c.eventHandlers))
	for _, h := range c.eventHandlers {
		handlers = append(handlers, h)
	}
	c.eventMu.RUnlock()
	for _, h := range handlers {
		h(evt)
	}
}

// SubscribeEvents registers a callback for CDP events. Returns a function to unsubscribe.
func (c *Client) SubscribeEvents(fn func(Event)) func() {
	id := atomic.AddInt64(&c.handlerID, 1)
	c.eventMu.Lock()
	c.eventHandlers[id] = fn
	c.eventMu.Unlock()
	return func() {
		c.eventMu.Lock()
		delete(c.eventHandlers, id)
		c.eventMu.Unlock()
	}
}

// RuntimeEvaluateResult contains the response from Runtime.evaluate.
type RuntimeEvaluateResult struct {
	Result           RemoteObject      `json:"result"`
	ExceptionDetails *ExceptionDetails `json:"exceptionDetails"`
}

// RemoteObject is a subset of Runtime.RemoteObject.
type RemoteObject struct {
	Type                string           `json:"type"`
	Subtype             string           `json:"subtype"`
	Value               *json.RawMessage `json:"value"`
	UnserializableValue string           `json:"unserializableValue"`
	Description         string           `json:"description"`
	ObjectID            string           `json:"objectId"`
}

// ExceptionDetails are returned on script errors.
type ExceptionDetails struct {
	Text      string        `json:"text"`
	Exception *RemoteObject `json:"exception"`
}

// EvaluateRaw evaluates JS inside the target and returns the raw CDP response.
func (c *Client) EvaluateRaw(ctx context.Context, expression string, returnByValue bool) (RuntimeEvaluateResult, error) {
	var res RuntimeEvaluateResult
	err := c.Call(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":                  expression,
		"returnByValue":               returnByValue,
		"awaitPromise":                true,
		"userGesture":                 true,
		"replMode":                    true,
		"allowUnsafeEvalBlockedByCSP": true,
	}, &res)
	if err != nil {
		return res, err
	}
	if res.ExceptionDetails != nil {
		msg := strings.TrimSpace(res.ExceptionDetails.Text)
		var detail string
		if res.ExceptionDetails.Exception != nil {
			if d, err := c.RemoteObjectValue(ctx, *res.ExceptionDetails.Exception); err == nil && d != nil {
				if m, ok := d.(map[string]interface{}); !ok || len(m) > 0 {
					detail = strings.TrimSpace(fmt.Sprint(d))
				}
			}
			if detail == "" {
				detail = strings.TrimSpace(res.ExceptionDetails.Exception.Description)
			}
		}
		if msg == "" && detail != "" {
			msg = detail
		}
		if msg == "" {
			msg = "runtime exception"
		} else if detail != "" && detail != msg {
			msg = fmt.Sprintf("%s (%s)", msg, detail)
		}
		return res, errors.New(msg)
	}
	return res, nil
}

// Evaluate evaluates JS inside the target and resolves the resulting object into Go values.
func (c *Client) Evaluate(ctx context.Context, expression string) (interface{}, error) {
	res, err := c.EvaluateRaw(ctx, expression, true)
	if err != nil {
		return nil, err
	}
	return c.RemoteObjectValue(ctx, res.Result)
}

// RemoteObjectValue resolves a RemoteObject into a native Go value.
func (c *Client) RemoteObjectValue(ctx context.Context, obj RemoteObject) (interface{}, error) {
	if obj.Value != nil {
		var out interface{}
		if err := json.Unmarshal(*obj.Value, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	if obj.UnserializableValue != "" {
		return obj.UnserializableValue, nil
	}
	if obj.ObjectID != "" {
		var call struct {
			Result           RemoteObject      `json:"result"`
			ExceptionDetails *ExceptionDetails `json:"exceptionDetails"`
		}
		err := c.Call(ctx, "Runtime.callFunctionOn", map[string]interface{}{
			"objectId": obj.ObjectID,
			"functionDeclaration": `function() {
                try {
                    if (typeof DOMRect !== 'undefined' && (this instanceof DOMRect || this instanceof DOMRectReadOnly)) {
                        return {
                            x: this.x,
                            y: this.y,
                            width: this.width,
                            height: this.height,
                            top: this.top,
                            right: this.right,
                            bottom: this.bottom,
                            left: this.left,
                        };
                    }
                } catch (e) {}
                try {
                    const json = JSON.stringify(this);
                    if (json !== undefined) {
                        return JSON.parse(json);
                    }
                } catch (e) {}
                if (this && typeof this === 'object' && typeof this.outerHTML === 'string') {
                    return this.outerHTML;
                }
                try { return String(this); } catch (e) {}
                return null;
            }`,
			"returnByValue": true,
		}, &call)
		if err != nil {
			if obj.Description != "" {
				return obj.Description, nil
			}
			return nil, err
		}
		if call.ExceptionDetails != nil {
			if obj.Description != "" {
				return obj.Description, nil
			}
			return nil, errors.New(call.ExceptionDetails.Text)
		}
		return c.RemoteObjectValue(ctx, call.Result)
	}
	if obj.Description != "" {
		return obj.Description, nil
	}
	return obj.Type, nil
}
