package redislimiter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/logger"
	orderriskcontract "github.com/dujiao-next/internal/modules/orderrisk/contract"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"

	"github.com/redis/go-redis/v9"
)

const (
	attemptKeyPrefix  = "dj:risk:order_attempt:ip:"
	attemptMetaPrefix = "dj:risk:order_attempt:meta:"
	attemptEventKey   = "dj:risk:order_attempt:events"
	attemptEventCap   = 200
)

var orderRateLimitScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
	redis.call("EXPIRE", KEYS[1], ARGV[1])
end
if tonumber(ARGV[2]) > 0 and tonumber(ARGV[3]) > 0 and current == tonumber(ARGV[2]) + 1 then
	redis.call("EXPIRE", KEYS[1], ARGV[3])
end
local ttl = redis.call("TTL", KEYS[1])
return {current, ttl}
`)

type localAttempt struct {
	count   int64
	expires time.Time
	device  string
	signal  string
	userID  uint
	guest   bool
}

type storedMeta struct {
	Device string `json:"device,omitempty"`
	Signal string `json:"signal,omitempty"`
	UserID uint   `json:"user_id,omitempty"`
	Guest  bool   `json:"guest,omitempty"`
}

// Limiter 对旧的游客/会员频率限制保持 Redis 不可用时放行。
// 同 IP 下单尝试限制不同：Redis 可用时用原子计数；Redis 缺失或出错时退回本进程计数。
// 退回不会把所有 IP 封禁，也不会在计数失败时静默取消限制。
type Limiter struct {
	mu     sync.Mutex
	local  map[string]*localAttempt
	events []orderriskcontract.AttemptEvent
}

var (
	_ orderriskcontract.RateLimiter    = (*Limiter)(nil)
	_ orderriskcontract.AttemptLimiter = (*Limiter)(nil)
)

func New() *Limiter {
	return &Limiter{local: map[string]*localAttempt{}}
}

func (l *Limiter) Check(input orderriskcontract.CheckInput, config settingssecurity.OrderRateLimitConfig) error {
	client := cache.Client()
	if client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if input.IsGuest && !input.SkipIPCheck && input.RiskIP != "" {
		if err := checkSingle(ctx, client, fmt.Sprintf("dj:risk:order_rate:guest_ip:%s", input.RiskIP), config); err != nil {
			return err
		}
	} else if !input.IsGuest && input.UserID > 0 {
		if err := checkSingle(ctx, client, fmt.Sprintf("dj:risk:order_rate:user:%d", input.UserID), config); err != nil {
			return err
		}
	}
	return nil
}

func (l *Limiter) Consume(riskIP string, config settingssecurity.OrderRateLimitConfig, meta orderriskcontract.AttemptMeta) error {
	if l == nil || riskIP == "" || !config.Enabled || config.MaxRequests < 1 || config.WindowSeconds < 1 {
		return nil
	}
	meta.Device = clipDevice(meta.Device)
	client := cache.Client()
	if client != nil {
		err := l.consumeRedis(client, riskIP, config, meta)
		if err == nil || isRateLimited(err) {
			return err
		}
		logger.Warnw("risk_order_attempt_redis_fallback", "error", err)
	}
	return l.consumeLocal(riskIP, config, meta)
}

func (l *Limiter) Peek(riskIP string, limit int) (int64, int64, bool) {
	if l == nil || riskIP == "" {
		return 0, 0, false
	}
	if limit < 1 {
		limit = 1
	}
	now := time.Now()
	l.mu.Lock()
	if state := l.local[riskIP]; state != nil && state.expires.After(now) {
		retry := int64(time.Until(state.expires).Seconds())
		if retry < 1 {
			retry = 1
		}
		count := state.count
		l.mu.Unlock()
		return count, retry, count > int64(limit)
	}
	l.mu.Unlock()
	client := cache.Client()
	if client == nil {
		return 0, 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	count, err := client.Get(ctx, attemptKeyPrefix+riskIP).Int64()
	if err != nil {
		return 0, 0, false
	}
	ttl, err := client.TTL(ctx, attemptKeyPrefix+riskIP).Result()
	if err != nil || ttl <= 0 {
		return count, 0, count > int64(limit)
	}
	retry := int64(ttl.Seconds())
	if retry < 1 {
		retry = 1
	}
	return count, retry, count > int64(limit)
}

func (l *Limiter) Snapshot(limit int) ([]orderriskcontract.AttemptSnapshot, []orderriskcontract.AttemptEvent) {
	if l == nil {
		return nil, nil
	}
	if limit < 1 {
		limit = 1
	}
	client := cache.Client()
	if client != nil {
		if active, events, ok := l.snapshotRedis(client, limit); ok {
			return active, events
		}
	}
	return l.snapshotLocal(limit), l.localEvents()
}

func (l *Limiter) Clear(riskIP string) (bool, error) {
	if l == nil || riskIP == "" {
		return false, nil
	}
	cleared := false
	l.mu.Lock()
	if _, ok := l.local[riskIP]; ok {
		delete(l.local, riskIP)
		cleared = true
	}
	l.mu.Unlock()
	client := cache.Client()
	if client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		removed, err := client.Del(ctx, attemptKeyPrefix+riskIP, attemptMetaPrefix+riskIP).Result()
		if err != nil {
			logger.Warnw("risk_order_attempt_clear_error", "error", err)
			return cleared, err
		}
		if removed > 0 {
			cleared = true
		}
	}
	l.pushEvent(orderriskcontract.AttemptEvent{
		At:      time.Now().UTC(),
		IP:      riskIP,
		Action:  "unbanned",
		Reason:  orderriskcontract.AttemptReason,
		Backend: backendName(client),
	})
	return cleared, nil
}

func (l *Limiter) consumeRedis(client *redis.Client, riskIP string, config settingssecurity.OrderRateLimitConfig, meta orderriskcontract.AttemptMeta) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	current, ttl, err := runCounter(ctx, client, attemptKeyPrefix+riskIP, config)
	if err != nil {
		return err
	}
	l.writeRedisMeta(ctx, client, riskIP, meta, ttl)
	if current == int64(config.MaxRequests)+1 {
		l.pushEvent(orderriskcontract.AttemptEvent{
			At:         time.Now().UTC(),
			IP:         riskIP,
			Action:     "banned",
			Reason:     orderriskcontract.AttemptReason,
			Count:      current,
			RetryAfter: ttl,
			Device:     meta.Device,
			Backend:    "redis",
		})
	}
	if current <= int64(config.MaxRequests) {
		return nil
	}
	return &orderriskcontract.RateLimitedError{RetryAfter: ttl}
}

func (l *Limiter) writeRedisMeta(ctx context.Context, client *redis.Client, riskIP string, meta orderriskcontract.AttemptMeta, ttl int64) {
	if ttl < 1 {
		ttl = 1
	}
	raw, err := json.Marshal(storedMeta{Device: meta.Device, Signal: clipDevice(meta.Signal), UserID: meta.UserID, Guest: meta.Guest})
	if err != nil {
		return
	}
	if err := client.Set(ctx, attemptMetaPrefix+riskIP, raw, time.Duration(ttl)*time.Second).Err(); err != nil {
		logger.Warnw("risk_order_attempt_meta_error", "error", err)
	}
}

func (l *Limiter) consumeLocal(riskIP string, config settingssecurity.OrderRateLimitConfig, meta orderriskcontract.AttemptMeta) error {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.local[riskIP]
	if state == nil || !state.expires.After(now) {
		state = &localAttempt{expires: now.Add(time.Duration(config.WindowSeconds) * time.Second)}
		l.local[riskIP] = state
	}
	state.count++
	state.device = meta.Device
	state.signal = clipDevice(meta.Signal)
	state.userID = meta.UserID
	state.guest = meta.Guest
	if config.BlockSeconds > 0 && state.count == int64(config.MaxRequests)+1 {
		state.expires = now.Add(time.Duration(config.BlockSeconds) * time.Second)
		l.events = append(l.events, orderriskcontract.AttemptEvent{
			At:         now.UTC(),
			IP:         riskIP,
			Action:     "banned",
			Reason:     orderriskcontract.AttemptReason,
			Count:      state.count,
			RetryAfter: int64(config.BlockSeconds),
			Device:     meta.Device,
			Backend:    "memory",
		})
		l.events = trimEvents(l.events)
	}
	if state.count <= int64(config.MaxRequests) {
		return nil
	}
	retry := int64(time.Until(state.expires).Seconds())
	if retry < 0 {
		retry = 0
	}
	return &orderriskcontract.RateLimitedError{RetryAfter: retry}
}

func (l *Limiter) snapshotLocal(limit int) []orderriskcontract.AttemptSnapshot {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	active := make([]orderriskcontract.AttemptSnapshot, 0)
	for ip, state := range l.local {
		if state == nil || !state.expires.After(now) {
			delete(l.local, ip)
			continue
		}
		retry := int64(time.Until(state.expires).Seconds())
		if retry < 0 {
			retry = 0
		}
		active = append(active, snapshotFrom(ip, state.count, limit, retry, state.device, state.signal, state.userID, state.guest, "memory"))
		if len(active) >= attemptEventCap {
			break
		}
	}
	return active
}

func (l *Limiter) snapshotRedis(client *redis.Client, limit int) ([]orderriskcontract.AttemptSnapshot, []orderriskcontract.AttemptEvent, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var (
		cursor uint64
		keys   []string
	)
	for {
		batch, next, err := client.Scan(ctx, cursor, attemptKeyPrefix+"*", 100).Result()
		if err != nil {
			logger.Warnw("risk_order_attempt_scan_error", "error", err)
			return nil, nil, false
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 || len(keys) >= attemptEventCap {
			break
		}
	}
	active := make([]orderriskcontract.AttemptSnapshot, 0, len(keys))
	for _, key := range keys {
		if len(active) >= attemptEventCap {
			break
		}
		ip := key[len(attemptKeyPrefix):]
		count, err := client.Get(ctx, key).Int64()
		if err != nil {
			continue
		}
		ttl, err := client.TTL(ctx, key).Result()
		if err != nil || ttl <= 0 {
			continue
		}
		meta := storedMeta{}
		if raw, err := client.Get(ctx, attemptMetaPrefix+ip).Bytes(); err == nil {
			_ = json.Unmarshal(raw, &meta)
		}
		active = append(active, snapshotFrom(ip, count, limit, int64(ttl.Seconds()), meta.Device, meta.Signal, meta.UserID, meta.Guest, "redis"))
	}
	events, err := readRedisEvents(ctx, client)
	if err != nil {
		return active, l.localEvents(), true
	}
	return active, events, true
}

func (l *Limiter) localEvents() []orderriskcontract.AttemptEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]orderriskcontract.AttemptEvent, len(l.events))
	copy(out, l.events)
	return out
}

func (l *Limiter) pushEvent(event orderriskcontract.AttemptEvent) {
	l.mu.Lock()
	l.events = append(l.events, event)
	l.events = trimEvents(l.events)
	l.mu.Unlock()
	client := cache.Client()
	if client == nil {
		return
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pipe := client.TxPipeline()
	pipe.LPush(ctx, attemptEventKey, raw)
	pipe.LTrim(ctx, attemptEventKey, 0, attemptEventCap-1)
	pipe.Expire(ctx, attemptEventKey, 24*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		logger.Warnw("risk_order_attempt_event_error", "error", err)
	}
}

func checkSingle(ctx context.Context, client *redis.Client, key string, config settingssecurity.OrderRateLimitConfig) error {
	current, ttl, err := runCounter(ctx, client, key, config)
	if err != nil {
		logger.Warnw("risk_control_rate_limit_script_error", "key", key, "error", err)
		return nil
	}
	if current <= int64(config.MaxRequests) {
		return nil
	}
	return &orderriskcontract.RateLimitedError{RetryAfter: ttl}
}

func runCounter(ctx context.Context, client *redis.Client, key string, config settingssecurity.OrderRateLimitConfig) (int64, int64, error) {
	result, err := orderRateLimitScript.Run(ctx, client, []string{key},
		config.WindowSeconds, config.MaxRequests, config.BlockSeconds,
	).Result()
	if err != nil {
		return 0, 0, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) < 2 {
		return 0, 0, errors.New("unexpected order attempt counter result")
	}
	current, ok := asInt64(values[0])
	if !ok {
		return 0, 0, errors.New("unexpected order attempt count")
	}
	ttl, ok := asInt64(values[1])
	if !ok {
		return 0, 0, errors.New("unexpected order attempt ttl")
	}
	if ttl < 0 {
		ttl = 0
	}
	return current, ttl, nil
}

func readRedisEvents(ctx context.Context, client *redis.Client) ([]orderriskcontract.AttemptEvent, error) {
	raws, err := client.LRange(ctx, attemptEventKey, 0, attemptEventCap-1).Result()
	if err != nil {
		return nil, err
	}
	events := make([]orderriskcontract.AttemptEvent, 0, len(raws))
	for _, raw := range raws {
		var event orderriskcontract.AttemptEvent
		if err := json.Unmarshal([]byte(raw), &event); err == nil {
			events = append(events, event)
		}
	}
	return events, nil
}

func snapshotFrom(ip string, count int64, limit int, retry int64, device, signal string, userID uint, guest bool, backend string) orderriskcontract.AttemptSnapshot {
	banned := count > int64(limit)
	reason := "order_attempt_window"
	if banned {
		reason = orderriskcontract.AttemptReason
	}
	return orderriskcontract.AttemptSnapshot{
		IP:         ip,
		Count:      count,
		Limit:      limit,
		Banned:     banned,
		RetryAfter: retry,
		Device:     device,
		Signal:     signal,
		UserID:     userID,
		Guest:      guest,
		Backend:    backend,
		Reason:     reason,
	}
}

func trimEvents(events []orderriskcontract.AttemptEvent) []orderriskcontract.AttemptEvent {
	if len(events) <= attemptEventCap {
		return events
	}
	return events[len(events)-attemptEventCap:]
}

func clipDevice(device string) string {
	if len(device) > 32 {
		return device[:32]
	}
	return device
}

func backendName(client *redis.Client) string {
	if client != nil {
		return "redis"
	}
	return "memory"
}

func isRateLimited(err error) bool {
	return errors.Is(err, orderriskcontract.ErrOrderRateLimited)
}

func asInt64(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case string:
		var parsed int64
		_, err := fmt.Sscan(typed, &parsed)
		return parsed, err == nil
	default:
		return 0, false
	}
}
