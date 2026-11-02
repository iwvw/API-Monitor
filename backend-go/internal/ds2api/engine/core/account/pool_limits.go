package account

import (
	"os"
	"strconv"
	"strings"
)

func (p *Pool) ApplyRuntimeLimits(maxInflightPerAccount, maxQueueSize, globalMaxInflight int) {
	if maxInflightPerAccount <= 0 {
		maxInflightPerAccount = 1
	}
	if maxQueueSize < 0 {
		maxQueueSize = 0
	}
	if globalMaxInflight <= 0 {
		globalMaxInflight = maxInflightPerAccount * len(p.store.Accounts())
		if globalMaxInflight <= 0 {
			globalMaxInflight = maxInflightPerAccount
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.maxInflightPerAccount = maxInflightPerAccount
	p.maxQueueSize = maxQueueSize
	p.globalMaxInflight = globalMaxInflight
	p.recommendedConcurrency = defaultRecommendedConcurrency(len(p.queue), p.maxInflightPerAccount)
	p.notifyWaiterLocked()
}

func maxInflightFromEnv() int {
	if raw := strings.TrimSpace(os.Getenv("DS2API_ACCOUNT_MAX_INFLIGHT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 2
}

func defaultRecommendedConcurrency(accountCount, maxInflightPerAccount int) int {
	if accountCount <= 0 {
		return 0
	}
	if maxInflightPerAccount <= 0 {
		maxInflightPerAccount = 2
	}
	return accountCount * maxInflightPerAccount
}

func maxQueueFromEnv(defaultSize int) int {
	if raw := strings.TrimSpace(os.Getenv("DS2API_ACCOUNT_MAX_QUEUE")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	if defaultSize < 0 {
		return 0
	}
	return defaultSize
}

func (p *Pool) canAcquireIDLocked(accountID string) bool {
	if accountID == "" {
		return false
	}
	if p.inUse[accountID] >= p.maxInflightPerAccount {
		return false
	}
	if p.globalMaxInflight > 0 && p.currentInUseLocked() >= p.globalMaxInflight {
		return false
	}
	return true
}

func (p *Pool) currentInUseLocked() int {
	total := 0
	for _, n := range p.inUse {
		total += n
	}
	return total
}
