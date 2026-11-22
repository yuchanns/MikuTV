package plugin

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
	"golang.org/x/net/http/httpguts"
)

const (
	wsDialTimeout      = 10 * time.Second
	wsWriteTimeout     = 10 * time.Second
	wsMaxMessageBytes  = 2 << 20
	wsMaxOutboundBytes = 256 << 10
	wsAcceptGUID       = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

type wsBridge struct {
	ctx    context.Context
	loop   *eventloop.Loop
	client *http.Client
	allow  func(*http.Request) error
	secure bool

	mu     sync.Mutex
	active *wsConn
	closed bool
}

func newWSBridge(ctx context.Context, loop *eventloop.Loop, client *http.Client, allow func(*http.Request) error, secure bool) *wsBridge {
	b := &wsBridge{ctx: ctx, loop: loop, client: client, allow: allow, secure: secure}
	return b
}

func (b *wsBridge) closeAll() {
	b.mu.Lock()
	b.closed = true
	active := b.active
	b.active = nil
	b.mu.Unlock()
	if active != nil {
		active.closeNow()
	}
}

func (b *wsBridge) hostObject(rt *moejs.Runtime) map[string]any {
	connect := rt.Function("connect", 2, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := b.ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		rawURL := strings.TrimSpace(moejs.Arg(args, 0).String())
		target, httpURL, err := b.validateURL(rawURL)
		if err != nil {
			return moejs.Undefined(), err
		}
		opts := moejs.Arg(args, 1)
		headers, err := b.parseHeaders(rt, opts, httpURL)
		if err != nil {
			return moejs.Undefined(), err
		}
		onOpen := getCallable(rt, opts, "onOpen")
		onMessage := getCallable(rt, opts, "onMessage")
		onClose := getCallable(rt, opts, "onClose")
		onError := getCallable(rt, opts, "onError")

		conn := &wsConn{
			bridge:    b,
			target:    target,
			headers:   headers,
			onOpen:    onOpen,
			onMessage: onMessage,
			onClose:   onClose,
			onError:   onError,
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return moejs.Undefined(), errors.New("websocket bridge closed")
		}
		prev := b.active
		b.active = conn
		b.mu.Unlock()
		if prev != nil {
			prev.closeNow()
		}

		release := b.loop.Hold()
		go conn.run(release)

		sendFn := rt.Function("send", 1, func(_ *moejs.Realm, _ moejs.Value, sendArgs []moejs.Value) (moejs.Value, error) {
			arg := moejs.Arg(sendArgs, 0)
			if arg.IsString() {
				data := []byte(arg.String())
				if len(data) > wsMaxOutboundBytes {
					return moejs.Undefined(), errors.New("websocket frame exceeds limit")
				}
				return moejs.Undefined(), conn.writeFrame(0x1, data)
			}
			raw, err := rt.ToGo(arg)
			if err != nil {
				return moejs.Undefined(), err
			}
			data, ok := raw.([]byte)
			if !ok {
				return moejs.Undefined(), errors.New("websocket send requires string or byte buffer")
			}
			if len(data) > wsMaxOutboundBytes {
				return moejs.Undefined(), errors.New("websocket frame exceeds limit")
			}
			return moejs.Undefined(), conn.writeFrame(0x2, data)
		})
		closeFn := rt.Function("close", 0, func(_ *moejs.Realm, _ moejs.Value, _ []moejs.Value) (moejs.Value, error) {
			conn.closeGraceful()
			return moejs.Undefined(), nil
		})
		return rt.FromGo(map[string]any{"send": sendFn, "close": closeFn})
	})
	return map[string]any{"connect": connect}
}

func getCallable(rt *moejs.Runtime, obj moejs.Value, key string) moejs.Value {
	if obj.IsUndefined() || obj.IsNull() {
		return moejs.Undefined()
	}
	v, err := rt.Get(obj, key)
	if err != nil || !v.IsObject() || !v.AsObject().IsCallable() {
		return moejs.Undefined()
	}
	return v
}

func (b *wsBridge) validateURL(raw string) (*url.URL, *url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" {
		return nil, nil, errors.New("invalid websocket URL")
	}
	scheme := strings.ToLower(u.Scheme)
	var httpScheme string
	switch scheme {
	case "wss":
		httpScheme = "https"
	case "ws":
		if b.secure {
			return nil, nil, errors.New("secure wss origin required")
		}
		httpScheme = "http"
	default:
		return nil, nil, errors.New("websocket scheme required")
	}
	check := *u
	check.Scheme = httpScheme
	if b.allow != nil {
		if err := b.allow(&http.Request{URL: &check}); err != nil {
			return nil, nil, err
		}
	}
	return u, &check, nil
}

func (b *wsBridge) parseHeaders(rt *moejs.Runtime, opts moejs.Value, httpURL *url.URL) (http.Header, error) {
	headers := make(http.Header)
	if !opts.IsUndefined() && !opts.IsNull() {
		hVal, err := rt.Get(opts, "headers")
		if err != nil {
			return nil, err
		}
		if !hVal.IsUndefined() && !hVal.IsNull() {
			raw, err := rt.ToGo(hVal)
			if err != nil {
				return nil, err
			}
			m, ok := raw.(map[string]any)
			if !ok {
				return nil, errors.New("invalid websocket headers")
			}
			for k, v := range m {
				sv, ok := v.(string)
				if !ok || !httpguts.ValidHeaderFieldValue(sv) {
					return nil, errors.New("invalid websocket header value")
				}
				switch strings.ToLower(k) {
				case "origin", "user-agent", "referer", "accept-language", "sec-websocket-protocol":
					headers.Set(k, sv)
				default:
					return nil, errors.New("disallowed websocket header")
				}
			}
		}
	}
	if b.client != nil && b.client.Jar != nil {
		var pairs []string
		for _, c := range b.client.Jar.Cookies(httpURL) {
			pairs = append(pairs, c.Name+"="+c.Value)
		}
		if len(pairs) > 0 {
			headers.Set("Cookie", strings.Join(pairs, "; "))
		}
	}
	return headers, nil
}

type wsConn struct {
	bridge    *wsBridge
	target    *url.URL
	headers   http.Header
	onOpen    moejs.Value
	onMessage moejs.Value
	onClose   moejs.Value
	onError   moejs.Value

	mu      sync.Mutex
	writeMu sync.Mutex
	netConn net.Conn
	closed  bool
}

func (c *wsConn) run(release func(func(*moejs.Runtime))) {
	var closeCode = 1006
	var closeReason string
	defer func() {
		c.closeNow()
		c.bridge.mu.Lock()
		if c.bridge.active == c {
			c.bridge.active = nil
		}
		c.bridge.mu.Unlock()
		release(func(rt *moejs.Runtime) {
			if !c.onClose.IsUndefined() && c.bridge.ctx.Err() == nil {
				_, _ = rt.CallFunction(c.onClose, moejs.Undefined(), moejs.Int(int64(closeCode)), moejs.String(closeReason))
			}
		})
	}()

	conn, reader, err := c.dial()
	if err != nil {
		closeReason = err.Error()
		c.postError(err.Error())
		return
	}
	c.mu.Lock()
	if c.closed || c.bridge.ctx.Err() != nil {
		c.mu.Unlock()
		_ = conn.Close()
		closeCode = 1000
		return
	}
	c.netConn = conn
	c.mu.Unlock()

	c.bridge.loop.RunOnLoop(func(rt *moejs.Runtime) {
		if !c.onOpen.IsUndefined() && c.bridge.ctx.Err() == nil {
			_, _ = rt.CallFunction(c.onOpen, moejs.Undefined())
		}
	})

	var fragOp byte
	var fragBuf []byte
	for {
		fin, op, payload, err := readWSFrame(reader)
		if err != nil {
			if c.isClosed() || c.bridge.ctx.Err() != nil {
				closeCode = 1000
			} else {
				closeReason = err.Error()
				c.postError(err.Error())
			}
			return
		}
		switch op {
		case 0x8: // Close
			closeCode = 1000
			if len(payload) >= 2 {
				closeCode = int(binary.BigEndian.Uint16(payload[:2]))
				if len(payload) > 2 {
					closeReason = string(payload[2:])
				}
			}
			_ = c.writeFrame(0x8, nil)
			return
		case 0x9: // Ping
			_ = c.writeFrame(0xA, payload)
		case 0xA: // Pong
		case 0x1, 0x2:
			if fin {
				c.deliverMessage(op, payload)
			} else {
				fragOp = op
				fragBuf = append(fragBuf[:0], payload...)
			}
		case 0x0: // Continuation
			if fragOp == 0 {
				return
			}
			if len(fragBuf)+len(payload) > wsMaxMessageBytes {
				closeCode = 1009
				return
			}
			fragBuf = append(fragBuf, payload...)
			if fin {
				out := make([]byte, len(fragBuf))
				copy(out, fragBuf)
				opOut := fragOp
				fragOp, fragBuf = 0, nil
				c.deliverMessage(opOut, out)
			}
		default:
			return
		}
	}
}

func (c *wsConn) deliverMessage(op byte, payload []byte) {
	if c.onMessage.IsUndefined() || c.bridge.ctx.Err() != nil {
		return
	}
	c.bridge.loop.RunOnLoop(func(rt *moejs.Runtime) {
		if c.onMessage.IsUndefined() || c.bridge.ctx.Err() != nil {
			return
		}
		if op == 0x1 {
			_, _ = rt.CallFunction(c.onMessage, moejs.Undefined(), moejs.String(string(payload)))
			return
		}
		val, err := rt.FromGo(payload)
		if err == nil {
			_, _ = rt.CallFunction(c.onMessage, moejs.Undefined(), val)
		}
	})
}

func (c *wsConn) postError(msg string) {
	if c.onError.IsUndefined() || c.bridge.ctx.Err() != nil {
		return
	}
	c.bridge.loop.RunOnLoop(func(rt *moejs.Runtime) {
		if !c.onError.IsUndefined() && c.bridge.ctx.Err() == nil {
			_, _ = rt.CallFunction(c.onError, moejs.Undefined(), moejs.String(msg))
		}
	})
}

func (c *wsConn) dial() (net.Conn, *bufio.Reader, error) {
	dialCtx, cancel := context.WithTimeout(c.bridge.ctx, wsDialTimeout)
	defer cancel()

	host := c.target.Hostname()
	port := c.target.Port()
	if port == "" {
		if strings.EqualFold(c.target.Scheme, "wss") {
			port = "443"
		} else {
			port = "80"
		}
	}
	addr := net.JoinHostPort(host, port)

	var rawConn net.Conn
	var err error
	type wsDialer interface {
		DialWS(ctx context.Context, network, addr string) (net.Conn, error)
	}
	type wsTLSProvider interface {
		WSTLSConfig() *tls.Config
	}
	switch tr := func() http.RoundTripper {
		if c.bridge.client != nil {
			return c.bridge.client.Transport
		}
		return nil
	}().(type) {
	case wsDialer:
		rawConn, err = tr.DialWS(dialCtx, "tcp", addr)
	case *http.Transport:
		if tr != nil && tr.DialContext != nil {
			rawConn, err = tr.DialContext(dialCtx, "tcp", addr)
		} else {
			var dialer net.Dialer
			rawConn, err = dialer.DialContext(dialCtx, "tcp", addr)
		}
	default:
		var dialer net.Dialer
		rawConn, err = dialer.DialContext(dialCtx, "tcp", addr)
	}
	if err != nil {
		return nil, nil, err
	}
	var conn net.Conn = rawConn
	if strings.EqualFold(c.target.Scheme, "wss") {
		tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		if c.bridge.client != nil {
			switch tr := c.bridge.client.Transport.(type) {
			case wsTLSProvider:
				if cfg := tr.WSTLSConfig(); cfg != nil {
					tlsCfg = cfg.Clone()
				}
			case *http.Transport:
				if tr != nil && tr.TLSClientConfig != nil {
					tlsCfg = tr.TLSClientConfig.Clone()
				}
			}
			if tlsCfg.ServerName == "" {
				tlsCfg.ServerName = host
			}
		}
		// RFC 6455 runs over HTTP/1.1 Upgrade; never inherit HTTP/2 ALPN ("h2") from http.Transport.
		tlsCfg.NextProtos = []string{"http/1.1"}
		tlsConn := tls.Client(rawConn, tlsCfg)
		if err := tlsConn.HandshakeContext(dialCtx); err != nil {
			_ = rawConn.Close()
			return nil, nil, err
		}
		conn = tlsConn
	}

	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes[:])

	path := c.target.RequestURI()
	var req strings.Builder
	fmt.Fprintf(&req, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&req, "Host: %s\r\n", c.target.Host)
	req.WriteString("Upgrade: websocket\r\n")
	req.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&req, "Sec-WebSocket-Key: %s\r\n", key)
	req.WriteString("Sec-WebSocket-Version: 13\r\n")
	for name, vals := range c.headers {
		for _, v := range vals {
			fmt.Fprintf(&req, "%s: %s\r\n", name, v)
		}
	}
	req.WriteString("\r\n")

	if deadline, ok := dialCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := io.WriteString(conn, req.String()); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	reader := bufio.NewReaderSize(conn, 8192)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("websocket upgrade rejected: HTTP %d", resp.StatusCode)
	}
	sum := sha1.Sum([]byte(key + wsAcceptGUID))
	expectedAccept := base64.StdEncoding.EncodeToString(sum[:])
	if !strings.EqualFold(strings.TrimSpace(resp.Header.Get("Upgrade")), "websocket") ||
		strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Accept")) != expectedAccept {
		_ = conn.Close()
		return nil, nil, errors.New("invalid websocket accept response")
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, reader, nil
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	conn := c.netConn
	closed := c.closed
	c.mu.Unlock()
	if closed || conn == nil {
		return errors.New("websocket not connected")
	}

	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	n := len(payload)
	headerLen := 2 + 4
	switch {
	case n >= 65536:
		headerLen += 8
	case n >= 126:
		headerLen += 2
	}
	frame := make([]byte, headerLen+n)
	frame[0] = 0x80 | (opcode & 0x0f)
	off := 2
	switch {
	case n < 126:
		frame[1] = 0x80 | byte(n)
	case n < 65536:
		frame[1] = 0x80 | 126
		binary.BigEndian.PutUint16(frame[2:4], uint16(n))
		off = 4
	default:
		frame[1] = 0x80 | 127
		binary.BigEndian.PutUint64(frame[2:10], uint64(n))
		off = 10
	}
	copy(frame[off:off+4], mask[:])
	off += 4
	for i := range n {
		frame[off+i] = payload[i] ^ mask[i&3]
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	_, err := conn.Write(frame)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func (c *wsConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *wsConn) closeGraceful() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	conn := c.netConn
	c.netConn = nil
	c.mu.Unlock()
	if conn != nil {
		_ = conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		_, _ = conn.Write([]byte{0x88, 0x80, 0, 0, 0, 0})
		_ = conn.Close()
	}
}

func (c *wsConn) closeNow() {
	c.mu.Lock()
	c.closed = true
	conn := c.netConn
	c.netConn = nil
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func readWSFrame(r *bufio.Reader) (fin bool, opcode byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return false, 0, nil, err
	}
	fin = hdr[0]&0x80 != 0
	opcode = hdr[0] & 0x0f
	masked := hdr[1]&0x80 != 0
	length := uint64(hdr[1] & 0x7f)
	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > wsMaxMessageBytes {
		return false, 0, nil, errors.New("websocket frame exceeds size limit")
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(r, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	if length > 0 {
		payload = make([]byte, length)
		if _, err = io.ReadFull(r, payload); err != nil {
			return false, 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i&3]
			}
		}
	}
	return fin, opcode, payload, nil
}
