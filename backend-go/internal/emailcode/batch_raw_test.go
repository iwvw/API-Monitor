package emailcode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIngestStoresRawAndGetReturnsIt(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	raw := "From: a@x.com\r\nSubject: Hi\r\n\r\nYour code is 123456"
	id, err := s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "123456", Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Raw != raw {
		t.Fatalf("Get 应返回 MIME 原文,得到 %q", got.Raw)
	}
	// 列表应保持轻量:不返回 raw。
	msgs, _ := s.List(ctx, "a@x.com", false, 10)
	if len(msgs) != 1 {
		t.Fatalf("list len=%d", len(msgs))
	}
	if msgs[0].Raw != "" {
		t.Fatalf("列表不应返回 raw,得到 %q", msgs[0].Raw)
	}
}

func TestGetManyAndConsumeMany(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	id1, _ := s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "111111", Raw: "raw-1"})
	id2, _ := s.Ingest(ctx, Message{Mailbox: "b@x.com", Code: "222222", Raw: "raw-2"})

	msgs, err := s.GetMany(ctx, []int64{id2, 999999, id1})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].ID != id2 || msgs[1].ID != id1 {
		t.Fatalf("GetMany 应保持输入顺序并跳过缺失 id: %+v", msgs)
	}
	if msgs[0].Raw != "raw-2" || msgs[1].Raw != "raw-1" {
		t.Fatalf("GetMany 应含 raw: %+v", msgs)
	}

	n, err := s.ConsumeMany(ctx, []int64{id1, id2, 999999}, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("ConsumeMany 应消费 2 封,得到 %d", n)
	}
	if n2, _ := s.ConsumeMany(ctx, []int64{id1, id2}, "agent"); n2 != 0 {
		t.Fatalf("重复消费应为 0,得到 %d", n2)
	}
}

func TestBatchHTTPHandlers(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	s.Ingest(ctx, Message{Mailbox: "a@x.com", Code: "111111", Raw: "raw-1"})
	s.Ingest(ctx, Message{Mailbox: "b@x.com", Code: "222222", Raw: "raw-2"})

	// 批量列出多个收件人
	req := httptest.NewRequest(http.MethodPost, "/api/emailcode/messages/batch",
		strings.NewReader(`{"mailboxes":["a@x.com","b@x.com"],"includeConsumed":true}`))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listRes struct {
		Success bool `json:"success"`
		Data    struct {
			Results []struct {
				Mailbox  string    `json:"mailbox"`
				Messages []Message `json:"messages"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listRes); err != nil {
		t.Fatal(err)
	}
	if !listRes.Success || len(listRes.Data.Results) != 2 {
		t.Fatalf("batch list 应返回 2 个邮箱结果: %s", rec.Body.String())
	}

	all, _ := s.List(ctx, "a@x.com", false, 10)
	idA := all[0].ID

	// 批量取详情(含 raw)
	reqF := httptest.NewRequest(http.MethodPost, "/api/emailcode/messages/fetch",
		strings.NewReader(fmt.Sprintf(`{"ids":[%d]}`, idA)))
	recF := httptest.NewRecorder()
	s.ServeHTTP(recF, reqF)
	if recF.Code != http.StatusOK || !strings.Contains(recF.Body.String(), `"raw":"raw-1"`) {
		t.Fatalf("batch fetch 应含 raw: status=%d body=%s", recF.Code, recF.Body.String())
	}

	// 批量标记已消费
	reqC := httptest.NewRequest(http.MethodPost, "/api/emailcode/messages/consume",
		strings.NewReader(fmt.Sprintf(`{"ids":[%d]}`, idA)))
	recC := httptest.NewRecorder()
	s.ServeHTTP(recC, reqC)
	if recC.Code != http.StatusOK || !strings.Contains(recC.Body.String(), `"consumed":1`) {
		t.Fatalf("batch consume: status=%d body=%s", recC.Code, recC.Body.String())
	}
}
