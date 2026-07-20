package source

import (
	"context"
	"fmt"
)

func Convert(ctx context.Context, from Repository, to *SQLite, progress func(done, total int, slug string)) error {
	items, err := from.Catalog(ctx)
	if err != nil {
		return err
	}
	for i, item := range items {
		p, err := from.Problem(ctx, item.Slug)
		if err != nil {
			return fmt.Errorf("read %s: %w", item.Slug, err)
		}
		if err := to.Put(ctx, p); err != nil {
			return fmt.Errorf("write %s: %w", item.Slug, err)
		}
		if progress != nil {
			progress(i+1, len(items), item.Slug)
		}
	}
	return nil
}
