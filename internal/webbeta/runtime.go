package webbeta

import (
	"context"
	"errors"
	"sync"
)

type engineStartup struct {
	done   chan struct{}
	engine *Engine
	err    error
}

var runtimeEngine struct {
	sync.RWMutex
	engine  *Engine
	startup *engineStartup
}

func CurrentEngine() *Engine {
	runtimeEngine.RLock()
	defer runtimeEngine.RUnlock()
	return runtimeEngine.engine
}

func finishStartupLocked(start *engineStartup, e *Engine, err error) {
	select {
	case <-start.done:
		return
	default:
	}
	start.engine, start.err = e, err
	close(start.done)
}

func SetEngine(e *Engine) {
	runtimeEngine.Lock()
	defer runtimeEngine.Unlock()
	runtimeEngine.engine = e
	if start := runtimeEngine.startup; start != nil {
		err := error(nil)
		if e == nil {
			err = ErrIdentityClosed
		}
		finishStartupLocked(start, e, err)
		runtimeEngine.startup = nil
	}
}

// Called by the desktop before its startup goroutine. It announces initialization
// only: it does not create a browser, authorize requests, refill or send anything.
func BeginEngineStartup() func(error) {
	runtimeEngine.Lock()
	if runtimeEngine.engine != nil || runtimeEngine.startup != nil {
		runtimeEngine.Unlock()
		return func(error) {}
	}
	start := &engineStartup{done: make(chan struct{})}
	runtimeEngine.startup = start
	runtimeEngine.Unlock()
	return func(err error) {
		runtimeEngine.Lock()
		defer runtimeEngine.Unlock()
		if runtimeEngine.startup != start {
			return
		}
		if err == nil && runtimeEngine.engine == nil {
			err = errors.New("网页后台初始化未完成，消息尚未发送。请检查网页接入状态。")
		}
		finishStartupLocked(start, runtimeEngine.engine, err)
		runtimeEngine.startup = nil
	}
}

// Await only an explicitly announced startup. A logout resolves that startup
// with ErrIdentityClosed; waiters cannot attach themselves to the next login.
func AwaitEngine(ctx context.Context) (*Engine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtimeEngine.RLock()
	e, start := runtimeEngine.engine, runtimeEngine.startup
	runtimeEngine.RUnlock()
	if e != nil {
		return e, nil
	}
	if start == nil {
		return nil, AvailabilityError(nil)
	}
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("网页正在后台初始化，请稍后重试。消息尚未发送。")
		}
		return nil, ctx.Err()
	case <-start.done:
		return start.engine, start.err
	}
}
