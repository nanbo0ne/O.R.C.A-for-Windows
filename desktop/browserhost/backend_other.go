//go:build !windows

package browserhost

import "context"

type Host struct{}

func New(func(Event)) *Host                                        { return &Host{} }
func NewWithWindowClass(string, func(Event)) *Host                 { return &Host{} }
func (*Host) SetNavigationGuard(func(string, string) bool)         {}
func (*Host) Create(context.Context, string, string, string) error { return ErrUnsupported }
func (*Host) Close(context.Context, string) error                  { return ErrUnsupported }
func (*Host) Navigate(context.Context, string, string) error       { return ErrUnsupported }
func (*Host) Command(context.Context, string, string, string) (string, error) {
	return "", ErrUnsupported
}
func (*Host) Present(context.Context, Viewport) error { return ErrUnsupported }
func (*Host) Observe(context.Context, string) (Observation, error) {
	return Observation{}, ErrUnsupported
}
func (*Host) Execute(context.Context, string, Action) error { return ErrUnsupported }
func (*Host) Shutdown()                                     {}

func (*Host) ShutdownConfirmed(context.Context) error { return nil }
