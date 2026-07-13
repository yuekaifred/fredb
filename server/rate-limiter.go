package main

import (
	"sync"
	"time"
)

type TokenBucket struct {
	mu           sync.Mutex
	tokens       float64
	capacity     float64
	refillPerSec float64
	last         time.Time
}

type RateLimitConfig struct {
	CapacityBytes     float64
	RefillBytesPerSec float64
	FlatOverheadBytes float64
	WriteByteWeight   float64
	ReadByteWeight    float64
}

func (rl RateLimitConfig) PutCost(valueLen int) float64 {
	return rl.FlatOverheadBytes + float64(valueLen)*rl.WriteByteWeight
}

func (rl RateLimitConfig) FlatCost() float64 {
	return rl.FlatOverheadBytes
}

func (rl RateLimitConfig) ReadCost(n int) float64 {
	return float64(n) * rl.ReadByteWeight
}

func NewTokenBucket(rl RateLimitConfig) *TokenBucket {
	return &TokenBucket{
		tokens:       rl.CapacityBytes,
		capacity:     rl.CapacityBytes,
		refillPerSec: rl.RefillBytesPerSec,
		last:         time.Now(),
	}
}

func (b *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(b.capacity, b.tokens+elapsed*b.refillPerSec)
	b.last = now
}

func (b *TokenBucket) Consume(cost float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refill()
	if b.tokens < cost {
		return false
	}
	b.tokens -= cost
	return true
}

func (b *TokenBucket) Charge(cost float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refill()
	b.tokens -= cost
}
