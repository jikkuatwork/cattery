package server

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

var ErrQueueFull = errors.New("queue full")

// PoolConfig configures a modality-specific engine pool.
type PoolConfig[T any] struct {
	Name        string
	Workers     int
	IdleTimeout time.Duration
	KeepAlive   bool
	MemEstimate int64
	Create      func() (T, error)
	Close       func(T) error
	OnEmpty     func()
}

// Pool manages lazy creation and idle eviction for one modality.
type Pool[T any] struct {
	name        string
	workers     int
	idleTimeout time.Duration
	keepAlive   bool
	memEstimate int64
	create      func() (T, error)
	close       func(T) error
	onEmpty     func()

	slots chan struct{}
	idle  chan T

	mu        sync.Mutex
	created   int
	idleTimer *time.Timer
}

// PoolStats describes the pool's ready capacity.
type PoolStats struct {
	Workers      int `json:"workers"`
	EnginesReady int `json:"engines_ready"`
}

// NewPool creates a modality-specific engine pool.
func NewPool[T any](cfg PoolConfig[T]) *Pool[T] {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}

	slots := make(chan struct{}, cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		slots <- struct{}{}
	}

	return &Pool[T]{
		name:        cfg.Name,
		workers:     cfg.Workers,
		idleTimeout: cfg.IdleTimeout,
		keepAlive:   cfg.KeepAlive,
		memEstimate: cfg.MemEstimate,
		create:      cfg.Create,
		close:       cfg.Close,
		onEmpty:     cfg.OnEmpty,
		slots:       slots,
		idle:        make(chan T, cfg.Workers),
	}
}

// Prewarm eagerly creates all configured engines.
func (p *Pool[T]) Prewarm() error {
	for i := 0; i < p.workers; i++ {
		eng, err := p.create()
		if err != nil {
			p.Shutdown()
			return err
		}

		p.mu.Lock()
		p.created++
		p.mu.Unlock()

		p.idle <- eng
	}

	log.Printf("pre-warmed %s pool with %d engine(s)", p.name, p.workers)
	return nil
}

// Borrow returns an engine, creating one lazily if needed.
func (p *Pool[T]) Borrow(
	ctx context.Context,
	queue chan struct{},
	queued *atomic.Int64,
) (T, error) {
	var zero T

	if err := p.acquireSlot(ctx, queue, queued); err != nil {
		return zero, err
	}

	p.stopIdleTimer()

	select {
	case eng := <-p.idle:
		return eng, nil
	default:
	}

	p.mu.Lock()
	if p.created < p.workers {
		p.created++
		p.mu.Unlock()

		eng, err := p.create()
		if err != nil {
			p.mu.Lock()
			p.created--
			p.mu.Unlock()
			p.releaseSlot()
			return zero, err
		}
		return eng, nil
	}
	p.mu.Unlock()

	select {
	case eng := <-p.idle:
		return eng, nil
	case <-ctx.Done():
		p.releaseSlot()
		return zero, ctx.Err()
	}
}

// Return puts an engine back into the idle pool.
func (p *Pool[T]) Return(eng T) {
	p.idle <- eng
	p.releaseSlot()
	p.scheduleIdleTimer()
}

// Stats reports current pool readiness.
func (p *Pool[T]) Stats() PoolStats {
	return PoolStats{
		Workers:      p.workers,
		EnginesReady: len(p.idle),
	}
}

// MemEstimate reports the estimated memory footprint of one engine instance.
func (p *Pool[T]) MemEstimate() int64 {
	return p.memEstimate
}

// Empty reports whether all engines have been evicted.
func (p *Pool[T]) Empty() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.created == 0
}

// Shutdown closes all idle engines and stops the idle timer.
func (p *Pool[T]) Shutdown() {
	p.mu.Lock()
	if p.idleTimer != nil {
		p.idleTimer.Stop()
		p.idleTimer = nil
	}

	var toClose []T
	for {
		select {
		case eng := <-p.idle:
			toClose = append(toClose, eng)
			p.created--
		default:
			p.mu.Unlock()
			for _, eng := range toClose {
				if err := p.close(eng); err != nil {
					log.Printf("close %s engine: %v", p.name, err)
				}
			}
			return
		}
	}
}

// EvictIdle closes all currently idle engines without affecting borrowed ones.
func (p *Pool[T]) EvictIdle() {
	p.evictIdle()
}

func (p *Pool[T]) acquireSlot(
	ctx context.Context,
	queue chan struct{},
	queued *atomic.Int64,
) error {
	select {
	case <-p.slots:
		return nil
	default:
	}

	if queue == nil {
		return ErrQueueFull
	}

	select {
	case queue <- struct{}{}:
		if queued != nil {
			queued.Add(1)
		}
	default:
		return ErrQueueFull
	}
	defer func() {
		<-queue
		if queued != nil {
			queued.Add(-1)
		}
	}()

	select {
	case <-p.slots:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Pool[T]) releaseSlot() {
	p.slots <- struct{}{}
}

func (p *Pool[T]) stopIdleTimer() {
	if p.keepAlive {
		return
	}

	p.mu.Lock()
	if p.idleTimer != nil {
		p.idleTimer.Stop()
		p.idleTimer = nil
	}
	p.mu.Unlock()
}

func (p *Pool[T]) scheduleIdleTimer() {
	if p.keepAlive || p.idleTimeout <= 0 {
		return
	}

	p.mu.Lock()
	if p.idleTimer != nil {
		p.idleTimer.Stop()
	}
	p.idleTimer = time.AfterFunc(p.idleTimeout, p.evictIdle)
	p.mu.Unlock()
}

func (p *Pool[T]) evictIdle() {
	p.mu.Lock()
	if len(p.slots) != p.workers {
		p.idleTimer = nil
		p.mu.Unlock()
		return
	}

	var toClose []T
	for {
		select {
		case eng := <-p.idle:
			toClose = append(toClose, eng)
			p.created--
		default:
			p.idleTimer = nil
			empty := p.created == 0
			p.mu.Unlock()

			for _, eng := range toClose {
				if err := p.close(eng); err != nil {
					log.Printf("close %s engine: %v", p.name, err)
				}
			}

			if len(toClose) > 0 {
				log.Printf("evicted %d %s engine(s)", len(toClose), p.name)
			}
			if empty && len(toClose) > 0 && p.onEmpty != nil {
				p.onEmpty()
			}
			return
		}
	}
}
