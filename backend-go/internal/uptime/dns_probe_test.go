package uptime

import (
	"context"
	"net"
	"strconv"
	"testing"
)

func TestDNSProbeCustomServerIsUsed(t *testing.T) {
	service, _ := testService(t, true)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 起一个本地假 DNS 服务：收到查询返回垃圾字节，解析必然失败。
	// 若 dns_resolve_server 字段真实生效，探测会失败（而非走系统解析成功）。
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen udp: %v", err)
	}
	defer pc.Close()
	received := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 512)
		n, addr, _ := pc.ReadFrom(buf)
		if n > 0 {
			select {
			case received <- struct{}{}:
			default:
			}
			_, _ = pc.WriteTo([]byte{0x00, 0x01, 0x02}, addr)
		}
	}()

	serverAddr := pc.LocalAddr().(*net.UDPAddr)
	server := serverAddr.IP.String() + ":" + strconv.Itoa(serverAddr.Port)
	monitor := map[string]interface{}{
		"type":               "dns",
		"hostname":           "example.com",
		"dns_resolve_server": server,
		"timeout":            3,
	}
	result, err := service.probe(ctx, db, monitor)
	if err == nil {
		t.Fatalf("expected garbage DNS response to fail probe, got %+v", result)
	}
	select {
	case <-received:
	default:
		t.Fatal("custom DNS server was never queried; dns_resolve_server is not effective")
	}
}

func TestDNSProbeDefaultResolverStillWorks(t *testing.T) {
	service, _ := testService(t, true)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	monitor := map[string]interface{}{
		"type":     "dns",
		"hostname": "example.com",
		"timeout":  3,
	}
	result, err := service.probe(ctx, db, monitor)
	if err != nil {
		t.Fatalf("default resolver probe failed: %v", err)
	}
	if !result.OK {
		t.Fatalf("default resolver probe not ok: %+v", result)
	}
	details, ok := result.Details["records"].([]string)
	if !ok || len(details) == 0 {
		t.Fatalf("expected resolved records, got %#v", result.Details)
	}
	if result.Details["type"] != "A" {
		t.Fatalf("expected type A, got %v", result.Details["type"])
	}
}