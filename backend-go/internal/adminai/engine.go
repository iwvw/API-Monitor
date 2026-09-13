package adminai

// approvalResolution 是审批结果（含请求更改原因），由 resolveApproval 下发给等待中的执行。
type approvalResolution struct {
	Action string
	Reason string
}
