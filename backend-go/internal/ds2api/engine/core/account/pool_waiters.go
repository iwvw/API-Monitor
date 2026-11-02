package account

// canQueueLocked 判断是否还有排队等待的意义。
//   - target 模式：target 必须存在、未被 exclude、且满足 filter
//   - 轮询模式：队列中至少存在一个未被 exclude、且满足 filter 的账号
//
// 排队等待的是"占用中的账号被释放"，因此并发槽位是否已满不影响是否排队，
// 只要有满足 filter 的候选账号即可。若无任何满足 filter 的候选账号，
// 则不应排队，避免请求无限挂起。
func (p *Pool) canQueueLocked(target string, exclude map[string]bool, filter AccountFilter) bool {
	if p.maxQueueSize <= 0 {
		return false
	}
	if len(p.waiters) >= p.maxQueueSize {
		return false
	}
	if target != "" {
		if exclude[target] {
			return false
		}
		acc, ok := p.store.FindAccount(target)
		if !ok {
			return false
		}
		if !acc.IsSchedulable() {
			return false
		}
		if filter != nil && !filter(acc) {
			return false
		}
		return true
	}
	// 轮询模式：确认至少存在一个未被 exclude 且满足 filter 的账号，
	// 否则即使唤醒也无法获取，排队无意义。并发槽位是否已满不影响判断，
	// 因为排队的请求等待的是账号被释放后腾出的槽位。
	for _, id := range p.queue {
		if exclude[id] {
			continue
		}
		acc, ok := p.store.FindAccount(id)
		if !ok {
			continue
		}
		if !acc.IsSchedulable() {
			continue
		}
		if filter != nil && !filter(acc) {
			continue
		}
		return true
	}
	return false
}

func (p *Pool) notifyWaiterLocked() {
	if len(p.waiters) == 0 {
		return
	}
	waiter := p.waiters[0]
	p.waiters = p.waiters[1:]
	close(waiter)
}

func (p *Pool) removeWaiterLocked(waiter chan struct{}) bool {
	for i, w := range p.waiters {
		if w != waiter {
			continue
		}
		p.waiters = append(p.waiters[:i], p.waiters[i+1:]...)
		return true
	}
	return false
}

func (p *Pool) drainWaitersLocked() {
	for _, waiter := range p.waiters {
		close(waiter)
	}
	p.waiters = nil
}
