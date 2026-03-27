package providers

import (
	"context"
	"fmt"
	"sync"
)

var (
	mu        sync.RWMutex
	providers = make(map[string]Provider)
)

// RegisterProvider adds a provider to the global registry.
func RegisterProvider(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers[p.Name()] = p
}

// GetProvider returns the provider registered under the given name.
func GetProvider(name string) (Provider, error) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown secret provider: %q", name)
	}
	return p, nil
}

// Resolve looks up the provider identified by ref.Source and delegates to it.
func Resolve(ctx context.Context, ref SecretRef) (string, error) {
	p, err := GetProvider(ref.Source)
	if err != nil {
		return "", err
	}
	return p.Resolve(ctx, ref)
}

func init() {
	RegisterProvider(&EnvProvider{})
	RegisterProvider(&FileProvider{})
	RegisterProvider(&ExecProvider{})
}
