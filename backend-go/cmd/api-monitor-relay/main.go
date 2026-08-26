package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ==================== 复用隧道协议 ====================
//
// 每个转发规则在入口主机上独占一个公开监听端口（remote_port），
// 源主机 Agent 与该端口建立长连接即「隧道」，随后该连接复用承载：
//   - 隧道建立：源主机 → [4B BE 长度 N][N 字节 forward_id]，中继 → [4B BE 状态码]（0=接受,1=未知/冲突）
//   - 每帧：   [1B type][2B conn_id BE][4B 载荷长度 BE][载荷]
//     type 0x01=数据, 0x02=关闭该 conn, 0x03=保活（双方消费，不透传）
//
// conn_id 由中继（连接发起侧）分配，源主机镜像原样使用；真实客户端连接总是
// 从「4 字节长度」不合法的内容开始，中继据此区分隧道与普通客户端。

const (
	frameTypeData        byte = 0x01
	frameTypeClose       byte = 0x02
	frameTypeKeepalive   byte = 0x03
	maxFramePayload           = 1 << 20 // 单帧载荷上限 1MB
	classifyClientWindow      = 300 * time.Millisecond
	keepaliveInterval         = 30 * time.Second
)

// ==================== 帧编解码 ====================

func writeFrame(w io.Writer, typ byte, connID uint16, payload []byte) error {
	var hdr [7]byte
	hdr[0] = typ
	binary.BigEndian.PutUint16(hdr[1:3], connID)
	binary.BigEndian.PutUint32(hdr[3:7], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func readFrame(r io.Reader) (typ byte, connID uint16, payload []byte, err error) {
	var hdr [7]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return 0, 0, nil, err
	}
	typ = hdr[0]
	connID = binary.BigEndian.Uint16(hdr[1:3])
	n := binary.BigEndian.Uint32(hdr[3:7])
	if n > maxFramePayload {
		return 0, 0, nil, fmt.Errorf("frame payload too large: %d", n)
	}
	if n > 0 {
		payload = make([]byte, n)
		if _, err = io.ReadFull(r, payload); err != nil {
			return 0, 0, nil, err
		}
	}
	return typ, connID, payload, nil
}

// ==================== 配置 ====================

type RelayConfig struct {
	Forwards []ForwardConfig `json:"forwards"`
}

type ForwardConfig struct {
	ID         string `json:"id"`
	ListenPort int    `json:"listen_port"`
}

// ==================== 单转发规则 ====================

type forward struct {
	id         string
	port       int
	mu         sync.Mutex
	ln         net.Listener
	tunnel     net.Conn // 当前活跃隧道（nil=无）
	tunWriteMu sync.Mutex
	conns      map[uint16]net.Conn
	nextID     atomic.Uint32
	closCh     chan struct{}
	close      sync.Once
}

func newForward(id string, port int) *forward {
	return &forward{
		id:     id,
		port:   port,
		conns:  make(map[uint16]net.Conn),
		closCh: make(chan struct{}),
	}
}

func (f *forward) shutdown() {
	f.close.Do(func() { close(f.closCh) })
	f.mu.Lock()
	if f.ln != nil {
		_ = f.ln.Close()
	}
	tun := f.tunnel
	f.tunnel = nil
	conns := f.conns
	f.conns = make(map[uint16]net.Conn)
	f.mu.Unlock()
	if tun != nil {
		_ = tun.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
}

// setTunnel 替换/清除活跃隧道：旧隧道及经其桥接的客户端全部断开。
func (f *forward) setTunnel(t net.Conn) (closed []net.Conn) {
	f.mu.Lock()
	old := f.tunnel
	f.tunnel = t
	conns := f.conns
	f.conns = make(map[uint16]net.Conn)
	f.mu.Unlock()
	if old != nil && old != t {
		_ = old.Close()
	}
	closed = make([]net.Conn, 0, len(conns))
	for _, c := range conns {
		closed = append(closed, c)
	}
	return closed
}

func (f *forward) getTunnel() net.Conn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tunnel
}

func (f *forward) allocConnID() uint16 {
	for {
		id := uint16(f.nextID.Add(1))
		if id == 0 {
			continue
		}
		f.mu.Lock()
		_, used := f.conns[id]
		if !used && f.tunnel != nil {
			f.conns[id] = nil // 预占
		}
		f.mu.Unlock()
		if !used {
			return id
		}
	}
}

func (f *forward) putClient(id uint16, c net.Conn) {
	f.mu.Lock()
	f.conns[id] = c
	f.mu.Unlock()
}

func (f *forward) removeClient(id uint16) net.Conn {
	f.mu.Lock()
	c := f.conns[id]
	delete(f.conns, id)
	f.mu.Unlock()
	return c
}

func (f *forward) getClient(id uint16) net.Conn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns[id]
}

// ==================== 中继服务 ====================

type RelayServer struct {
	mgmtAddr   string
	token      string
	mgmtServer *http.Server
	mu         sync.Mutex
	forwards   map[string]*forward
	stats      RelayStats
	wg         sync.WaitGroup
	closCh     chan struct{}
	close      sync.Once
}

type RelayStats struct {
	TotalConnections  atomic.Int64
	ActiveConnections atomic.Int64
	StartedAt         time.Time
}

// NewRelay 构造中继，mgmtAddr 为空则不做管理监听（仅转发）。token 非空时管理接口校验 Bearer。
func NewRelay(mgmtAddr, token string) *RelayServer {
	return &RelayServer{
		mgmtAddr: mgmtAddr,
		token:    token,
		forwards: make(map[string]*forward),
		stats:    RelayStats{StartedAt: time.Now()},
		closCh:   make(chan struct{}),
	}
}

func (f *forward) writeFrameLocked(tun net.Conn, typ byte, connID uint16, payload []byte) error {
	if tun == nil {
		return nil
	}
	f.tunWriteMu.Lock()
	defer f.tunWriteMu.Unlock()
	return writeFrame(tun, typ, connID, payload)
}

// Serve 启动中继：管理监听 + 全部 forward 监听。阻塞直到全部关闭。
func (s *RelayServer) Serve() error {
	s.mu.Lock()
	var holders []*forward
	for _, f := range s.forwards {
		holders = append(holders, f)
	}
	s.mu.Unlock()
	for _, f := range holders {
		s.listenForward(f)
	}
	if s.mgmtAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /forwards", s.handleAddForward)
		mux.HandleFunc("DELETE /forwards/{id}", s.handleDeleteForward)
		mux.HandleFunc("GET /forwards", s.handleListForwards)
		mux.HandleFunc("GET /health", s.handleHealth)
		s.mgmtServer = &http.Server{Addr: s.mgmtAddr, Handler: s.auth(mux), ReadHeaderTimeout: 10 * time.Second}
		ln, err := net.Listen("tcp", s.mgmtAddr)
		if err != nil {
			return fmt.Errorf("management listen %s: %w", s.mgmtAddr, err)
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			_ = s.mgmtServer.Serve(ln)
		}()
	}
	s.wg.Wait()
	return nil
}

func (s *RelayServer) auth(next http.Handler) http.Handler {
	if s.token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); !strings.HasPrefix(h, "Bearer ") || h[len("Bearer "):] != s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *RelayServer) listenForward(f *forward) {
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", f.port))
	if err != nil {
		log.Printf("listen port %d (forward %s): %v", f.port, f.id, err)
		return
	}
	f.mu.Lock()
	f.ln = ln
	f.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop(f)
	}()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.keepaliveLoop(f)
	}()
}

func (s *RelayServer) keepaliveLoop(f *forward) {
	t := time.NewTicker(keepaliveInterval)
	defer t.Stop()
	for {
		select {
		case <-s.closCh:
			return
		case <-f.closCh:
			return
		case <-t.C:
			_ = f.writeFrameLocked(f.getTunnel(), frameTypeKeepalive, 0, nil)
		}
	}
}

func (s *RelayServer) acceptLoop(f *forward) {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			select {
			case <-f.closCh:
				return
			case <-s.closCh:
				return
			default:
				time.Sleep(50 * time.Millisecond)
				continue
			}
		}
		s.stats.TotalConnections.Add(1)
		s.stats.ActiveConnections.Add(1)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleIncoming(f, conn)
			s.stats.ActiveConnections.Add(-1)
		}()
	}
}

// handleIncoming 区分隧道与普通客户端。
// 隧道总是在建立后立刻发送长度+forward_id 头；普通客户端则随机开头。用很短的
// 分类窗口（300ms）读 4 字节：读满且与 forward_id 精确匹配 → 隧道；其余 → 客户端，
// 并把已读字节作为流前缀原样桥接（不因读取早停丢失任何客户端数据）。
func (s *RelayServer) handleIncoming(f *forward, conn net.Conn) {
	hdr := make([]byte, 4)
	total := 0
	_ = conn.SetReadDeadline(time.Now().Add(classifyClientWindow))
	for total < 4 {
		n, err := conn.Read(hdr[total:])
		total += n
		if err != nil {
			break
		}
	}
	_ = conn.SetReadDeadline(time.Time{})
	if total == 0 {
		_ = conn.Close()
		return
	}

	prefix := make([]byte, 0, total)
	prefix = append(prefix, hdr[:total]...)
	if total >= 4 {
		n := binary.BigEndian.Uint32(hdr[:4])
		if n == uint32(len(f.id)) && n > 0 && n <= 256 {
			idBytes := make([]byte, n)
			rn := 0
			for rn < int(n) {
				m, err := conn.Read(idBytes[rn:])
				rn += m
				if err != nil {
					break
				}
			}
			prefix = append(prefix, idBytes[:rn]...)
			if rn == int(n) && string(idBytes) == f.id {
				s.acceptTunnel(f, conn)
				return
			}
		}
	}
	s.handleClient(f, conn, prefix)
}

// acceptTunnel 完成隧道握手并注册；同时替换旧隧道。
func (s *RelayServer) acceptTunnel(f *forward, conn net.Conn) {
	closed := f.setTunnel(conn)
	for _, c := range closed {
		_ = c.Close()
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		_ = conn.Close()
		return
	}
	log.Printf("tunnel connected for forward %s", f.id)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.tunnelReadLoop(f, conn)
	}()
}

func (s *RelayServer) tunnelReadLoop(f *forward, conn net.Conn) {
	for {
		typ, connID, payload, err := readFrame(conn)
		if err != nil {
			break
		}
		switch typ {
		case frameTypeData:
			c := f.getClient(connID)
			if c == nil {
				_ = f.writeFrameLocked(conn, frameTypeClose, connID, nil)
				continue
			}
			if len(payload) > 0 {
				if _, werr := c.Write(payload); werr != nil {
					_ = f.writeFrameLocked(conn, frameTypeClose, connID, nil)
					_ = c.Close()
					f.removeClient(connID)
				}
			}
		case frameTypeClose:
			if c := f.removeClient(connID); c != nil {
				_ = c.Close()
			}
		case frameTypeKeepalive:
			// 消耗即可
		default:
			log.Printf("forward %s: unknown frame type %d", f.id, typ)
		}
	}
	// 隧道断开：清理
	closed := f.setTunnel(nil)
	for _, c := range closed {
		_ = c.Close()
	}
}

func (s *RelayServer) handleClient(f *forward, conn net.Conn, prefix []byte) {
	tun := f.getTunnel()
	if tun == nil {
		_ = conn.Close()
		return
	}
	connID := f.allocConnID()
	f.putClient(connID, conn)
	if len(prefix) > 0 {
		_ = f.writeFrameLocked(tun, frameTypeData, connID, prefix)
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			_ = f.writeFrameLocked(tun, frameTypeData, connID, buf[:n])
		}
		if err != nil {
			break
		}
	}
	f.removeClient(connID)
	_ = f.writeFrameLocked(f.getTunnel(), frameTypeClose, connID, nil)
	_ = conn.Close()
}

// ==================== 管理 API ====================

func (s *RelayServer) handleAddForward(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string `json:"id"`
		ListenPort int    `json:"listen_port"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" || req.ListenPort < 1 || req.ListenPort > 65535 {
		http.Error(w, "id and listen_port(1-65535) required", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	existing, existed := s.forwards[req.ID]
	// 幂等：同端口直接认为成功；端口变化则关闭旧监听重绑新端口（面板重试/换端口必须收敛，而非 409）
	if existed && existing.port == req.ListenPort {
		s.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": req.ID, "port": req.ListenPort})
		return
	}
	if existed {
		delete(s.forwards, req.ID)
	}
	f := newForward(req.ID, req.ListenPort)
	s.forwards[req.ID] = f
	s.mu.Unlock()
	if existed {
		existing.shutdown()
	}
	s.listenForward(f)
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": req.ID, "port": req.ListenPort})
}

func (s *RelayServer) handleDeleteForward(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	f, ok := s.forwards[id]
	if ok {
		delete(s.forwards, id)
	}
	s.mu.Unlock()
	if !ok {
		http.Error(w, "forward not found", http.StatusNotFound)
		return
	}
	f.shutdown()
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": id})
}

func (s *RelayServer) handleListForwards(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	list := make([]map[string]any, 0, len(s.forwards))
	for _, f := range s.forwards {
		tun := f.getTunnel()
		list = append(list, map[string]any{"id": f.id, "port": f.port, "has_tunnel": tun != nil})
	}
	s.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"forwards": list})
}

func (s *RelayServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	n := len(s.forwards)
	s.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{
		"status":   "running",
		"forwards": n,
		"stats": map[string]any{
			"total_connections":  s.stats.TotalConnections.Load(),
			"active_connections": s.stats.ActiveConnections.Load(),
			"uptime_seconds":     time.Since(s.stats.StartedAt).Seconds(),
		},
	})
}

// ==================== main ====================

var (
	listenAddr = flag.String("listen", "127.0.0.1:18080", "管理 API 监听地址")
	configFile = flag.String("config", "/etc/api-monitor-relay/config.json", "配置文件路径")
)

func main() {
	flag.Parse()
	token := os.Getenv("API_MONITOR_RELAY_TOKEN")

	relay := NewRelay(*listenAddr, token)

	if data, err := os.ReadFile(*configFile); err != nil {
		log.Printf("warning: cannot read config %s: %v", *configFile, err)
	} else {
		var config RelayConfig
		if err := json.Unmarshal(data, &config); err != nil {
			log.Printf("warning: invalid config: %v", err)
		} else {
			for _, fwd := range config.Forwards {
				if fwd.ID == "" || fwd.ListenPort < 1 || fwd.ListenPort > 65535 {
					continue
				}
				relay.mu.Lock()
				if _, ok := relay.forwards[fwd.ID]; !ok {
					relay.forwards[fwd.ID] = newForward(fwd.ID, fwd.ListenPort)
				}
				relay.mu.Unlock()
				log.Printf("forward %s configured on :%d", fwd.ID, fwd.ListenPort)
			}
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		relay.Close()
	}()

	log.Printf("api-monitor-relay started, mgmt=%s", *listenAddr)
	if err := relay.Serve(); err != nil {
		log.Fatalf("relay: %v", err)
	}
}

func (s *RelayServer) Close() {
	s.close.Do(func() {
		close(s.closCh)
		s.mu.Lock()
		forwards := s.forwards
		s.forwards = make(map[string]*forward)
		s.mu.Unlock()
		for _, f := range forwards {
			f.shutdown()
		}
		if s.mgmtServer != nil {
			s.mgmtServer.Close()
		}
	})
}
