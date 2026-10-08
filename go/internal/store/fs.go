package store

import (
	"fmt"
	"io/fs"
)

func fsSub() (fs.FS, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("store: migrations fs: %w", err)
	}
	return sub, nil
}
