package confkit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeromicro/go-zero/core/conf"
)

// ResolvePath resolves a file path relative to a base directory.
// It expands environment variables and handles both absolute and relative paths.
// If the file path is absolute, it returns the expanded path directly.
// Otherwise, it joins the base directory with the file path.
func ResolvePath(base, file string) string {
	file = os.ExpandEnv(file)
	if filepath.IsAbs(file) {
		return filepath.Clean(file)
	}
	return filepath.Join(base, file)
}

// BaseDir returns the directory of the main config file path.
func BaseDir(mainPath string) string {
	return filepath.Dir(mainPath)
}

// LoadFile loads a configuration file into the provided type T.
// It uses go-zero's conf.Load with optional environment variable expansion.
func LoadFile[T any](path string, useEnv bool) (*T, error) {
	var cfg T
	opts := []conf.Option{}
	if useEnv {
		opts = append(opts, conf.UseEnv())
	}
	if err := conf.Load(path, &cfg, opts...); err != nil {
		return nil, fmt.Errorf("load config %s: %w", path, err)
	}
	return &cfg, nil
}

// Section represents a configuration section that can be loaded from a separate file.
// The generic type T is the configuration type for this section.
type Section[T any] struct {
	File  string `json:",optional"`
	Value *T     `json:"-"`
}

// Hydrate loads the configuration file specified in the File field and stores
// the result in the Value field. The loader function is responsible for loading
// and parsing the configuration file.
// If File is empty, this method does nothing and returns nil.
func (s *Section[T]) Hydrate(base string, loader func(string) (*T, error)) error {
	if s.File == "" {
		return nil
	}
	p := ResolvePath(base, s.File)
	v, err := loader(p)
	if err != nil {
		return err
	}
	s.File, s.Value = p, v
	return nil
}
