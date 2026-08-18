package serveragent

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func insertReservePortHost(t *testing.T, db *sql.DB, serverID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id,name,host,username,auth_type) VALUES (?,?,?,'agent','password')`, serverID, serverID, "192.0.2.41"); err != nil {
		t.Fatal(err)
	}
}

func TestReserveManagedProxyPortFirstFree(t *testing.T) {
	_, db := testService(t)
	insertReservePortHost(t, db, "reserve-host")
	port, excluded, err := reserveManagedProxyPort(context.Background(), db, "reserve-host", "new-node", 0)
	if err != nil || port != 45654 || len(excluded) != 0 {
		t.Fatalf("port=%d excluded=%v err=%v", port, excluded, err)
	}
}

func TestReserveManagedProxyPortHonorsFreeRequestedPort(t *testing.T) {
	_, db := testService(t)
	insertReservePortHost(t, db, "reserve-host")
	port, _, err := reserveManagedProxyPort(context.Background(), db, "reserve-host", "new-node", 47890)
	if err != nil || port != 47890 {
		t.Fatalf("port=%d err=%v", port, err)
	}
}

func TestReserveManagedProxyPortRejectsOutOfRangeRequestedPort(t *testing.T) {
	_, db := testService(t)
	insertReservePortHost(t, db, "reserve-host")
	for _, requested := range []int{45653, 55655, -1, 0} {
		port, _, err := reserveManagedProxyPort(context.Background(), db, "reserve-host", "node-"+fmt.Sprint(requested), requested)
		if err != nil || port != 45654 {
			t.Fatalf("requested=%d port=%d err=%v, want first free 45654", requested, port, err)
		}
	}
}

func TestReserveManagedProxyPortSkipsConflictAndMovesToNextFree(t *testing.T) {
	_, db := testService(t)
	insertReservePortHost(t, db, "reserve-host")
	if _, err := db.ExecContext(context.Background(), `INSERT INTO managed_proxy_nodes (id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES ('port-a','reserve-host','a','vless-reality','sing-box','192.0.2.41',45654,'tcp','{}','')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO managed_proxy_nodes (id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES ('port-b','reserve-host','b','vless-reality','sing-box','192.0.2.41',0,'tcp','{}','')`); err != nil {
		t.Fatal(err)
	}
	port, excluded, err := reserveManagedProxyPort(context.Background(), db, "reserve-host", "port-b", 45654)
	if err != nil || port != 45655 || len(excluded) != 1 || excluded[0] != 45654 {
		t.Fatalf("port=%d excluded=%v err=%v", port, excluded, err)
	}
	var stored int
	if err := db.QueryRowContext(context.Background(), `SELECT assigned_port FROM managed_proxy_nodes WHERE id='port-b'`).Scan(&stored); err != nil || stored != 45655 {
		t.Fatalf("stored port=%d err=%v", stored, err)
	}
}

func TestReserveManagedProxyPortIsIdempotentForOwnedPort(t *testing.T) {
	_, db := testService(t)
	insertReservePortHost(t, db, "reserve-host")
	if _, err := db.ExecContext(context.Background(), `INSERT INTO managed_proxy_nodes (id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES ('port-a','reserve-host','a','vless-reality','sing-box','192.0.2.41',45660,'tcp','{}','')`); err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 2; iteration++ {
		port, excluded, err := reserveManagedProxyPort(context.Background(), db, "reserve-host", "port-a", 45660)
		if err != nil || port != 45660 || len(excluded) != 0 {
			t.Fatalf("iteration %d port=%d excluded=%v err=%v", iteration, port, excluded, err)
		}
	}
}

func TestReserveManagedProxyPortIsolatedPerServer(t *testing.T) {
	_, db := testService(t)
	for _, server := range []string{"reserve-host-a", "reserve-host-b"} {
		insertReservePortHost(t, db, server)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO managed_proxy_nodes (id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES ('port-a','reserve-host-a','a','vless-reality','sing-box','192.0.2.41',45654,'tcp','{}','')`); err != nil {
		t.Fatal(err)
	}
	port, excluded, err := reserveManagedProxyPort(context.Background(), db, "reserve-host-b", "port-b", 0)
	if err != nil || port != 45654 || len(excluded) != 0 {
		t.Fatalf("port=%d excluded=%v err=%v, want 45654 on empty server", port, excluded, err)
	}
}

func TestReserveManagedProxyPortExhaustionReturnsError(t *testing.T) {
	_, db := testService(t)
	insertReservePortHost(t, db, "reserve-host")
	for port := 45654; port <= 55654; port++ {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO managed_proxy_nodes (id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES (?,?,?,?,?,?,?,?,?,?)`, fmt.Sprintf("port-%d", port), "reserve-host", fmt.Sprintf("node-%d", port), "vless-reality", "sing-box", "192.0.2.41", port, "tcp", "{}", ""); err != nil {
			t.Fatalf("seed port %d: %v", port, err)
		}
	}
	port, excluded, err := reserveManagedProxyPort(context.Background(), db, "reserve-host", "exhausted-node", 0)
	if err == nil {
		t.Fatalf("reservation succeeded port=%d, want exhaustion error", port)
	}
	if port != 0 || len(excluded) != 10001 {
		t.Fatalf("port=%d excluded=%d, want 0 and 10001", port, len(excluded))
	}
}
