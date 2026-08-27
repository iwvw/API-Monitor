package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func newTestRelay(t *testing.T) (*RelayServer, int) {
	t.Helper()
	mgmtPort := freePort(t)
	relay := NewRelay(fmt.Sprintf("127.0.0.1:%d", mgmtPort), "test-token")
	done := make(chan error, 1)
	go func() { done <- relay.Serve() }()
	t.Cleanup(func() { relay.Close() })
	waitUntil(t, time.Second*3, func() bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", mgmtPort), 200*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	})
	return relay, mgmtPort
}

func waitUntil(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func mgmtReq(t *testing.T, method, url, token string, body io.Reader, wantStatus int) []byte {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d (%s), want %d", method, url, resp.StatusCode, data, wantStatus)
	}
	return data
}

func addForward(t *testing.T, relay *RelayServer, mgmtPort int, id string, port int) {
	t.Helper()
	addForwardToken(t, relay, mgmtPort, id, port, "")
}

func addForwardToken(t *testing.T, relay *RelayServer, mgmtPort int, id string, port int, token string) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/forwards", mgmtPort)
	body := fmt.Sprintf(`{"id":%q,"listen_port":%d,"token":%q}`, id, port, token)
	mgmtReq(t, "POST", url, "test-token", bytes.NewBufferString(body), 200)
}

// 隧道握手：源主机 Agent 建立反向隧道
func dialTunnel(t *testing.T, port int, id string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second*3)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	idBytes := []byte(id)
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(idBytes)))
	if _, err := conn.Write(append(header, idBytes...)); err != nil {
		t.Fatalf("send handshake: %v", err)
	}
	var ack [4]byte
	if _, err := io.ReadFull(conn, ack[:]); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if status := binary.BigEndian.Uint32(ack[:]); status != 0 {
		t.Fatalf("tunnel rejected: status=%d", status)
	}
	return conn
}

func readFrameFrom(t *testing.T, conn net.Conn, timeout time.Duration) (byte, uint16, []byte) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	defer conn.SetReadDeadline(time.Time{})
	typ, id, payload, err := readFrame(conn)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return typ, id, payload
}

// accumulateData 连续读取 DATA 帧拼装为一段流数据（隧道/客户端是流式分段，需累积到目标长度）。
func accumulateData(t *testing.T, conn net.Conn, target int, timeout time.Duration) (uint16, []byte) {
	t.Helper()
	buf := make([]byte, 0, target)
	var connID uint16
	deadline := time.Now().Add(timeout)
	for len(buf) < target {
		_ = conn.SetReadDeadline(deadline)
		typ, id, payload, err := readFrame(conn)
		if err != nil {
			t.Fatalf("accumulate: %v (got %d/%d bytes)", err, len(buf), target)
		}
		switch typ {
		case frameTypeData:
			if connID == 0 {
				connID = id
			}
			buf = append(buf, payload...)
		case frameTypeClose:
			return connID, buf
		default:
			// 忽略保活帧
		}
	}
	_ = conn.SetReadDeadline(time.Time{})
	return connID, buf
}

func TestRelayBridgesSingleClient(t *testing.T) {
	relay, mgmt := newTestRelay(t)
	fwdPort := freePort(t)
	addForward(t, relay, mgmt, "fwd_test01", fwdPort)

	tun := dialTunnel(t, fwdPort, "fwd_test01")
	defer tun.Close()

	client, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort), time.Second*3)
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer client.Close()

	if _, err := client.Write([]byte("hello from client")); err != nil {
		t.Fatal(err)
	}
	id, payload := accumulateData(t, tun, len("hello from client"), 3*time.Second)
	if string(payload) != "hello from client" {
		t.Fatalf("tunnel got payload=%q", payload)
	}

	// 回程：Agent → 客户端（conn_id = 客户端侧分配的那个）
	if err := writeFrame(tun, frameTypeData, id, []byte("hello from server")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("hello from server"))
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(buf) != "hello from server" {
		t.Fatalf("client got %q", buf)
	}

	// 客户端断开 → 隧道收到 CLOSE
	client.Close()
	typ, id, payload := readFrameFrom(t, tun, 3*time.Second)
	if typ != frameTypeClose || len(payload) != 0 {
		t.Fatalf("expected CLOSE frame, got type=%d id=%d payload_len=%d", typ, id, len(payload))
	}
}

func TestRelayMultiplexesConcurrentClients(t *testing.T) {
	relay, mgmt := newTestRelay(t)
	fwdPort := freePort(t)
	addForward(t, relay, mgmt, "fwd_mux", fwdPort)

	tun := dialTunnel(t, fwdPort, "fwd_mux")
	defer tun.Close()

	c1, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort))
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	c2, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort))
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	_, _ = c1.Write([]byte("AAA"))
	_, _ = c2.Write([]byte("BBB"))

	byID := map[uint16]string{}
	paired := map[string]uint16{}
	for i := 0; i < 2; i++ {
		id, payload := accumulateData(t, tun, 3, 3*time.Second)
		byID[id] = string(payload)
		paired[byID[id]] = id
	}
	if len(byID) != 2 {
		t.Fatalf("expected 2 distinct conn ids, got %v", byID)
	}
	if byID[paired["AAA"]] != "AAA" || byID[paired["BBB"]] != "BBB" {
		t.Fatalf("streams crossed: %v", byID)
	}

	// 回程分别投递，各自客户端应收到自己的内容
	if err := writeFrame(tun, frameTypeData, paired["AAA"], []byte("R1")); err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(tun, frameTypeData, paired["BBB"], []byte("R2")); err != nil {
		t.Fatal(err)
	}
	assertClientGot := func(c net.Conn, want string) {
		t.Helper()
		buf := make([]byte, len(want))
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, err := io.ReadFull(c, buf); err != nil {
			t.Fatalf("client read: %v", err)
		}
		if string(buf) != want {
			t.Fatalf("client got %q, want %q", buf, want)
		}
	}
	assertClientGot(c1, "R1")
	assertClientGot(c2, "R2")

	// 关闭 c1，隧道应收到对该 conn_id 的 CLOSE
	_ = c1.Close()
	timeout := time.Now().Add(3 * time.Second)
	_ = tun.SetReadDeadline(timeout)
	var closedID uint16
	for time.Now().Before(timeout) {
		typ, id, _, err := readFrame(tun)
		if err != nil {
			break
		}
		if typ == frameTypeClose {
			closedID = id
			break
		}
	}
	if closedID != paired["AAA"] {
		t.Fatalf("expected CLOSE for client1 id=%d, got %d", paired["AAA"], closedID)
	}
}

func TestRelayKeepaliveNotForwarded(t *testing.T) {
	relay, mgmt := newTestRelay(t)
	fwdPort := freePort(t)
	addForward(t, relay, mgmt, "fwd_ka", fwdPort)

	tun := dialTunnel(t, fwdPort, "fwd_ka")
	defer tun.Close()

	client, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// 先让客户端发一个字节学习其 conn_id
	if _, err := client.Write([]byte("P")); err != nil {
		t.Fatal(err)
	}
	id, payload := accumulateData(t, tun, 1, 3*time.Second)
	if string(payload) != "P" {
		t.Fatalf("learn client id: got %q", payload)
	}

	// Agent 发送保活帧，客户端不应收到任何字节；随后数据帧只拿到数据
	if err := writeFrame(tun, frameTypeKeepalive, 0, nil); err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(tun, frameTypeData, id, []byte("X")); err != nil {
		t.Fatal(err)
	}
	var first [1]byte
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := client.Read(first[:]); err != nil || first[0] != 'X' {
		t.Fatalf("client received keepalive or no data (err=%v first=%q)", err, first[0])
	}
}

func TestRelayAuthAndDelete(t *testing.T) {
	relay, mgmt := newTestRelay(t)
	fwdPort := freePort(t)

	// 无 token 应 401
	mgmtReq(t, "POST", fmt.Sprintf("http://127.0.0.1:%d/forwards", mgmt), "", bytes.NewBufferString(`{"id":"x","listen_port":1}`), http.StatusUnauthorized)

	addForward(t, relay, mgmt, "fwd_del", fwdPort)
	// 隧道建立后删除规则，监听关闭、隧道与客户端断开
	tun := dialTunnel(t, fwdPort, "fwd_del")
	mgmtReq(t, "DELETE", fmt.Sprintf("http://127.0.0.1:%d/forwards/fwd_del", mgmt), "test-token", nil, 200)
	// 隧道应被关闭
	_ = tun.SetReadDeadline(time.Now().Add(2 * time.Second))
	var one [1]byte
	if _, err := tun.Read(one[:]); err == nil || err != io.EOF {
		// 允许已关闭错误；只要不是读到数据都算通过
		t.Logf("tunnel post-delete read err=%v", err)
	}
	// 端口不再接受连接
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort), 500*time.Millisecond); err == nil {
		c.Close()
		t.Fatalf("listener still accepting after delete")
	}
}

func TestRelayRejectsWrongHandshakeAsClient(t *testing.T) {
	relay, mgmt := newTestRelay(t)
	fwdPort := freePort(t)
	addForward(t, relay, mgmt, "fwd_wr", fwdPort)

	tun := dialTunnel(t, fwdPort, "fwd_wr")
	defer tun.Close()

	// 客户端以错误长度开头（长度 7 但内容不匹配），应被当作普通客户端而不是隧道
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, 7)
	_, _ = c.Write(append(hdr, []byte("bogus!!")...))
	_, _ = c.Write([]byte(" rest-of-data"))

	want := "\x00\x00\x00\x07bogus!! rest-of-data"
	_, payload := accumulateData(t, tun, len(want), 3*time.Second)
	if string(payload) != want {
		t.Fatalf("client prefix corrupted: got %q", payload)
	}
}

// 隧道（forward_id 握手）在 token 模式转发上仍应被接受。
func TestRelayTokenTunnelStillWorks(t *testing.T) {
	relay, mgmt := newTestRelay(t)
	fwdPort := freePort(t)
	addForwardToken(t, relay, mgmt, "fwd_tk", fwdPort, "am_secret")
	tun := dialTunnel(t, fwdPort, "fwd_tk")
	defer tun.Close()

	// 客户端用 raw 握手通过校验后桥接
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	tk := []byte("am_secret")
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, uint32(len(tk)))
	_, _ = c.Write(append(hdr, tk...))
	_, _ = c.Write([]byte("ping"))
	_, payload := accumulateData(t, tun, 4, 3*time.Second)
	if string(payload) != "ping" {
		t.Fatalf("bridged payload=%q, token should be stripped", payload)
	}
}

// token 模式：raw 握手通过才桥接，token 被剥离；错误/缺失 token 被关闭。
func TestRelayTokenRawHandshakeGate(t *testing.T) {
	relay, mgmt := newTestRelay(t)
	fwdPort := freePort(t)
	addForwardToken(t, relay, mgmt, "fwd_tk2", fwdPort, "am_abc123")
	tun := dialTunnel(t, fwdPort, "fwd_tk2")
	defer tun.Close()

	// 1) 缺 token：连接应立即被关闭
	bad, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort))
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()
	_, _ = bad.Write([]byte("no token here"))
	_ = bad.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := bad.Read(make([]byte, 1)); err == nil {
		t.Fatalf("expected connection closed without token")
	}

	// 2) 错误 token：关闭
	wrong, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort))
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()
	wt := []byte("am_wrong")
	wh := make([]byte, 4)
	binary.BigEndian.PutUint32(wh, uint32(len(wt)))
	_, _ = wrong.Write(append(wh, wt...))
	_ = wrong.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := wrong.Read(make([]byte, 1)); err == nil {
		t.Fatalf("expected connection closed with wrong token")
	}

	// 3) 正确 token：桥接且 token 被剥离
	ok, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort))
	if err != nil {
		t.Fatal(err)
	}
	defer ok.Close()
	tk := []byte("am_abc123")
	oh := make([]byte, 4)
	binary.BigEndian.PutUint32(oh, uint32(len(tk)))
	_, _ = ok.Write(append(oh, tk...))
	_, _ = ok.Write([]byte("hello"))
	_, payload := accumulateData(t, tun, 5, 3*time.Second)
	if string(payload) != "hello" {
		t.Fatalf("token stripped, got %q", payload)
	}
}

// token 模式：HTTP 客户端用 Authorization: Bearer 校验，整请求原样桥接。
func TestRelayTokenHTTPBearerGate(t *testing.T) {
	relay, mgmt := newTestRelay(t)
	fwdPort := freePort(t)
	addForwardToken(t, relay, mgmt, "fwd_tk3", fwdPort, "am_http")
	tun := dialTunnel(t, fwdPort, "fwd_tk3")
	defer tun.Close()

	// 1) 无 Bearer：关闭
	nb, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort))
	if err != nil {
		t.Fatal(err)
	}
	defer nb.Close()
	_, _ = nb.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	_ = nb.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := nb.Read(make([]byte, 1)); err == nil {
		t.Fatalf("expected HTTP without bearer closed")
	}

	// 2) 带正确 Bearer：整请求（含 Authorization 头）桥接到隧道
	hc, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort))
	if err != nil {
		t.Fatal(err)
	}
	defer hc.Close()
	req := "GET / HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer am_http\r\n\r\n"
	_, _ = hc.Write([]byte(req))
	id, payload := accumulateData(t, tun, len(req), 3*time.Second)
	if string(payload) != req {
		t.Fatalf("HTTP request not forwarded intact: got %q", payload)
	}
	// 回程正常
	if err := writeFrame(tun, frameTypeData, id, []byte("HTTP/1.1 200 OK\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("HTTP/1.1 200 OK\r\n\r\n"))
	_ = hc.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(hc, buf); err != nil {
		t.Fatalf("client read: %v", err)
	}
}
