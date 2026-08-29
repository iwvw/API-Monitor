package client

import "testing"

func TestShouldAttemptRefreshOnTokenInvalidSignal(t *testing.T) {
	if !shouldAttemptRefresh(401, 0, 0, "unauthorized", "") {
		t.Fatal("expected refresh when response indicates invalid token")
	}
}

func TestShouldAttemptRefreshOnAuthIndicativeBizCodeFailure(t *testing.T) {
	if !shouldAttemptRefresh(200, 0, 400123, "", "login expired, token invalid") {
		t.Fatal("expected refresh on auth-indicative biz_code failure")
	}
}

func TestShouldAttemptRefreshFalseOnNonAuthBizCodeFailure(t *testing.T) {
	if shouldAttemptRefresh(200, 0, 400123, "", "session create failed: quota reached") {
		t.Fatal("did not expect refresh on non-auth biz_code failure")
	}
}

func TestShouldAttemptRefreshFalseOnGenericServerError(t *testing.T) {
	if shouldAttemptRefresh(500, 500, 0, "internal error", "") {
		t.Fatal("did not expect refresh on generic server error")
	}
}
