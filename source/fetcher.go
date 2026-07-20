package source

import (
	"context"
	"fmt"
	"time"
)

// Fetcher mirrors public problem data into SQLite without an API key.
type Fetcher struct {
	Client   *Client
	Delay    time.Duration
	Progress func(done, total int, slug string)
}

func (f Fetcher) Sync(ctx context.Context, database *SQLite) error {
	if f.Client == nil {
		return fmt.Errorf("fetcher client is nil")
	}
	catalog, err := f.Client.Catalog(ctx)
	if err != nil {
		return err
	}
	delay := f.Delay
	if delay <= 0 {
		delay = 50 * time.Millisecond
	}
	for i, item := range catalog {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		p, err := f.Client.Problem(ctx, item.Slug)
		if err != nil {
			return fmt.Errorf("fetch %s: %w", item.Slug, err)
		}
		if err := database.Put(ctx, p); err != nil {
			return fmt.Errorf("store %s: %w", item.Slug, err)
		}
		if f.Progress != nil {
			f.Progress(i+1, len(catalog), item.Slug)
		}
	}
	return nil
}
