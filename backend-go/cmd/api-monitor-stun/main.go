// api-monitor-stun 是一个极简 RFC 8489 STUN 服务器。
//
// 用途：为转发中心的 P2P 打洞提供自建 STUN（Binding 反射地址），减少对外部公共
// STUN 的依赖。行为：UDP 监听，对合法的 STUN Binding Request 回 XOR-MAPPED-ADDRESS
// （客户端源地址）；非 Binding Request 一律丢弃，不做反射放大源。
//
// 由 Agent 下载并托管（复用 api-monitor-relay 的二进制生命周期模式）：
//
//	api-monitor-stun -listen 0.0.0.0:3478 -manage 127.0.0.1:18081
package main

import (
	"encoding/binary"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	stunBindingRequest   uint16 = 0x0001
	stunBindingSuccess   uint16 = 0x0101
	stunMagicCookie      uint32 = 0x2112_A442
	attrXorMappedAddress uint16 = 0x0020
	stunHeaderLen               = 20
)

var (
	listenAddr = flag.String("listen", "0.0.0.0:3478", "UDP 监听地址")
	manageAddr = flag.String("manage", "127.0.0.1:18081", "HTTP 管理监听地址")
)

var (
	requests  atomic.Uint64
	responses atomic.Uint64
)

// isBindingRequest 校验数据报是否为合法的 STUN Binding Request。
func isBindingRequest(msg []byte) bool {
	if len(msg) < stunHeaderLen {
		return false
	}
	if binary.BigEndian.Uint16(msg[0:2]) != stunBindingRequest {
		return false
	}
	return binary.BigEndian.Uint32(msg[4:8]) == stunMagicCookie
}

// buildBindingResponse 构造 Binding Success Response，反射地址为客户端源地址。
func buildBindingResponse(tid []byte, src net.Addr) []byte {
	host, port, _ := net.SplitHostPort(src.String())
	var ipBytes [4]byte
	ip := net.ParseIP(host).To4()
	copy(ipBytes[:], ip)

	// XOR-MAPPED-ADDRESS value: [1B res][1B family][2B X-Port][4B X-Address]
	var value [8]byte
	value[1] = 0x01 // IPv4
	u16 := uint16(portToInt(port))
	binary.BigEndian.PutUint16(value[2:4], u16^(uint16(stunMagicCookie>>16)))
	xip := binary.BigEndian.Uint32(ipBytes[:]) ^ stunMagicCookie
	binary.BigEndian.PutUint32(value[4:8], xip)

	resp := make([]byte, stunHeaderLen+4+len(value))
	binary.BigEndian.PutUint16(resp[0:2], stunBindingSuccess)
	binary.BigEndian.PutUint16(resp[2:4], uint16(4+len(value)))
	binary.BigEndian.PutUint32(resp[4:8], stunMagicCookie)
	copy(resp[8:20], tid)
	binary.BigEndian.PutUint16(resp[20:22], attrXorMappedAddress)
	binary.BigEndian.PutUint16(resp[22:24], uint16(len(value)))
	copy(resp[24:], value[:])
	return resp
}

func portToInt(port string) int {
	n := 0
	for _, c := range port {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func serveUDP() {
	pc, err := net.ListenPacket("udp", *listenAddr)
	if err != nil {
		log.Fatalf("stun listen %s: %v", *listenAddr, err)
	}
	defer pc.Close()
	log.Printf("api-monitor-stun listening udp %s", *listenAddr)

	buf := make([]byte, 2048)
	for {
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		requests.Add(1)
		msg := buf[:n]
		if !isBindingRequest(msg) {
			continue
		}
		tid := make([]byte, 12)
		copy(tid, msg[8:20])
		resp := buildBindingResponse(tid, src)
		if _, err := pc.WriteTo(resp, src); err != nil {
			continue
		}
		responses.Add(1)
	}
}

func serveManage() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","requests":` + itoa(requests.Load()) +
			`,"responses":` + itoa(responses.Load()) + `}`))
	})
	srv := &http.Server{Addr: *manageAddr, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("stun manage http: %v", err)
		}
	}()
	log.Printf("api-monitor-stun manage http %s", *manageAddr)
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func main() {
	flag.Parse()

	go serveManage()
	go serveUDP()

	// 定期打印统计（便于排查）
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for range t.C {
			log.Printf("stun stats requests=%d responses=%d", requests.Load(), responses.Load())
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("api-monitor-stun stopping")
}
