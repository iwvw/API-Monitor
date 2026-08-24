package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/subscription"
	"github.com/iwvw/api-monitor/backend-go/internal/subscriptionledger"
)

func seedSubscriberBindingTest(t *testing.T, protocol string) (*sql.DB, string) {
	t.Helper()
	service, db := testService(t)
	if err := subscription.New(service.cfg).Initialize(context.Background()); err != nil {
		t.Fatalf("initialize subscription schema: %v", err)
	}
	statements := []string{
		`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host','主机','192.0.2.1','root','password')`,
		`INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,apply_status,enabled,stats_port) VALUES('node','host','节点',?,'sing-box','192.0.2.1',45654,'tcp','{}','','running',1,21000)`,
		`INSERT INTO subscription_plans(id,name,enabled,total_bytes,cycle_type,cycle_day,selection_mode,include_internal_nodes,include_external_nodes) VALUES('plan','套餐',1,100,'monthly',1,'explicit',1,0)`,
		`INSERT INTO subscription_plan_nodes(plan_id,node_id,source) VALUES('plan','node','internal')`,
		`INSERT INTO subscription_subscriptions(id,profile_id,plan_id,name,public_token,vless_uuid,hysteria2_password,enabled,created_at) VALUES('active','active','plan','有效','token-a','11111111-1111-4111-8111-111111111111','pass-a',1,'2026-01-01 00:00:00'),('spent','spent','plan','耗尽','token-b','22222222-2222-4222-8222-222222222222','pass-b',1,'2026-01-01 00:00:00')`,
		`UPDATE user_settings SET time_zone='UTC' WHERE id=1`,
	}
	for index, statement := range statements {
		var err error
		if index == 1 {
			_, err = db.Exec(statement, protocol)
		} else {
			_, err = db.Exec(statement)
		}
		if err != nil {
			t.Fatalf("seed statement %d: %v", index, err)
		}
	}
	start, end := subscriptionledger.CycleWindow(time.Now().UTC(), "monthly", 1, "", time.UTC)
	if _, err := db.Exec(`INSERT INTO subscription_usage_cycles(subscription_id,cycle_start,cycle_end,upload_bytes,download_bytes) VALUES('spent',?,?,60,40)`, start, end); err != nil {
		t.Fatal(err)
	}
	return db, "node"
}

func TestBindManagedNodeSubscribersWritesOnlyActiveVLESSUsers(t *testing.T) {
	db, nodeID := seedSubscriberBindingTest(t, "vless-reality")
	raw := `{"inbounds":[{"type":"vless","listen_port":45654,"users":[{"uuid":"bootstrap"}]}]}`
	encoded, count, err := bindManagedNodeSubscribers(context.Background(), db, nodeID, "vless-reality", raw)
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(encoded), &root); err != nil {
		t.Fatal(err)
	}
	inbound := root["inbounds"].([]interface{})[0].(map[string]interface{})
	users := inbound["users"].([]interface{})
	if len(users) != 2 {
		t.Fatalf("expected bootstrap + active subscriber, got %d users: %#v", len(users), users)
	}
	// users[0] 保留节点 bootstrap 凭据（节点页 client_uri 仍可用）
	bootstrap := users[0].(map[string]interface{})
	if bootstrap["uuid"] != "bootstrap" {
		t.Fatalf("bootstrap credential not preserved: %#v", bootstrap)
	}
	// 订阅用户追加在后
	user := users[1].(map[string]interface{})
	if user["name"] != "active" || user["uuid"] != "11111111-1111-4111-8111-111111111111" || user["flow"] != "xtls-rprx-vision" {
		t.Fatalf("unexpected users: %#v", users)
	}
	v2ray := root["experimental"].(map[string]interface{})["v2ray_api"].(map[string]interface{})
	statsUsers := v2ray["stats"].(map[string]interface{})["users"].([]interface{})
	if len(statsUsers) != 1 || statsUsers[0] != "active" {
		t.Fatalf("unexpected stats users: %#v", statsUsers)
	}
}

func TestBindManagedNodeSubscribersWritesVLESSWebSocketUsersWithoutRealityFlow(t *testing.T) {
	db, nodeID := seedSubscriberBindingTest(t, "vless-ws-tunnel")
	raw := `{"inbounds":[{"type":"vless","listen_port":45654,"users":[{"uuid":"bootstrap","flow":"xtls-rprx-vision"}]}]}`
	encoded, count, err := bindManagedNodeSubscribers(context.Background(), db, nodeID, "vless-ws-tunnel", raw)
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(encoded), &root); err != nil {
		t.Fatal(err)
	}
	inbound := root["inbounds"].([]interface{})[0].(map[string]interface{})
	users := inbound["users"].([]interface{})
	if len(users) != 2 {
		t.Fatalf("expected bootstrap + active subscriber, got %d users: %#v", len(users), users)
	}
	// bootstrap 凭据原样保留（含 flow 字段），订阅用户追加在后
	if users[0].(map[string]interface{})["uuid"] != "bootstrap" {
		t.Fatalf("bootstrap credential not preserved: %#v", users[0])
	}
	user := users[1].(map[string]interface{})
	if user["name"] != "active" || user["uuid"] != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("unexpected users: %#v", users)
	}
	if _, exists := user["flow"]; exists {
		t.Fatalf("websocket user must not contain reality flow: %#v", user)
	}
}

func TestBindManagedNodeSubscribersWritesHY2Passwords(t *testing.T) {
	db, nodeID := seedSubscriberBindingTest(t, "hysteria2")
	raw := `{"inbounds":[{"type":"hysteria2","listen_port":45654,"users":[{"password":"bootstrap"}]}]}`
	encoded, count, err := bindManagedNodeSubscribers(context.Background(), db, nodeID, "hysteria2", raw)
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	var root map[string]interface{}
	_ = json.Unmarshal([]byte(encoded), &root)
	users := root["inbounds"].([]interface{})[0].(map[string]interface{})["users"].([]interface{})
	if len(users) != 2 || users[0].(map[string]interface{})["password"] != "bootstrap" {
		t.Fatalf("bootstrap not preserved: %#v", users)
	}
	if users[1].(map[string]interface{})["password"] != "pass-a" {
		t.Fatalf("unexpected users: %#v", users)
	}
}

func seedPlainSubscriberBindingTest(t *testing.T, protocol string) (*sql.DB, string) {
	t.Helper()
	db, nodeID := seedSubscriberBindingTest(t, protocol)
	statements := []string{
		`UPDATE managed_proxy_nodes SET protocol=? WHERE id=?`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement, protocol, nodeID); err != nil {
			t.Fatalf("seed plain statement: %v", err)
		}
	}
	return db, nodeID
}

func TestBindManagedNodeSubscribersWritesSOCKSUsers(t *testing.T) {
	db, nodeID := seedPlainSubscriberBindingTest(t, "socks")
	raw := `{"inbounds":[{"type":"socks","listen_port":45654,"users":[{"username":"bootstrap","password":"bootstrap"}]}]}`
	encoded, count, err := bindManagedNodeSubscribers(context.Background(), db, nodeID, "socks", raw)
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	var root map[string]interface{}
	_ = json.Unmarshal([]byte(encoded), &root)
	bound := root["inbounds"].([]interface{})[0].(map[string]interface{})["users"].([]interface{})
	if len(bound) != 2 || bound[0].(map[string]interface{})["username"] != "bootstrap" {
		t.Fatalf("bootstrap not preserved: %#v", bound)
	}
	user := bound[1].(map[string]interface{})
	if user["username"] != "11111111-1111-4111-8111-111111111111" || user["password"] != "pass-a" {
		t.Fatalf("unexpected socks users: %#v", bound)
	}
	v2ray := root["experimental"].(map[string]interface{})["v2ray_api"].(map[string]interface{})
	statsUsers := v2ray["stats"].(map[string]interface{})["users"].([]interface{})
	if len(statsUsers) != 1 || statsUsers[0] != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("unexpected socks stats users: %#v", statsUsers)
	}
}

func TestBindManagedNodeSubscribersWritesHTTPUsers(t *testing.T) {
	db, nodeID := seedPlainSubscriberBindingTest(t, "http")
	raw := `{"inbounds":[{"type":"http","listen_port":45654,"users":[{"username":"bootstrap","password":"bootstrap"}]}]}`
	encoded, count, err := bindManagedNodeSubscribers(context.Background(), db, nodeID, "http", raw)
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	var root map[string]interface{}
	_ = json.Unmarshal([]byte(encoded), &root)
	bound := root["inbounds"].([]interface{})[0].(map[string]interface{})["users"].([]interface{})
	if len(bound) != 2 || bound[0].(map[string]interface{})["username"] != "bootstrap" {
		t.Fatalf("bootstrap not preserved: %#v", bound)
	}
	user := bound[1].(map[string]interface{})
	if user["username"] != "11111111-1111-4111-8111-111111111111" || user["password"] != "pass-a" {
		t.Fatalf("unexpected http users: %#v", bound)
	}
}
