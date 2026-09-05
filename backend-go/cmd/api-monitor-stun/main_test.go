package main

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func testBindingRequest() ([]byte, []byte) {
	tid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	msg := make([]byte, stunHeaderLen)
	binary.BigEndian.PutUint16(msg[0:2], stunBindingRequest)
	binary.BigEndian.PutUint32(msg[4:8], stunMagicCookie)
	copy(msg[8:20], tid)
	return msg, tid
}

func TestIsBindingRequest(t *testing.T) {
	msg, _ := testBindingRequest()
	if !isBindingRequest(msg) {
		t.Fatal("valid binding request should be recognized")
	}
	garbage := make([]byte, 20)
	if isBindingRequest(garbage) {
		t.Fatal("garbage should not be a binding request")
	}
	short := []byte{0x00, 0x01}
	if isBindingRequest(short) {
		t.Fatal("short message should not be a binding request")
	}
}

func TestBuildBindingResponseRoundtrip(t *testing.T) {
	src := &net.UDPAddr{IP: net.ParseIP("203.0.113.7"), Port: 40001}
	msg, tid := testBindingRequest()
	_ = msg
	resp := buildBindingResponse(tid, src)
	if binary.BigEndian.Uint16(resp[0:2]) != stunBindingSuccess {
		t.Fatalf("expected success response, got %x", binary.BigEndian.Uint16(resp[0:2]))
	}
	if binary.BigEndian.Uint32(resp[4:8]) != stunMagicCookie {
		t.Fatal("magic cookie mismatch")
	}
	// XOR-MAPPED: 检查反射地址还原 = src
	value := resp[24:32]
	family := value[1]
	if family != 0x01 {
		t.Fatalf("expected IPv4 family, got %d", family)
	}
	xPort := binary.BigEndian.Uint16(value[2:4])
	port := xPort ^ uint16(stunMagicCookie>>16)
	if port != 40001 {
		t.Fatalf("port mismatch: got %d want 40001", port)
	}
	xIP := binary.BigEndian.Uint32(value[4:8])
	ip := xIP ^ stunMagicCookie
	want := binary.BigEndian.Uint32(net.ParseIP("203.0.113.7").To4())
	if ip != want {
		t.Fatalf("ip mismatch: got %x want %x", ip, want)
	}
}

func TestEndToEndBinding(t *testing.T) {
	oldListen := *listenAddr
	*listenAddr = "127.0.0.1:0"
	defer func() { *listenAddr = oldListen }()

	// 手动启动 UDP 服务（用随机端口）
	pc, err := net.ListenPacket("udp", *listenAddr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, src, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			msg := buf[:n]
			if !isBindingRequest(msg) {
				continue
			}
			resp := buildBindingResponse(msg[8:20], src)
			_, _ = pc.WriteTo(resp, src)
		}
	}()

	client, err := net.DialUDP("udp", nil, pc.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	req, _ := testBindingRequest()
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if binary.BigEndian.Uint16(buf[0:2]) != stunBindingSuccess {
		t.Fatalf("unexpected response type %x", binary.BigEndian.Uint16(buf[0:2]))
	}
	_ = n
}
