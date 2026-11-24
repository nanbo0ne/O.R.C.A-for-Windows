//go:build windows

package browserhost

import (
	"context"
	"fmt"
	wv "github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost/internal/webviewabi"
	"unsafe"
)

func (h *Host) clearProfile(ctx context.Context, id string) error {
	done := make(chan error, 1)
	err := h.post(ctx, func() {
		t, e := h.tab(id)
		if e != nil {
			done <- e
			return
		}
		var view *wv.View13
		if e = raw(unsafe.Pointer(t.view), t.view.Vtbl.QueryInterface, uintptr(unsafe.Pointer(&wv.IIDView13)), uintptr(unsafe.Pointer(&view))); e != nil || view == nil {
			done <- fmt.Errorf("browser profile interface unavailable: %v", e)
			return
		}
		defer raw(unsafe.Pointer(view), view.Vtbl.Release)
		var profile *wv.Profile2
		if e = raw(unsafe.Pointer(view), view.Vtbl.GetProfile, uintptr(unsafe.Pointer(&profile))); e != nil || profile == nil {
			done <- fmt.Errorf("browser profile unavailable: %v", e)
			return
		}
		defer raw(unsafe.Pointer(profile), profile.Vtbl.Release)
		var p2 *wv.Profile2
		if e = raw(unsafe.Pointer(profile), profile.Vtbl.QueryInterface, uintptr(unsafe.Pointer(&wv.IIDProfile2)), uintptr(unsafe.Pointer(&p2))); e != nil || p2 == nil {
			done <- fmt.Errorf("browser profile clearing unavailable: %v", e)
			return
		}
		defer raw(unsafe.Pointer(p2), p2.Vtbl.Release)
		handler := newCOMCallback(true, func(hr, unused uintptr) uintptr {
			if int32(hr) < 0 {
				done <- fmt.Errorf("browser data clear failed: 0x%x", hr)
			} else {
				done <- nil
			}
			return 0
		})
		defer handler.release()
		if e = raw(unsafe.Pointer(p2), p2.Vtbl.ClearBrowsingDataAll, handler.pointer()); e != nil {
			done <- e
		}
	})
	if err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
		return context.Canceled
	}
}
